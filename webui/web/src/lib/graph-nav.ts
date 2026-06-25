import type { Node, Edge } from './types';

export type NavDir = 'left' | 'right' | 'up' | 'down';

export function nextNodeByDirection(
  currentId: string | null,
  nodes: Node[],
  edges: Edge[],
  dir: NavDir,
  visible?: Set<string>,
): string | null {
  const vNodes = visible ? nodes.filter((nd) => visible.has(nd.id)) : nodes;
  const vEdges = visible
    ? edges.filter((ed) => visible.has(ed.src) && visible.has(ed.dst))
    : edges;
  if (vNodes.length === 0) return null;

  if (currentId === null) {
    const hasIncoming = new Set(vEdges.filter((e) => e.type === 'parent_child').map((e) => e.dst));
    const roots = vNodes.filter((nd) => !hasIncoming.has(nd.id)).map((nd) => nd.id).sort();
    return roots.length > 0 ? roots[0]! : vNodes.map((nd) => nd.id).sort()[0]!;
  }

  if (dir === 'right') {
    const targets = vEdges.filter((e) => e.src === currentId).map((e) => e.dst).sort();
    return targets.length > 0 ? targets[0]! : currentId;
  }

  if (dir === 'left') {
    const sources = vEdges.filter((e) => e.dst === currentId).map((e) => e.src).sort();
    return sources.length > 0 ? sources[0]! : currentId;
  }

  const parentEdge = vEdges.find((e) => e.type === 'parent_child' && e.dst === currentId);
  if (!parentEdge) return currentId;

  const parentId = parentEdge.src;
  const siblings = vEdges
    .filter((e) => e.type === 'parent_child' && e.src === parentId)
    .map((e) => e.dst)
    .sort();

  const idx = siblings.indexOf(currentId);
  if (dir === 'up') {
    return idx > 0 ? siblings[idx - 1]! : currentId;
  }
  return idx < siblings.length - 1 ? siblings[idx + 1]! : currentId;
}
