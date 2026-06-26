import { describe, it, expect } from 'vitest';
import { isConversationNode, conversationText } from './conversation';

describe('isConversationNode', () => {
  it('returns true for user_prompt', () => {
    expect(isConversationNode('user_prompt')).toBe(true);
  });

  it('returns true for assistant_turn', () => {
    expect(isConversationNode('assistant_turn')).toBe(true);
  });

  it('returns false for tool_call', () => {
    expect(isConversationNode('tool_call')).toBe(false);
  });

  it('returns false for subagent', () => {
    expect(isConversationNode('subagent')).toBe(false);
  });

  it('returns false for unknown node type', () => {
    expect(isConversationNode('unknown_type')).toBe(false);
  });

  it('returns false for empty string', () => {
    expect(isConversationNode('')).toBe(false);
  });

  it('returns false for mcp_call', () => {
    expect(isConversationNode('mcp_call')).toBe(false);
  });
});

describe('conversationText', () => {
  it('returns empty string for undefined', () => {
    expect(conversationText(undefined)).toBe('');
  });

  it('returns empty string for null', () => {
    expect(conversationText(null)).toBe('');
  });

  it('returns string as-is for string input', () => {
    expect(conversationText('hello world')).toBe('hello world');
  });

  it('preserves newlines in string input', () => {
    expect(conversationText('line 1\nline 2\nline 3')).toBe('line 1\nline 2\nline 3');
  });

  it('returns JSON stringified for object', () => {
    expect(conversationText({ text: 'hello' })).toBe('{\n  "text": "hello"\n}');
  });

  it('returns JSON stringified for array', () => {
    expect(conversationText(['a', 'b', 'c'])).toBe('[\n  "a",\n  "b",\n  "c"\n]');
  });

  it('handles object with nested structure', () => {
    const obj = {
      type: 'text',
      content: {
        message: 'test',
        multiline: 'line 1\nline 2',
      },
    };
    const result = conversationText(obj);
    expect(result).toContain('"type": "text"');
    expect(result).toContain('"message": "test"');
  });

  it('returns empty string for empty string input', () => {
    expect(conversationText('')).toBe('');
  });

  it('handles whitespace-only string', () => {
    expect(conversationText('   \n\t  ')).toBe('   \n\t  ');
  });

  it('returns string representation for number', () => {
    expect(conversationText(42)).toBe('42');
  });

  it('returns string representation for boolean', () => {
    expect(conversationText(true)).toBe('true');
  });

  it('single text block array returns its text', () => {
    expect(conversationText([{ type: 'text', text: 'Hello world' }])).toBe('Hello world');
  });

  it('multiple text blocks are concatenated without separator', () => {
    expect(
      conversationText([
        { type: 'text', text: 'Hello' },
        { type: 'text', text: ' world' },
      ])
    ).toBe('Hello world');
  });

  it('mixed text and tool_use blocks returns only text blocks concatenated', () => {
    expect(
      conversationText([
        { type: 'text', text: 'Before' },
        { type: 'tool_use', id: 'abc', name: 'fn', input: {} },
        { type: 'text', text: 'After' },
      ])
    ).toBe('BeforeAfter');
  });

  it('array of all tool_use blocks returns empty string', () => {
    expect(
      conversationText([
        { type: 'tool_use', id: 'x', name: 'fn', input: {} },
        { type: 'tool_result', tool_use_id: 'x', content: 'ok' },
      ])
    ).toBe('');
  });

  it('empty array returns empty string', () => {
    expect(conversationText([])).toBe('');
  });

  it('text block with missing text field contributes empty string', () => {
    expect(conversationText([{ type: 'text' }, { type: 'text', text: 'hi' }])).toBe('hi');
  });

  it('array where elements have no type field falls back to JSON.stringify', () => {
    expect(conversationText([{ text: 'hello' }, { text: 'world' }])).toBe(
      '[\n  {\n    "text": "hello"\n  },\n  {\n    "text": "world"\n  }\n]'
    );
  });
});
