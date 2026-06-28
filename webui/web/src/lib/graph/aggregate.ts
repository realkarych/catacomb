import type { Node, Hierarchy, Aggregate } from './types';

export function aggregateOf(
  id: string,
  hierarchy: Hierarchy,
  byId: Record<string, Node>,
): Aggregate {
  let count = 0;
  let tokensIn = 0;
  let tokensOut = 0;
  let costUsd = 0;
  let anyError = false;
  let anyRunning = false;
  let minStart = Infinity;
  let maxEnd = -Infinity;

  const span = (node: Node): void => {
    if (node.t_start === undefined) return;
    const start = Date.parse(node.t_start);
    if (start < minStart) minStart = start;
    const end =
      node.t_end !== undefined
        ? Date.parse(node.t_end)
        : node.duration_ms !== undefined && node.duration_ms > 0
          ? start + node.duration_ms
          : start;
    if (end > maxEnd) maxEnd = end;
  };

  const root = byId[id];
  if (root) span(root);

  for (const descId of hierarchy.descendantsOf(id)) {
    const node = byId[descId];
    if (!node) continue;
    count += 1;
    tokensIn += node.tokens_in ?? 0;
    tokensOut += node.tokens_out ?? 0;
    costUsd += node.cost_usd ?? 0;
    if (node.status === 'error') anyError = true;
    else if (node.status === 'running') anyRunning = true;
    span(node);
  }

  const durationMs = minStart === Infinity ? 0 : Math.max(0, maxEnd - minStart);
  const status: Aggregate['status'] = anyError ? 'error' : anyRunning ? 'running' : 'ok';
  return { count, tokensIn, tokensOut, costUsd, status, hasError: anyError, durationMs };
}

function attrNumber(node: Node, key: string): number {
  const v = node.attrs?.[key];
  return typeof v === 'number' ? v : 0;
}

export function descendantCount(node: Node): number {
  return attrNumber(node, 'descendant_count');
}

export function isLazySubagent(node: Node): boolean {
  return node.type === 'subagent' && descendantCount(node) > 0;
}

function hasBackendCount(node: Node): boolean {
  return node.type === 'subagent' && typeof node.attrs?.descendant_count === 'number';
}

export function rowAggregate(
  node: Node,
  hierarchy: Hierarchy,
  byId: Record<string, Node>,
): Aggregate {
  const agg = aggregateOf(node.id, hierarchy, byId);
  if (agg.count > 0 || !hasBackendCount(node)) return agg;
  return {
    count: attrNumber(node, 'descendant_count'),
    tokensIn: attrNumber(node, 'descendant_tokens_in'),
    tokensOut: attrNumber(node, 'descendant_tokens_out'),
    costUsd: attrNumber(node, 'descendant_cost_usd'),
    status: 'ok',
    hasError: false,
    durationMs: 0,
  };
}
