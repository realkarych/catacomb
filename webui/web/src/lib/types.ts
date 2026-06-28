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
  step_key?: string;
  step_key_method?: string;
  phase_key?: string;
  annotations?: Record<string, unknown>;
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

export interface ReproMeta {
  claude_code_version?: string;
  catacomb_version?: string;
  cwd?: string;
  prompts_hash?: string;
  skills_hash?: string;
  subagents_hash?: string;
  catacomb_config_hash?: string;
}

export interface SessionSummary {
  session: string;
  label?: string;
  status: string;
  started_at?: string;
  ended_at?: string;
  last_activity?: string;
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
  repro?: ReproMeta;
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

export interface DiffStringChange {
  before: string;
  after: string;
}

export interface DiffNumberChange {
  before: number;
  after: number;
  delta: number;
}

export interface DiffDeltas {
  args?: DiffStringChange;
  status?: DiffStringChange;
  cost_usd?: DiffNumberChange;
  duration_ms?: DiffNumberChange;
  tokens_in?: DiffNumberChange;
  tokens_out?: DiffNumberChange;
}

export interface DiffStep {
  type: string;
  tool: string;
  step_key: string;
  content_key: string;
}

export interface DiffMatch {
  type: string;
  tool: string;
  a_step_key: string;
  b_step_key: string;
  a_content_key: string;
  b_content_key: string;
  tier: string;
}

export interface DiffChangedStep extends DiffMatch {
  deltas: DiffDeltas;
}

export interface DiffResult {
  added: DiffStep[];
  removed: DiffStep[];
  changed: DiffChangedStep[];
  unchanged: DiffMatch[];
}
