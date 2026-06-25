import type { Node, Edge, Hierarchy } from './types';
import { buildHierarchy } from './hierarchy';

function chronoKey(node: Node): string {
  return node.t_start ?? node.id;
}

function compareKeyed(aKey: string, aId: string, bKey: string, bId: string): number {
  if (aKey !== bKey) return aKey < bKey ? -1 : 1;
  return aId < bId ? -1 : 1;
}

export function buildOutlineHierarchy(nodes: Node[], edges: Edge[]): Hierarchy {
  const base = buildHierarchy(nodes, edges);
  const present = new Set(nodes.map((nd) => nd.id));
  const byId = new Map<string, Node>(nodes.map((nd) => [nd.id, nd]));

  const sessionByRun = new Map<string, string>();
  for (const nd of nodes) {
    if (nd.type !== 'session') continue;
    const existing = sessionByRun.get(nd.run_id);
    if (existing === undefined || nd.id < existing) sessionByRun.set(nd.run_id, nd.id);
  }

  const promptsByRun = new Map<string, Node[]>();
  for (const nd of nodes) {
    if (nd.type !== 'user_prompt') continue;
    const arr = promptsByRun.get(nd.run_id);
    if (arr) arr.push(nd);
    else promptsByRun.set(nd.run_id, [nd]);
  }

  const parent = new Map<string, string>();
  for (const nd of nodes) {
    const p = base.parentOf(nd.id);
    if (p !== undefined) parent.set(nd.id, p);
  }

  const wouldCycle = (childId: string, parentId: string): boolean => {
    const seen = new Set<string>();
    let cur: string | undefined = parentId;
    while (cur !== undefined && !seen.has(cur)) {
      if (cur === childId) return true;
      seen.add(cur);
      cur = parent.get(cur);
    }
    return false;
  };

  const precedingPrompt = (turn: Node): string | undefined => {
    const prompts = promptsByRun.get(turn.run_id);
    if (!prompts) return undefined;
    const tKey = chronoKey(turn);
    let best: Node | undefined;
    for (const p of prompts) {
      const pKey = chronoKey(p);
      if (pKey > tKey) continue;
      if (best === undefined || compareKeyed(chronoKey(best), best.id, pKey, p.id) < 0) best = p;
    }
    return best?.id;
  };

  const ordered = [...nodes].sort((a, b) => (a.id < b.id ? -1 : 1));
  for (const nd of ordered) {
    if (parent.has(nd.id)) continue;
    if (nd.type === 'session') continue;
    let candidate: string | undefined;
    if (nd.type === 'assistant_turn') {
      candidate = precedingPrompt(nd) ?? sessionByRun.get(nd.run_id);
    } else {
      candidate = sessionByRun.get(nd.run_id);
    }
    if (candidate === undefined || !present.has(candidate)) continue;
    if (wouldCycle(nd.id, candidate)) continue;
    parent.set(nd.id, candidate);
  }

  const childNodes = new Map<string, Node[]>();
  for (const nd of nodes) {
    const p = parent.get(nd.id);
    if (p === undefined) continue;
    const arr = childNodes.get(p);
    if (arr) arr.push(nd);
    else childNodes.set(p, [nd]);
  }
  const children = new Map<string, string[]>();
  for (const [p, arr] of childNodes) {
    arr.sort((a, b) => compareKeyed(chronoKey(a), a.id, chronoKey(b), b.id));
    children.set(p, arr.map((nd) => nd.id));
  }

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
