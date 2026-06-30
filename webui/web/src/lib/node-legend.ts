export interface NodeTypeInfo {
  token: string;
  label: string;
}

const NODE_TYPE_MAP: Record<string, NodeTypeInfo> = {
  session: { token: '--node-session', label: 'session' },
  user_prompt: { token: '--node-user_prompt', label: 'user prompt' },
  assistant_turn: { token: '--node-assistant_turn', label: 'assistant turn' },
  tool_call: { token: '--node-tool_call', label: 'tool call' },
  subagent: { token: '--node-subagent', label: 'subagent' },
  mcp_call: { token: '--node-mcp_call', label: 'mcp call' },
  skill: { token: '--node-skill', label: 'skill' },
  hook_event: { token: '--node-hook_event', label: 'hook event' },
  marker: { token: '--node-marker', label: 'marker' },
};

const FALLBACK: NodeTypeInfo = { token: '--node-marker', label: 'marker' };

export function nodeTypeInfo(type: string): NodeTypeInfo {
  return NODE_TYPE_MAP[type] ?? FALLBACK;
}

export function presentNodeTypes(types: string[]): NodeTypeInfo[] {
  const seen = new Set<string>();
  const result: NodeTypeInfo[] = [];
  for (const t of types) {
    const key = NODE_TYPE_MAP[t] ? t : 'marker';
    if (!seen.has(key)) {
      seen.add(key);
      result.push(nodeTypeInfo(key));
    }
  }
  return result;
}
