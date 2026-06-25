import type { Edge, Hierarchy } from './types';

function liftEndpoint(
  id: string,
  visible: Set<string>,
  hierarchy: Hierarchy,
): string | undefined {
  if (visible.has(id)) return id;
  for (const anc of hierarchy.ancestorsOf(id)) {
    if (visible.has(anc)) return anc;
  }
  return undefined;
}

export function liftEdges(
  edges: Edge[],
  visible: Set<string>,
  hierarchy: Hierarchy,
): Edge[] {
  const out: Edge[] = [];
  const seen = new Set<string>();
  for (const edge of edges) {
    const src = liftEndpoint(edge.src, visible, hierarchy);
    const dst = liftEndpoint(edge.dst, visible, hierarchy);
    if (src === undefined || dst === undefined) continue;
    if (src === dst) continue;
    const key = `${src}|${dst}|${edge.type}`;
    if (seen.has(key)) continue;
    seen.add(key);
    out.push({ ...edge, src, dst });
  }
  return out;
}
