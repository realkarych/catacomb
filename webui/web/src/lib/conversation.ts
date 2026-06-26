export function isConversationNode(nodeType: string): boolean {
  return nodeType === 'user_prompt' || nodeType === 'assistant_turn';
}

export function isToolNode(nodeType: string): boolean {
  return nodeType === 'tool_call' || nodeType === 'mcp_call';
}

type ContentBlock = { type: string; text?: string };

function isContentBlockArray(value: unknown): value is ContentBlock[] {
  return (
    Array.isArray(value) &&
    value.length > 0 &&
    value.every((el) => typeof el === 'object' && el !== null && 'type' in el)
  );
}

export function conversationText(value: unknown): string {
  if (value === undefined || value === null) {
    return '';
  }

  if (typeof value === 'string') {
    return value;
  }

  if (Array.isArray(value) && value.length === 0) {
    return '';
  }

  if (isContentBlockArray(value)) {
    return value
      .filter((block) => block.type === 'text')
      .map((block) => block.text ?? '')
      .join('');
  }

  return JSON.stringify(value, null, 2);
}

const KEY_ARG_MAX = 80;
const OUTPUT_MAX = 80;
const PREFERRED_KEYS = ['command', 'file_path', 'path', 'url', 'query', 'pattern', 'prompt'];
const OUTPUT_KEYS = ['stdout', 'content', 'result', 'output', 'text'];
const REDACTION_MARKERS = ['‹redacted:', '‹binary:'];

function truncate(text: string, limit: number): string {
  return text.length > limit ? text.slice(0, limit) + '…' : text;
}

function scalarToString(value: unknown): string {
  if (typeof value === 'string') return value;
  if (typeof value === 'number') return String(value);
  if (typeof value === 'boolean') return String(value);
  return '';
}

function isPlainObject(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

export function toolKeyArg(input: unknown): string {
  if (!isPlainObject(input)) return '';

  for (const key of PREFERRED_KEYS) {
    if (key in input) {
      const text = scalarToString(input[key]).trim();
      if (text) return truncate(text, KEY_ARG_MAX);
    }
  }

  const parts: string[] = [];
  for (const [key, value] of Object.entries(input)) {
    const text = scalarToString(value).trim();
    if (text) parts.push(`${key}: ${text}`);
  }
  return truncate(parts.join(' · '), KEY_ARG_MAX);
}

export function toolOutputSnippet(output: unknown): string {
  let raw = '';
  if (typeof output === 'string') {
    raw = output;
  } else if (isContentBlockArray(output)) {
    raw = conversationText(output);
  } else if (isPlainObject(output)) {
    for (const key of OUTPUT_KEYS) {
      if (typeof output[key] === 'string') {
        raw = output[key];
        break;
      }
    }
  }

  for (const line of raw.split('\n')) {
    const trimmed = line.trim();
    if (trimmed) return truncate(trimmed, OUTPUT_MAX);
  }
  return '';
}

export function cleanRedacted(text: string): string {
  for (const marker of REDACTION_MARKERS) {
    if (text.includes(marker)) return '[redacted]';
  }
  return text;
}
