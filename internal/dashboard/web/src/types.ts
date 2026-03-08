export type TicketStatus =
  | 'queued' | 'clarification_needed' | 'planning' | 'plan_validating'
  | 'implementing' | 'reviewing' | 'pr_created' | 'done' | 'partial'
  | 'failed' | 'blocked' | 'decomposing' | 'decomposed'
  | 'awaiting_merge' | 'merged' | 'pr_closed' | 'pr_updated';

export type TaskStatus =
  | 'pending' | 'implementing' | 'tdd_verifying' | 'testing'
  | 'spec_review' | 'quality_review' | 'done' | 'failed' | 'skipped' | 'escalated';

export const ACTIVE_STATUSES: TicketStatus[] = [
  'planning', 'plan_validating', 'implementing', 'reviewing',
  'pr_created', 'awaiting_merge', 'clarification_needed', 'decomposing',
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
  Comments: TicketComment[];
  Labels: string[];
  ChildTicketIDs: string[];
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
  EstimatedComplexity: string;
  DependsOn: string[];
  FilesToModify: string[];
  FilesToRead: string[];
  AcceptanceCriteria: string[];
  TestAssertions: string[];
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
}

export interface LlmCallRecord {
  ID: string;
  TicketID: string;
  TaskID: string;
  Role: string;
  Provider: string;
  Model: string;
  Stage: string;
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
