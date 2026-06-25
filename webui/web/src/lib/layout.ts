import dagre from '@dagrejs/dagre';
import type { Node as CNode, Edge as CEdge } from './types';
import { buildHierarchy } from './graph/hierarchy';
import { visibleNodeIds } from './graph/collapse';
import { liftEdges } from './graph/lift-edges';
import type { Hierarchy } from './graph/types';

export interface LayoutOptions {
  rankdir?: 'LR' | 'TB';
  nodesep?: number;
  ranksep?: number;
  nodeWidth?: number;
  nodeHeight?: number;
}

export interface XyNode {
  id: string;
  position: { x: number; y: number };
  data: { catNode: CNode };
  type: string;
}

export interface XyEdge {
  id: string;
  source: string;
  target: string;
  type: string;
}

export interface LayoutResult {
  nodes: XyNode[];
  edges: XyEdge[];
}

export function dagreNodeToPosition(
  pos: { x: number; y: number } | undefined,
  nodeWidth: number,
  nodeHeight: number,
): { x: number; y: number } {
  if (!pos) return { x: 0, y: 0 };
  return { x: pos.x - nodeWidth / 2, y: pos.y - nodeHeight / 2 };
}

function edgeXyType(edgeType: string): string {
  if (edgeType === 'parent_child') return 'default';
  if (edgeType === 'sequence') return 'step';
  if (edgeType === 'data_dep') return 'smoothstep';
  return 'default';
}

export function applyLayout(
  nodes: CNode[],
  edges: CEdge[],
  opts?: LayoutOptions,
): LayoutResult {
  const nodeWidth = opts?.nodeWidth ?? 200;
  const nodeHeight = opts?.nodeHeight ?? 60;

  const g = new dagre.graphlib.Graph({ multigraph: false });
  g.setGraph({
    rankdir: opts?.rankdir ?? 'LR',
    nodesep: opts?.nodesep ?? 60,
    ranksep: opts?.ranksep ?? 100,
    align: 'UL',
    ranker: 'network-simplex',
  });
  g.setDefaultEdgeLabel(() => ({}));

  for (const node of nodes) {
    g.setNode(node.id, { width: nodeWidth, height: nodeHeight });
  }

  for (const edge of edges) {
    g.setEdge(edge.src, edge.dst);
  }

  dagre.layout(g);

  const outNodes: XyNode[] = nodes.map((node) => {
    const pos = g.node(node.id) as { x: number; y: number } | undefined;
    return {
      id: node.id,
      position: dagreNodeToPosition(pos, nodeWidth, nodeHeight),
      data: { catNode: node },
      type: node.type,
    };
  });

  const outEdges: XyEdge[] = edges.map((edge) => ({
    id: edge.id,
    source: edge.src,
    target: edge.dst,
    type: edgeXyType(edge.type),
  }));

  return { nodes: outNodes, edges: outEdges };
}

export interface CollapseView {
  nodes: CNode[];
  edges: CEdge[];
  visible: Set<string>;
  hierarchy: Hierarchy;
}

export function collapseView(
  nodes: CNode[],
  edges: CEdge[],
  collapsed: Set<string>,
): CollapseView {
  const hierarchy = buildHierarchy(nodes, edges);
  const visible = visibleNodeIds(nodes, hierarchy, collapsed);
  const visNodes = nodes.filter((n) => visible.has(n.id));
  const visEdges = liftEdges(edges, visible, hierarchy);
  return { nodes: visNodes, edges: visEdges, visible, hierarchy };
}

export function anchorOffset(
  anchorId: string | null,
  oldPos: Record<string, { x: number; y: number }>,
  newPos: Record<string, { x: number; y: number }>,
): { dx: number; dy: number } {
  if (!anchorId) return { dx: 0, dy: 0 };
  const o = oldPos[anchorId];
  const n = newPos[anchorId];
  if (!o || !n) return { dx: 0, dy: 0 };
  return { dx: o.x - n.x, dy: o.y - n.y };
}

export function collapseTopologyKey(
  nodes: { id: string }[],
  edges: { id: string }[],
): string {
  return JSON.stringify([
    [...nodes.map((n) => n.id)].sort(),
    [...edges.map((e) => e.id)].sort(),
  ]);
}
