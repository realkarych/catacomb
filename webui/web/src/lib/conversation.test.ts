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
});
