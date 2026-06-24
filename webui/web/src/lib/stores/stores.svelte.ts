import { applyDelta, emptyState } from '../reducer/reducer';
import type { GraphState } from '../reducer/reducer';
import type { Node, Edge, SessionSummary, SseEvent } from '../types';
import { emptyFilter } from '../filters';
import type { FilterState } from '../filters';
import { sessionGraphFrom } from './selectors';
import { nextLastSeenRev } from '../sse/client';

const _graphState: GraphState = $state(emptyState());

export const nodesById: Record<string, Node> = _graphState.nodes;
export const edgesById: Record<string, Edge> = _graphState.edges;
export const sessionsById: Record<string, SessionSummary> = $state({});
export const selectedNodeId: { value: string | null } = $state({ value: null });
export const connectionState: { status: 'idle' | 'connecting' | 'open' | 'error' } = $state({ status: 'idle' });
export const filterState: FilterState = $state(emptyFilter());
export const filteredNodeIds: { value: Set<string> | null } = $state({ value: null });
export const desync: { stale: boolean; parseErrors: number } = $state({ stale: false, parseErrors: 0 });
export const lastSeenRev: { value: number } = $state({ value: 0 });

export function selectNode(id: string | null): void {
  selectedNodeId.value = id;
}

export function navigateToNode(sessionHash: string, id: string | null): void {
  selectedNodeId.value = id;
  if (typeof window === 'undefined') return;
  if (id === null) {
    window.location.hash = `#/s/${encodeURIComponent(sessionHash)}`;
  } else {
    window.location.hash = `#/s/${encodeURIComponent(sessionHash)}/n/${encodeURIComponent(id)}`;
  }
}

export function upsertSession(s: SessionSummary): void {
  sessionsById[s.session] = s;
}

export function sessionGraph(hash: string): { nodes: Node[]; edges: Edge[] } {
  const session = sessionsById[hash];
  if (!session) return { nodes: [], edges: [] };
  const runIds = new Set(session.run_ids);
  return sessionGraphFrom(_graphState, runIds);
}

export function handleEvent(ev: SseEvent): void {
  applyDelta(_graphState, ev);
  lastSeenRev.value = nextLastSeenRev(lastSeenRev.value, ev);
}

export function recordParseError(): void {
  desync.stale = true;
  desync.parseErrors += 1;
}

export function setDesyncStale(stale: boolean): void {
  desync.stale = stale;
}

export function resetFilter(): void {
  Object.assign(filterState, emptyFilter());
}

export function setFilteredNodeIds(ids: Set<string> | null): void {
  filteredNodeIds.value = ids;
}
