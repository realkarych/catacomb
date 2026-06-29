import { describe, it, expect } from 'vitest';
import {
  isConversationNode,
  conversationText,
  isToolNode,
  toolKeyArg,
  toolOutputSnippet,
  cleanRedacted,
} from './conversation';

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

describe('isToolNode', () => {
  it('returns true for tool_call', () => {
    expect(isToolNode('tool_call')).toBe(true);
  });

  it('returns true for mcp_call', () => {
    expect(isToolNode('mcp_call')).toBe(true);
  });

  it('returns true for skill', () => {
    expect(isToolNode('skill')).toBe(true);
  });

  it('returns false for user_prompt', () => {
    expect(isToolNode('user_prompt')).toBe(false);
  });

  it('returns false for assistant_turn', () => {
    expect(isToolNode('assistant_turn')).toBe(false);
  });

  it('returns false for empty string', () => {
    expect(isToolNode('')).toBe(false);
  });
});

describe('toolKeyArg', () => {
  it('returns the Bash command', () => {
    expect(toolKeyArg({ command: 'ls -la /tmp' })).toBe('ls -la /tmp');
  });

  it('returns the file_path for Read/Edit/Write', () => {
    expect(toolKeyArg({ file_path: '/src/components/Foo.svelte' })).toBe('/src/components/Foo.svelte');
  });

  it('prefers command over file_path when both present', () => {
    expect(toolKeyArg({ file_path: '/x', command: 'echo hi' })).toBe('echo hi');
  });

  it('returns the path key', () => {
    expect(toolKeyArg({ path: '/etc/hosts' })).toBe('/etc/hosts');
  });

  it('returns the url key', () => {
    expect(toolKeyArg({ url: 'https://example.com' })).toBe('https://example.com');
  });

  it('returns the query key', () => {
    expect(toolKeyArg({ query: 'select 1' })).toBe('select 1');
  });

  it('joins compact key args for mcp/other inputs', () => {
    expect(toolKeyArg({ owner: 'octo', repo: 'cat' })).toBe('owner: octo · repo: cat');
  });

  it('skips empty and non-scalar values when joining', () => {
    expect(toolKeyArg({ a: '', b: 'x', c: { nested: 1 }, d: 7 })).toBe('b: x · d: 7');
  });

  it('returns empty string for an empty object', () => {
    expect(toolKeyArg({})).toBe('');
  });

  it('returns empty string for null', () => {
    expect(toolKeyArg(null)).toBe('');
  });

  it('returns empty string for undefined', () => {
    expect(toolKeyArg(undefined)).toBe('');
  });

  it('returns empty string for a non-object', () => {
    expect(toolKeyArg('a string')).toBe('');
  });

  it('returns empty string for an array', () => {
    expect(toolKeyArg(['a', 'b'])).toBe('');
  });

  it('coerces a preferred numeric/boolean value to string', () => {
    expect(toolKeyArg({ command: 0 })).toBe('0');
    expect(toolKeyArg({ command: true })).toBe('true');
  });

  it('falls through to the join when a preferred key holds a non-scalar', () => {
    expect(toolKeyArg({ command: { nested: 1 }, repo: 'cat' })).toBe('repo: cat');
  });

  it('truncates a long key arg', () => {
    const long = 'x'.repeat(120);
    const out = toolKeyArg({ command: long });
    expect(out.length).toBe(81);
    expect(out.endsWith('…')).toBe(true);
  });
});

describe('toolOutputSnippet', () => {
  it('returns the first non-empty line of a string', () => {
    expect(toolOutputSnippet('\n\n  hello world  \nsecond')).toBe('hello world');
  });

  it('reads stdout from an object', () => {
    expect(toolOutputSnippet({ stdout: 'exit 0\nmore' })).toBe('exit 0');
  });

  it('reads content from an object', () => {
    expect(toolOutputSnippet({ content: 'file body' })).toBe('file body');
  });

  it('reads result from an object', () => {
    expect(toolOutputSnippet({ result: 'done' })).toBe('done');
  });

  it('reads text from an object', () => {
    expect(toolOutputSnippet({ text: 'a line' })).toBe('a line');
  });

  it('flattens a content-block array via conversationText', () => {
    expect(toolOutputSnippet([{ type: 'text', text: 'block line' }])).toBe('block line');
  });

  it('returns empty string for an object with no known keys', () => {
    expect(toolOutputSnippet({ foo: 'bar' })).toBe('');
  });

  it('returns empty string for null', () => {
    expect(toolOutputSnippet(null)).toBe('');
  });

  it('returns empty string for undefined', () => {
    expect(toolOutputSnippet(undefined)).toBe('');
  });

  it('returns empty string when every line is blank', () => {
    expect(toolOutputSnippet('   \n\t\n  ')).toBe('');
  });

  it('truncates a long first line to ~80 chars', () => {
    const long = 'y'.repeat(200);
    const out = toolOutputSnippet(long);
    expect(out.length).toBe(81);
    expect(out.endsWith('…')).toBe(true);
  });

  it('ignores a non-string known key and falls through to empty', () => {
    expect(toolOutputSnippet({ stdout: 42 })).toBe('');
  });
});

describe('cleanRedacted', () => {
  it('returns [redacted] when text is the high-entropy placeholder', () => {
    expect(cleanRedacted('‹redacted:high-entropy›')).toBe('[redacted]');
  });

  it('returns [redacted] for any redaction reason', () => {
    expect(cleanRedacted('‹redacted:aws-key›')).toBe('[redacted]');
  });

  it('returns [redacted] when the placeholder is embedded in a line', () => {
    expect(cleanRedacted('token=‹redacted:github-token›')).toBe('[redacted]');
  });

  it('returns [redacted] for a binary placeholder', () => {
    expect(cleanRedacted('‹binary:12,deadbeef›')).toBe('[redacted]');
  });

  it('passes through normal text unchanged', () => {
    expect(cleanRedacted('hello world')).toBe('hello world');
  });

  it('passes through empty string unchanged', () => {
    expect(cleanRedacted('')).toBe('');
  });
});
