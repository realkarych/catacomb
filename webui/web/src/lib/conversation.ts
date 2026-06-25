export function isConversationNode(nodeType: string): boolean {
  return nodeType === 'user_prompt' || nodeType === 'assistant_turn';
}

export function conversationText(value: unknown): string {
  if (value === undefined || value === null) {
    return '';
  }

  if (typeof value === 'string') {
    return value;
  }

  return JSON.stringify(value, null, 2);
}
