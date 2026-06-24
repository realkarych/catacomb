import { describe, it, expect } from 'vitest';
import { nodeTypeInfo, presentNodeTypes } from './node-legend';

describe('nodeTypeInfo', () => {
  it('returns correct token and label for session', () => {
    expect(nodeTypeInfo('session')).toEqual({ token: '--node-session', label: 'session' });
  });

  it('returns correct token and label for user_prompt', () => {
    expect(nodeTypeInfo('user_prompt')).toEqual({ token: '--node-user_prompt', label: 'user prompt' });
  });

  it('returns correct token and label for assistant_turn', () => {
    expect(nodeTypeInfo('assistant_turn')).toEqual({ token: '--node-assistant_turn', label: 'assistant turn' });
  });

  it('returns correct token and label for tool_call', () => {
    expect(nodeTypeInfo('tool_call')).toEqual({ token: '--node-tool_call', label: 'tool call' });
  });

  it('returns correct token and label for subagent', () => {
    expect(nodeTypeInfo('subagent')).toEqual({ token: '--node-subagent', label: 'subagent' });
  });

  it('returns correct token and label for mcp_call', () => {
    expect(nodeTypeInfo('mcp_call')).toEqual({ token: '--node-mcp_call', label: 'mcp call' });
  });

  it('returns correct token and label for hook_event', () => {
    expect(nodeTypeInfo('hook_event')).toEqual({ token: '--node-hook_event', label: 'hook event' });
  });

  it('returns correct token and label for marker', () => {
    expect(nodeTypeInfo('marker')).toEqual({ token: '--node-marker', label: 'marker' });
  });

  it('returns fallback for unknown type', () => {
    expect(nodeTypeInfo('unknown_xyz')).toEqual({ token: '--node-marker', label: 'marker' });
  });
});

describe('presentNodeTypes', () => {
  it('returns empty array for empty input', () => {
    expect(presentNodeTypes([])).toEqual([]);
  });

  it('returns one entry for a single known type', () => {
    expect(presentNodeTypes(['session'])).toEqual([{ token: '--node-session', label: 'session' }]);
  });

  it('deduplicates the same type', () => {
    const result = presentNodeTypes(['session', 'session', 'session']);
    expect(result).toHaveLength(1);
    expect(result[0]).toEqual({ token: '--node-session', label: 'session' });
  });

  it('maps unknown type to marker and deduplicates with explicit marker', () => {
    const result = presentNodeTypes(['unknown', 'marker']);
    expect(result).toHaveLength(1);
    expect(result[0]).toEqual({ token: '--node-marker', label: 'marker' });
  });

  it('returns multiple distinct types in encounter order', () => {
    const result = presentNodeTypes(['tool_call', 'session', 'user_prompt']);
    expect(result).toEqual([
      { token: '--node-tool_call', label: 'tool call' },
      { token: '--node-session', label: 'session' },
      { token: '--node-user_prompt', label: 'user prompt' },
    ]);
  });

  it('handles all eight known types without duplicates', () => {
    const all = ['session', 'user_prompt', 'assistant_turn', 'tool_call', 'subagent', 'mcp_call', 'hook_event', 'marker'];
    const result = presentNodeTypes(all);
    expect(result).toHaveLength(8);
  });
});
