import type { Node, Edge, SseEvent } from '../types';

export interface GraphState {
  nodes: Record<string, Node>;
  edges: Record<string, Edge>;
  established: Record<string, true>;
  tombstones: Record<string, number>;
  // Internal: tracks the highest rev of any event that carried status fields
  // for each node. Kept separate from nodes[id].rev so that status LWW is
  // independent of the upsert identity guard.
  statusRevs: Record<string, number>;
}

export function emptyState(): GraphState {
  return { nodes: {}, edges: {}, established: {}, tombstones: {}, statusRevs: {} };
}

function applyStatusFields(target: Node, source: Partial<Node>): void {
  if (source.status !== undefined) target.status = source.status;
  if (source.t_end !== undefined) target.t_end = source.t_end;
  if (source.duration_ms !== undefined) target.duration_ms = source.duration_ms;
}

export function applyDelta(state: GraphState, ev: SseEvent): void {
  switch (ev.kind) {
    case 'node_upsert': {
      if (!ev.node) return;
      const n = ev.node;
      const nodeTomb = state.tombstones[`node:${n.id}`];
      if (nodeTomb !== undefined && n.rev <= nodeTomb) return;
      const existing = state.nodes[n.id];
      if (existing && state.established[n.id]) {
        if (n.rev <= existing.rev) return;
      }
      if (existing && !state.established[n.id]) {
        // Merging a new upsert into an unestablished (status-seeded) node.
        // Identity fields come from the upsert; status fields are latest-rev-wins
        // between the upsert rev and the previously tracked statusRevs[id].
        // statusRevs[n.id] is always defined here: an unestablished node can only
        // exist because node_status seeded it, and node_status always sets statusRevs.
        const merged: Node = { ...n };
        const prevStatusRev = state.statusRevs[n.id] as number;
        if (prevStatusRev > n.rev) {
          // Keep the higher-rev status fields from the seed.
          if (existing.status !== undefined) merged.status = existing.status;
          if (existing.t_end !== undefined) merged.t_end = existing.t_end;
          if (existing.duration_ms !== undefined) merged.duration_ms = existing.duration_ms;
        }
        state.nodes[n.id] = merged;
      } else {
        state.nodes[n.id] = { ...n };
      }
      state.established[n.id] = true;
      // An upsert is also a status-carrying event at its rev.
      const prevUpsertStatusRev = state.statusRevs[n.id] ?? 0;
      if (n.rev >= prevUpsertStatusRev) {
        state.statusRevs[n.id] = n.rev;
      }
      return;
    }

    case 'node_status': {
      if (!ev.node) return;
      const patch = ev.node;
      const nodeTomb = state.tombstones[`node:${patch.id}`];
      if (nodeTomb !== undefined && patch.rev <= nodeTomb) return;
      const existing = state.nodes[patch.id];
      // Latest-rev-wins for status fields, independently of the upsert rev.
      const prevStatusRev = state.statusRevs[patch.id] ?? 0;
      if (!existing) {
        const seed: Node = {
          id: patch.id,
          run_id: patch.run_id,
          type: patch.type,
          rev: patch.rev,
        };
        applyStatusFields(seed, patch);
        state.nodes[patch.id] = seed;
        state.statusRevs[patch.id] = patch.rev;
      } else {
        if (patch.rev >= prevStatusRev) {
          applyStatusFields(existing, patch);
          state.statusRevs[patch.id] = patch.rev;
          if (!state.established[patch.id]) {
            existing.rev = patch.rev;
          }
        }
      }
      return;
    }

    case 'node_merge': {
      if (!ev.node) return;
      const n = ev.node;
      if (ev.old_id && ev.old_id !== n.id) {
        delete state.nodes[ev.old_id];
        delete state.established[ev.old_id];
        delete state.statusRevs[ev.old_id];
        const existingTomb = state.tombstones[`node:${ev.old_id}`];
        state.tombstones[`node:${ev.old_id}`] =
          existingTomb !== undefined ? Math.max(existingTomb, ev.rev) : ev.rev;
        for (const e of Object.values(state.edges)) {
          if (e.src === ev.old_id) e.src = n.id;
          if (e.dst === ev.old_id) e.dst = n.id;
        }
      }
      state.nodes[n.id] = { ...n };
      state.established[n.id] = true;
      state.statusRevs[n.id] = n.rev;
      return;
    }

    case 'edge_upsert': {
      if (!ev.edge) return;
      const e = ev.edge;
      // Backend assigns monotonically increasing revs, so delete.rev > upsert.rev
      // always holds for any given edge in production. The tombstone LWW is correct
      // for all backend-realistic inputs: if a delete arrived first (out of SSE order),
      // its higher rev is already in the tombstone and will block this upsert.
      const tomb = state.tombstones[e.id];
      if (tomb !== undefined && e.rev <= tomb) return;
      const existing = state.edges[e.id];
      if (existing && e.rev <= existing.rev) return;
      state.edges[e.id] = { ...e };
      return;
    }

    case 'edge_delete': {
      if (!ev.edge) return;
      const id = ev.edge.id;
      // Backend assigns monotonically increasing revs, so a delete always has a
      // strictly higher rev than the same edge's prior upsert. Both arrival orders
      // therefore converge: delete-first records the tombstone which then blocks the
      // later upsert (upsert.rev < tombstone); upsert-first writes the edge which is
      // then removed here. The case upsert.rev > delete.rev cannot arise from the
      // backend and is out of scope.
      const existing = state.tombstones[id];
      state.tombstones[id] = existing !== undefined ? Math.max(existing, ev.rev) : ev.rev;
      delete state.edges[id];
      return;
    }

    default:
      return;
  }
}
