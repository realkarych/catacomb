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

  for (const descId of hierarchy.descendantsOf(id)) {
    const node = byId[descId];
    if (!node) continue;
    count += 1;
    tokensIn += node.tokens_in ?? 0;
    tokensOut += node.tokens_out ?? 0;
    costUsd += node.cost_usd ?? 0;
    if (node.status === 'error') anyError = true;
    else if (node.status === 'running') anyRunning = true;
  }

  const status: Aggregate['status'] = anyError ? 'error' : anyRunning ? 'running' : 'ok';
  return { count, tokensIn, tokensOut, costUsd, status, hasError: anyError };
}
