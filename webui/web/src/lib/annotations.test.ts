import { describe, it, expect } from 'vitest';
import { annotationEntries } from './annotations';
import type { Node } from './types';

function makeNode(annotations?: Record<string, unknown>): Node {
  return { id: 'n1', run_id: 'r1', type: 'tool', rev: 1, annotations };
}

describe('annotationEntries', () => {
  it('returns [] when annotations is absent', () => {
    expect(annotationEntries(makeNode())).toEqual([]);
  });

  it('returns [] when annotations is empty object', () => {
    expect(annotationEntries(makeNode({}))).toEqual([]);
  });

  it('returns single entry with string value', () => {
    expect(annotationEntries(makeNode({ foo: 'bar' }))).toEqual([{ key: 'foo', value: 'bar' }]);
  });

  it('sorts multiple entries alphabetically by key', () => {
    const result = annotationEntries(makeNode({ z: 'last', a: 'first', m: 'mid' }));
    expect(result).toEqual([
      { key: 'a', value: 'first' },
      { key: 'm', value: 'mid' },
      { key: 'z', value: 'last' },
    ]);
  });

  it('stringifies non-string values with JSON.stringify', () => {
    const result = annotationEntries(makeNode({ num: 42, obj: { x: 1 }, arr: [1, 2] }));
    expect(result).toEqual([
      { key: 'arr', value: '[1,2]' },
      { key: 'num', value: '42' },
      { key: 'obj', value: '{"x":1}' },
    ]);
  });

  it('handles mixed string and non-string values', () => {
    const result = annotationEntries(makeNode({ b: true, s: 'hello' }));
    expect(result).toEqual([
      { key: 'b', value: 'true' },
      { key: 's', value: 'hello' },
    ]);
  });
});
