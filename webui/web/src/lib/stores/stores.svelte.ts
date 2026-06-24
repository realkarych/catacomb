import { applyDelta, emptyState } from '../reducer/reducer';
import type { GraphState } from '../reducer/reducer';
import type { Node, Edge, SessionSummary, SseEvent } from '../types';
import { sessionGraphFrom } from './selectors';

const _graphState: GraphState = $state(emptyState());

export const nodesById: Record<string, Node> = _graphState.nodes;
export const edgesById: Record<string, Edge> = _graphState.edges;
export const sessionsById: Record<string, SessionSummary> = $state({});
export const selectedNodeId: { value: string | null } = $state({ value: null });
export const connectionState: { status: 'idle' | 'connecting' | 'open' | 'error' } = $state({ status: 'idle' });

export function selectNode(id: string | null): void {
  selectedNodeId.value = id;
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
}
