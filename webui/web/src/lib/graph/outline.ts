import type { Node, Hierarchy } from './types';
import { isLazySubagent } from './aggregate';

export interface OutlineRow {
  id: string;
  node: Node;
  depth: number;
  hasChildren: boolean;
  collapsed: boolean;
}

export function flattenOutline(
  nodes: Node[],
  hierarchy: Hierarchy,
  collapsed: Set<string>,
): OutlineRow[] {
  const byId = new Map<string, Node>(nodes.map((nd) => [nd.id, nd]));
  const rows: OutlineRow[] = [];
  const visited = new Set<string>();

  const visit = (id: string, depth: number): void => {
    if (visited.has(id)) return;
    visited.add(id);
    const node = byId.get(id);
    const children = hierarchy.childrenOf(id);
    if (node) {
      rows.push({
        id,
        node,
        depth,
        hasChildren: children.length > 0,
        collapsed: collapsed.has(id),
      });
    }
    if (!collapsed.has(id)) {
      const childDepth = node ? depth + 1 : depth;
      for (const childId of children) {
        visit(childId, childDepth);
      }
    }
  };

  for (const id of hierarchy.roots) {
    visit(id, 0);
  }
  for (const id of hierarchy.orphans) {
    visit(id, 0);
  }

  return rows;
}

export function defaultOutlineCollapsed(nodes: Node[], hierarchy: Hierarchy): Set<string> {
  const rootSet = new Set(hierarchy.roots);
  const out = new Set<string>();
  for (const node of nodes) {
    if (rootSet.has(node.id)) continue;
    if (hierarchy.childrenOf(node.id).length > 0 || isLazySubagent(node)) out.add(node.id);
  }
  return out;
}

export function outlineLabel(node: Node): { primary: string; secondary: string } {
  switch (node.type) {
    case 'session':
      return { primary: node.name || 'session', secondary: '' };
    case 'user_prompt':
      return { primary: 'prompt', secondary: '' };
    case 'assistant_turn':
      return {
        primary: 'assistant',
        secondary: String(node.attrs?.model ?? node.attrs?.model_id ?? ''),
      };
    case 'tool_call':
      return { primary: node.name || 'tool', secondary: '' };
    case 'mcp_call':
      return { primary: node.name || 'mcp', secondary: '' };
    case 'subagent':
      return {
        primary: node.name || 'subagent',
        secondary: node.subagent_type || String(node.attrs?.subagent_type ?? ''),
      };
    default:
      return { primary: node.name || node.type, secondary: '' };
  }
}

export function isSystemPrompt(node: Node): boolean {
  return node.type === 'user_prompt' && node.attrs?.['prompt_kind'] === 'system';
}
