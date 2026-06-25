import type { Node, Edge, Hierarchy } from './types';

export function buildHierarchy(nodes: Node[], edges: Edge[]): Hierarchy {
  const present = new Set(nodes.map((nd) => nd.id));
  const parent = new Map<string, string>();
  const children = new Map<string, string[]>();

  for (const ed of edges) {
    if (ed.type !== 'parent_child') continue;
    if (!present.has(ed.src) || !present.has(ed.dst)) continue;
    parent.set(ed.dst, ed.src);
  }
  for (const nd of nodes) {
    if (parent.has(nd.id)) continue;
    if (nd.parent_id && present.has(nd.parent_id)) {
      parent.set(nd.id, nd.parent_id);
    }
  }
  for (const nd of nodes) {
    const p = parent.get(nd.id);
    if (p === undefined) continue;
    const arr = children.get(p);
    if (arr) arr.push(nd.id);
    else children.set(p, [nd.id]);
  }
  for (const arr of children.values()) arr.sort();

  const roots: string[] = [];
  const orphans: string[] = [];
  for (const nd of nodes) {
    if (parent.has(nd.id)) continue;
    if ((children.get(nd.id)?.length ?? 0) > 0) roots.push(nd.id);
    else orphans.push(nd.id);
  }
  roots.sort();
  orphans.sort();

  const childrenOf = (id: string): string[] => children.get(id) ?? [];
  const parentOf = (id: string): string | undefined => parent.get(id);

  const ancestorsOf = (id: string): string[] => {
    const out: string[] = [];
    const seen = new Set<string>([id]);
    let cur = parent.get(id);
    while (cur !== undefined && !seen.has(cur)) {
      out.push(cur);
      seen.add(cur);
      cur = parent.get(cur);
    }
    return out;
  };

  const descendantsOf = (id: string): string[] => {
    if (!present.has(id)) return [];
    const out: string[] = [];
    const seen = new Set<string>([id]);
    const walk = (node: string): void => {
      for (const c of childrenOf(node)) {
        if (seen.has(c)) continue;
        seen.add(c);
        out.push(c);
        walk(c);
      }
    };
    walk(id);
    return out;
  };

  return { childrenOf, parentOf, ancestorsOf, descendantsOf, roots, orphans };
}
