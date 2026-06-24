import type { Node, Edge, SseEvent } from '../types';

export interface GraphState {
  nodes: Record<string, Node>;
  edges: Record<string, Edge>;
  established: Record<string, true>;
  tombstones: Record<string, number>;
}

export function emptyState(): GraphState {
  return { nodes: {}, edges: {}, established: {}, tombstones: {} };
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
        const merged: Node = { ...n };
        if (existing.status !== undefined && existing.rev > n.rev) {
          merged.status = existing.status;
        }
        if (existing.t_end !== undefined && existing.rev > n.rev) {
          merged.t_end = existing.t_end;
        }
        if (existing.duration_ms !== undefined && existing.rev > n.rev) {
          merged.duration_ms = existing.duration_ms;
        }
        state.nodes[n.id] = merged;
      } else {
        state.nodes[n.id] = { ...n };
      }
      state.established[n.id] = true;
      return;
    }

    case 'node_status': {
      if (!ev.node) return;
      const patch = ev.node;
      const nodeTomb = state.tombstones[`node:${patch.id}`];
      if (nodeTomb !== undefined && patch.rev <= nodeTomb) return;
      const existing = state.nodes[patch.id];
      if (!existing) {
        const seed: Node = {
          id: patch.id,
          run_id: patch.run_id,
          type: patch.type,
          rev: patch.rev,
        };
        if (patch.status !== undefined) seed.status = patch.status;
        if (patch.t_end !== undefined) seed.t_end = patch.t_end;
        if (patch.duration_ms !== undefined) seed.duration_ms = patch.duration_ms;
        state.nodes[patch.id] = seed;
      } else {
        if (patch.rev >= existing.rev) {
          if (patch.status !== undefined) existing.status = patch.status;
          if (patch.t_end !== undefined) existing.t_end = patch.t_end;
          if (patch.duration_ms !== undefined) existing.duration_ms = patch.duration_ms;
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
      return;
    }

    case 'edge_upsert': {
      if (!ev.edge) return;
      const e = ev.edge;
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
      const existing = state.tombstones[id];
      state.tombstones[id] = existing !== undefined ? Math.max(existing, ev.rev) : ev.rev;
      delete state.edges[id];
      return;
    }

    default:
      return;
  }
}
