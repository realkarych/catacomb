export type Route =
  | { kind: 'list' }
  | { kind: 'session'; hash: string }
  | { kind: 'session-node'; hash: string; nodeId: string }
  | { kind: 'diff'; a?: string; b?: string };

export function parseHash(hash: string): Route {
  const path = hash.startsWith('#') ? hash.slice(1) : hash;
  const parts = path.split('/').filter(Boolean);
  if (parts.length === 2 && parts[0] === 's' && parts[1] !== undefined) {
    return { kind: 'session', hash: decodeURIComponent(parts[1]) };
  }
  if (parts.length === 4 && parts[0] === 's' && parts[2] === 'n' && parts[1] !== undefined && parts[3] !== undefined) {
    return {
      kind: 'session-node',
      hash: decodeURIComponent(parts[1]),
      nodeId: decodeURIComponent(parts[3]),
    };
  }
  if (parts.length === 1 && parts[0] === 'diff') {
    return { kind: 'diff' };
  }
  if (parts.length === 2 && parts[0] === 'diff' && parts[1] !== undefined) {
    return { kind: 'diff', a: decodeURIComponent(parts[1]) };
  }
  if (parts.length === 3 && parts[0] === 'diff' && parts[1] !== undefined && parts[2] !== undefined) {
    return { kind: 'diff', a: decodeURIComponent(parts[1]), b: decodeURIComponent(parts[2]) };
  }
  return { kind: 'list' };
}

export function toHash(route: Route): string {
  if (route.kind === 'list') return '#/';
  if (route.kind === 'session') return `#/s/${encodeURIComponent(route.hash)}`;
  if (route.kind === 'diff') {
    if (route.a === undefined) return '#/diff';
    if (route.b === undefined) return `#/diff/${encodeURIComponent(route.a)}`;
    return `#/diff/${encodeURIComponent(route.a)}/${encodeURIComponent(route.b)}`;
  }
  return `#/s/${encodeURIComponent(route.hash)}/n/${encodeURIComponent(route.nodeId)}`;
}
