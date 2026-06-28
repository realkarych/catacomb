import { describe, it, expect } from 'vitest';
import { rowStatLine } from './outline-stats';
import type { Node, Aggregate } from './types';

function node(extra: Partial<Node> = {}): Node {
  return { id: 'x', run_id: 'r1', type: 'assistant_turn', rev: 1, ...extra };
}

function agg(extra: Partial<Aggregate> = {}): Aggregate {
  return {
    count: 0,
    tokensIn: 0,
    tokensOut: 0,
    costUsd: 0,
    status: 'ok',
    hasError: false,
    durationMs: 0,
    ...extra,
  };
}

describe('rowStatLine — collapsed aggregate', () => {
  it('formats node count, in/out tokens and cost with no bare arrow', () => {
    const res = rowStatLine(node({ type: 'user_prompt' }), {
      collapsed: true,
      hasChildren: true,
      aggregate: agg({ count: 3, tokensIn: 1500, tokensOut: 12000, costUsd: 0.0234 }),
    });
    expect(res.text).toBe('3 nodes · in 1,500 · out 12.0k · $0.02');
    expect(res.text).not.toContain('→');
  });

  it('appends the wall-clock duration when durationMs is positive', () => {
    const res = rowStatLine(node({ type: 'user_prompt' }), {
      collapsed: true,
      hasChildren: true,
      aggregate: agg({ count: 3, tokensIn: 1500, tokensOut: 12000, costUsd: 0.0234, durationMs: 90000 }),
    });
    expect(res.text).toBe('3 nodes · in 1,500 · out 12.0k · $0.02 · 1m 30s');
    expect(res.title).toContain('duration 1m 30s');
  });

  it('omits the duration when durationMs is zero', () => {
    const res = rowStatLine(node({ type: 'user_prompt' }), {
      collapsed: true,
      hasChildren: true,
      aggregate: agg({ count: 3, tokensIn: 1500, tokensOut: 12000, costUsd: 0.0234, durationMs: 0 }),
    });
    expect(res.text).toBe('3 nodes · in 1,500 · out 12.0k · $0.02');
    expect(res.title).not.toContain('duration');
  });

  it('carries a tooltip explaining the rollup', () => {
    const res = rowStatLine(node({ type: 'session' }), {
      collapsed: true,
      hasChildren: true,
      aggregate: agg({ count: 1, tokensIn: 5, tokensOut: 7, costUsd: 0.0005 }),
    });
    expect(res.title.length).toBeGreaterThan(0);
    expect(res.title).toContain('1');
  });

  it('maps aggregate error status to the error color', () => {
    const res = rowStatLine(node({ type: 'session' }), {
      collapsed: true,
      hasChildren: true,
      aggregate: agg({ count: 2, status: 'error', hasError: true }),
    });
    expect(res.color).toBe('var(--error)');
  });

  it('maps aggregate running status to the running color', () => {
    const res = rowStatLine(node({ type: 'session' }), {
      collapsed: true,
      hasChildren: true,
      aggregate: agg({ count: 2, status: 'running' }),
    });
    expect(res.color).toBe('var(--running)');
  });

  it('maps aggregate ok status to the ok color', () => {
    const res = rowStatLine(node({ type: 'session' }), {
      collapsed: true,
      hasChildren: true,
      aggregate: agg({ count: 2, status: 'ok' }),
    });
    expect(res.color).toBe('var(--ok)');
  });
});

describe('rowStatLine — assistant_turn leaf', () => {
  it('shows labeled in/out tokens, cost and duration with no bare arrow', () => {
    const res = rowStatLine(
      node({ type: 'assistant_turn', tokens_in: 9740, tokens_out: 58300, cost_usd: 1.51, duration_ms: 1200 }),
      { collapsed: false, hasChildren: false },
    );
    expect(res.text).toBe('in 9,740 · out 58.3k · $1.51 · 1.2s');
    expect(res.text).not.toContain('→');
  });

  it('omits absent pieces', () => {
    const res = rowStatLine(node({ type: 'assistant_turn', tokens_out: 40 }), {
      collapsed: false,
      hasChildren: false,
    });
    expect(res.text).toBe('out 40');
  });

  it('includes zero tokens explicitly', () => {
    const res = rowStatLine(node({ type: 'assistant_turn', tokens_in: 0, tokens_out: 0 }), {
      collapsed: false,
      hasChildren: false,
    });
    expect(res.text).toBe('in 0 · out 0');
  });

  it('returns empty string when an assistant turn has no metrics', () => {
    const res = rowStatLine(node({ type: 'assistant_turn' }), { collapsed: false, hasChildren: false });
    expect(res.text).toBe('');
  });

  it('provides a human-readable tooltip', () => {
    const res = rowStatLine(
      node({ type: 'assistant_turn', tokens_in: 9740, tokens_out: 58300, cost_usd: 1.51, duration_ms: 1200 }),
      { collapsed: false, hasChildren: false },
    );
    expect(res.title).toContain('input');
    expect(res.title).toContain('output');
  });

  it('uses the error color for an error turn', () => {
    const res = rowStatLine(node({ type: 'assistant_turn', status: 'error', tokens_in: 1 }), {
      collapsed: false,
      hasChildren: false,
    });
    expect(res.color).toBe('var(--error)');
  });

  it('uses the running color for a running turn', () => {
    const res = rowStatLine(node({ type: 'assistant_turn', status: 'running', tokens_in: 1 }), {
      collapsed: false,
      hasChildren: false,
    });
    expect(res.color).toBe('var(--running)');
  });

  it('uses the blocked color for a blocked turn', () => {
    const res = rowStatLine(node({ type: 'assistant_turn', status: 'blocked', tokens_in: 1 }), {
      collapsed: false,
      hasChildren: false,
    });
    expect(res.color).toBe('var(--blocked)');
  });
});

describe('rowStatLine — tool / mcp leaf', () => {
  it('shows duration only, no token columns and no arrow', () => {
    const res = rowStatLine(
      node({ type: 'tool_call', duration_ms: 307, tokens_in: 10, tokens_out: 5 }),
      { collapsed: false, hasChildren: false },
    );
    expect(res.text).toBe('307ms');
    expect(res.text).not.toContain('→');
    expect(res.text).not.toContain('in');
    expect(res.text).not.toContain('$');
  });

  it('shows duration for an mcp_call', () => {
    const res = rowStatLine(node({ type: 'mcp_call', duration_ms: 1500 }), {
      collapsed: false,
      hasChildren: false,
    });
    expect(res.text).toBe('1.5s');
  });

  it('returns an empty string when a tool has no duration', () => {
    const res = rowStatLine(node({ type: 'tool_call' }), { collapsed: false, hasChildren: false });
    expect(res.text).toBe('');
  });

  it('carries a duration tooltip when present', () => {
    const res = rowStatLine(node({ type: 'tool_call', duration_ms: 307 }), {
      collapsed: false,
      hasChildren: false,
    });
    expect(res.title).toContain('Duration');
  });

  it('uses the error color for a failed tool', () => {
    const res = rowStatLine(node({ type: 'tool_call', status: 'error', duration_ms: 5 }), {
      collapsed: false,
      hasChildren: false,
    });
    expect(res.color).toBe('var(--error)');
  });
});

describe('rowStatLine — empty leaf', () => {
  it('returns an empty string and empty tooltip for a user_prompt leaf', () => {
    const res = rowStatLine(node({ type: 'user_prompt' }), { collapsed: false, hasChildren: false });
    expect(res.text).toBe('');
    expect(res.title).toBe('');
  });

  it('returns an empty string for an unknown leaf type', () => {
    const res = rowStatLine(node({ type: 'marker' }), { collapsed: false, hasChildren: false });
    expect(res.text).toBe('');
  });

  it('treats an expanded parent (hasChildren but not collapsed) as a leaf for its own row', () => {
    const res = rowStatLine(node({ type: 'session' }), { collapsed: false, hasChildren: true });
    expect(res.text).toBe('');
  });

  it('treats a collapsed node without an aggregate as a plain leaf', () => {
    const res = rowStatLine(node({ type: 'session' }), { collapsed: true, hasChildren: true });
    expect(res.text).toBe('');
  });

  it('returns transparent color for a user_prompt with no status', () => {
    const res = rowStatLine(node({ type: 'user_prompt' }), { collapsed: false, hasChildren: false });
    expect(res.color).toBe('transparent');
  });

  it('returns transparent color for unknown status', () => {
    const res = rowStatLine(node({ type: 'assistant_turn', status: 'unknown' }), {
      collapsed: false,
      hasChildren: false,
    });
    expect(res.color).toBe('transparent');
  });

  it('returns transparent color for pending status', () => {
    const res = rowStatLine(node({ type: 'assistant_turn', status: 'pending' }), {
      collapsed: false,
      hasChildren: false,
    });
    expect(res.color).toBe('transparent');
  });
});
