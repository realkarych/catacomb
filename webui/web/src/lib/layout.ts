import dagre from '@dagrejs/dagre';
import type { Node as CNode, Edge as CEdge } from './types';

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
