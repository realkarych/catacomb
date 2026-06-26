export function isConversationNode(nodeType: string): boolean {
  return nodeType === 'user_prompt' || nodeType === 'assistant_turn';
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
