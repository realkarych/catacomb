import type { Node } from './types';

export interface AnnotationEntry {
  key: string;
  value: string;
}

export function annotationEntries(node: Node): AnnotationEntry[] {
  if (!node.annotations) return [];
  const entries = Object.entries(node.annotations);
  if (entries.length === 0) return [];
  return entries
    .map(([key, val]) => ({
      key,
      value: typeof val === 'string' ? val : JSON.stringify(val),
    }))
    .sort((a, b) => a.key.localeCompare(b.key));
}
