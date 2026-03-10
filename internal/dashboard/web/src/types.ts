export type TicketStatus =
  | 'queued' | 'clarification_needed' | 'planning' | 'plan_validating'
  | 'implementing' | 'reviewing' | 'pr_created' | 'done' | 'partial'
  | 'failed' | 'blocked' | 'decomposing' | 'decomposed'
  | 'awaiting_merge' | 'merged' | 'pr_closed' | 'pr_updated';

export type TaskStatus =
  | 'pending' | 'implementing' | 'tdd_verifying' | 'testing'
  | 'spec_review' | 'quality_review' | 'done' | 'failed' | 'skipped' | 'escalated';

export const PR_STATUSES: TicketStatus[] = ['pr_created', 'pr_updated', 'awaiting_merge', 'merged', 'pr_closed'];

export const ACTIVE_STATUSES: TicketStatus[] = [
  'queued', 'planning', 'plan_validating', 'implementing', 'reviewing',
  'pr_created', 'pr_updated', 'awaiting_merge', 'clarification_needed', 'decomposing',
];
export const DONE_STATUSES: TicketStatus[] = ['done', 'merged'];
export const FAIL_STATUSES: TicketStatus[] = ['failed', 'blocked', 'partial'];

export interface Ticket {
  ID: string;
  ExternalID: string;
  Title: string;
  Description: string;
  Status: TicketStatus;
  ChannelSenderID: string;
  PRURL: string;
  PRNumber: number;
  PRHeadSHA: string;
  CostUSD: number;
  TokensInput: number;
  TokensOutput: number;
  TotalLlmCalls: number;
  LastCompletedTaskSeq: number;
  CreatedAt: string;
  UpdatedAt: string;
  StartedAt: string | null;
  CompletedAt: string | null;
  ClarificationRequestedAt: string | null;
  ErrorMessage: string;
  Comments: TicketComment[] | null;
  Labels: string[] | null;
  ChildTicketIDs: string[] | null;
}

export interface TicketComment {
  Author: string;
  Body: string;
  CreatedAt: string;
}

export interface TicketSummary {
  ID: string;
  Title: string;
  Status: TicketStatus;
  ChannelSenderID: string;
  CostUSD: number;
  UpdatedAt: string;
  CreatedAt: string;
  StartedAt: string | null;
  CompletedAt: string | null;
  tasks_total: number;
  tasks_done: number;
}

export interface Task {
  ID: string;
  TicketID: string;
  Title: string;
  Description: string;
  Status: TaskStatus;
  Sequence: number;
  AgentRunner: string;  // "builtin" | "claudecode" | "copilot" | ""
  EstimatedComplexity: string;
  DependsOn: string[] | null;
  FilesToModify: string[] | null;
  FilesToRead: string[] | null;
  AcceptanceCriteria: string[] | null;
  TestAssertions: string[] | null;
  ImplementationAttempts: number;
  SpecReviewAttempts: number;
  QualityReviewAttempts: number;
  TotalLlmCalls: number;
  CostUSD: number;
  CommitSHA: string;
  ErrorMessage?: string;
  CreatedAt: string;
  StartedAt: string | null;
  CompletedAt: string | null;
}

export interface EventRecord {
  ID: string;
  TicketID: string;
  TaskID: string;
  EventType: string;
  Severity: 'info' | 'success' | 'warning' | 'error';
  Message: string;
  Details: string;
  CreatedAt: string;
  // Enriched by WebSocket
  ticket_title?: string;
  submitter?: string;
  seq?: number;
  isNew?: boolean;
  runner?: string;
  model?: string;
}

export interface LlmCallRecord {
  ID: string;
  TicketID: string;
  TaskID: string;
  Role: string;
  Provider: string;
  Model: string;
  Stage: string;
  AgentRunner: string;  // "builtin" | "claudecode" | "copilot" | ""
  TokensInput: number;
  TokensOutput: number;
  CostUSD: number;
  DurationMs: number;
  Status: string;
  Attempt: number;
}

export interface TeamStat {
  channel_sender_id: string;
  ticket_count: number;
  cost_usd: number;
  failed_count: number;
}

export interface DayCost {
  date: string;
  cost_usd: number;
}

export interface StatusResponse {
  daemon_state: string;
  version: string;
  channels: Record<string, { connected: boolean }>;
  mcp_servers?: Record<string, { status: string; error?: string }>;
}

export interface ConfigSummary {
  llm: { provider: string; models: Record<string, string>; api_key: string };
  tracker: { provider: string; poll_interval: string };
  git: { provider: string; clone_url: string; branch_prefix: string; auto_merge: boolean };
  agent_runner: { provider: string; max_turns: number; token_budget: number };
  cost: { daily_budget: number; monthly_budget: number; per_ticket_budget: number; alert_threshold: number };
  daemon: { max_parallel_tickets: number; max_parallel_tasks: number; work_dir: string; log_level: string };
  database: { driver: string; path: string };
  mcp: { servers: string[] };
  rate_limit: { requests_per_minute: number };
}

export interface ClaudeCodeUsage {
  available: boolean;
  estimate_note?: string;
  today?: { sessions: number; input_tokens: number; output_tokens: number; cache_read_tokens: number; estimated_cost_usd: number };
  last_7_days?: { date: string; input_tokens: number; output_tokens: number; cost_usd: number }[];
  total_sessions?: number;
}

export interface ActivityBreakdown {
  by_runner: { runner: string; calls: number; tokens_in: number; tokens_out: number; cost_usd: number }[];
  by_model: { model: string; calls: number; tokens_in: number; tokens_out: number; cost_usd: number }[];
  by_role: { role: string; runner: string; model: string; calls: number; cost_usd: number }[];
  recent_calls: {
    ticket_id: string; ticket_title: string; task_title: string;
    role: string; runner: string; model: string;
    tokens_in: number; tokens_out: number; cost_usd: number;
    status: string; duration_ms: number; timestamp: string;
  }[];
}

export interface ProjectEntry {
  id: string;
  name: string;
  created_at: string;
  active: boolean;
  status?: string;       // from worker: running, paused, error, stopped
  needsInput?: number;   // tickets needing clarification
}

export interface ProjectOverview {
  active_tickets: number;
  open_prs: number;
  need_input: number;
  cost_today: number;
  projects: number;
}

export interface ProjectSummary {
  project: ProjectEntry;
  active_tickets: number;
  open_prs: number;
  cost_today: number;
  status: string;
}

export interface ChatMessage {
  id: string;
  ticket_id: string;
  sender: 'agent' | 'user' | 'system';
  message_type: 'clarification' | 'action_request' | 'info' | 'error' | 'reply';
  content: string;
  metadata?: string;
  created_at: string;
}

export interface ProjectConfig {
  name: string;
  description: string;
  git_clone_url: string;
  git_default_branch: string;
  git_token: string;
  git_provider: string;
  tracker_provider: string;
  tracker_token: string;
  tracker_project_key: string;
  tracker_labels: string;
  tracker_url: string;
  tracker_email: string;
  agent_runner: string;
  model_planner: string;
  model_implementer: string;
  max_parallel_tickets: number;
  max_tasks_per_ticket: number;
  max_cost_per_ticket: number;
}
