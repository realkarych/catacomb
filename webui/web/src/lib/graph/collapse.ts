import type { Node, Hierarchy } from './types';

export type CollapsePredicate = (node: Node) => boolean;

export const DEFAULT_COLLAPSE: CollapsePredicate = (node) =>
  node.type === 'assistant_turn' || node.type === 'subagent';

function hasChildren(hierarchy: Hierarchy, id: string): boolean {
  return hierarchy.childrenOf(id).length > 0;
}

export function defaultCollapsed(
  nodes: Node[],
  hierarchy: Hierarchy,
  predicate: CollapsePredicate = DEFAULT_COLLAPSE,
): Set<string> {
  const out = new Set<string>();
  for (const node of nodes) {
    if (predicate(node) && hasChildren(hierarchy, node.id)) out.add(node.id);
  }
  return out;
}

export function visibleNodeIds(
  nodes: Node[],
  hierarchy: Hierarchy,
  collapsed: Set<string>,
): Set<string> {
  const out = new Set<string>();
  for (const node of nodes) {
    const hidden = hierarchy.ancestorsOf(node.id).some((a) => collapsed.has(a));
    if (!hidden) out.add(node.id);
  }
  return out;
}

export function toggle(collapsed: Set<string>, id: string): Set<string> {
  const next = new Set(collapsed);
  if (next.has(id)) next.delete(id);
  else next.add(id);
  return next;
}

export function collapseAll(nodes: Node[], hierarchy: Hierarchy): Set<string> {
  const out = new Set<string>();
  for (const node of nodes) {
    if (hasChildren(hierarchy, node.id)) out.add(node.id);
  }
  return out;
}

export function expandAll(): Set<string> {
  return new Set<string>();
}
