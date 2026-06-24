export type CostSource = 'reported' | 'estimated';

export interface SseEvent {
  kind: string;
  rev: number;
  run_id?: string;
  execution_id?: string;
  node?: Node;
  edge?: Edge;
  old_id?: string;
}

export interface Node {
  id: string;
  run_id: string;
  type: string;
  parent_id?: string;
  agent_id?: string;
  parent_agent_id?: string;
  subagent_type?: string;
  name?: string;
  status?: string;
  t_start?: string;
  t_end?: string;
  duration_ms?: number;
  tokens_in?: number;
  tokens_out?: number;
  cost_usd?: number;
  attrs?: Record<string, unknown>;
  payload_hash?: string;
  sources?: { source: string; obs_id: string; observed_at: string }[];
  tier?: string;
  rev: number;
}

export interface Edge {
  id: string;
  run_id: string;
  type: string;
  src: string;
  dst: string;
  attrs?: Record<string, unknown>;
  rev: number;
}

export interface SessionSummary {
  session: string;
  status: string;
  started_at?: string;
  ended_at?: string;
  duration_ms?: number;
  tokens_in: number;
  tokens_out: number;
  cost_usd?: number;
  cost_source?: CostSource;
  node_count: number;
  tool_count: number;
  error_count: number;
  model_id?: string;
  run_ids: string[];
  counts_by_type?: Record<string, number>;
  counts_by_status?: Record<string, number>;
  error_rate?: number;
}

export interface RedactionFinding {
  path: string;
  reason: string;
}

export interface PayloadView {
  node_id: string;
  payload_hash?: string;
  input?: unknown;
  output?: unknown;
  redactions: RedactionFinding[];
  redacted: boolean;
}
