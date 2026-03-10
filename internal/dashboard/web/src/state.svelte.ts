import { fetchJSON, postJSON, postJSONBody, deleteJSON, getToken, clearToken, setOnUnauthorized } from './api';
import type {
  Ticket, TicketSummary, Task, EventRecord, LlmCallRecord,
  TeamStat, DayCost, StatusResponse,
  ConfigSummary, ClaudeCodeUsage, ActivityBreakdown,
} from './types';

// ── Toasts ──
export interface Toast {
  id: string;
  message: string;
  ticketId?: string;
  severity: string;
  createdAt: number;
}

// ── Reactive State (class-based singleton to allow internal reassignment) ──
class AppState {
  authenticated = $state(!!getToken());
  daemonState = $state<string>('stopped');
  wsConnected = $state(false);
  whatsapp = $state<boolean | null>(null);
  mcpServers = $state<Record<string, { status: string; error?: string }>>({});

  dailyCost = $state(0);
  dailyBudget = $state(0);
  monthlyCost = $state(0);
  monthlyBudget = $state(0);
  weeklyCost = $state(0);
  weekDays = $state<DayCost[]>([]);

  tickets = $state<TicketSummary[]>([]);
  activeCount = $state(0);

  selectedTicketId = $state<string | null>(null);
  ticketDetail = $state<Ticket | null>(null);
  ticketTasks = $state<Task[]>([]);
  ticketLlmCalls = $state<LlmCallRecord[]>([]);
  ticketEvents = $state<EventRecord[]>([]);
  expandedTasks = $state<Record<string, boolean>>({});

  events = $state<EventRecord[]>([]);
  feedCollapsed = $state(localStorage.getItem('feed_collapsed') === 'true');

  teamStats = $state<TeamStat[]>([]);
  recentPRs = $state<Ticket[]>([]);

  toasts = $state<Toast[]>([]);

  settingsOpen = $state(false);
  settingsTab = $state<'config' | 'usage'>('config');
  configSummary = $state<ConfigSummary | null>(null);
  claudeCodeUsage = $state<ClaudeCodeUsage | null>(null);
  activityBreakdown = $state<ActivityBreakdown | null>(null);

  // Live task progress from WebSocket events
  activeTaskProgress = $state<Record<string, {
    turn: number;
    maxTurns: number;
    tokensUsed: number;
    runner: string;
    model: string;
    lastTool?: string;
    lastToolTime?: string;
  }>>({});

  activePanel = $state<'tickets' | 'detail' | 'feed' | 'health'>('tickets');
  filter = $state<'all' | 'active' | 'done' | 'fail'>('all');
  search = $state('');
}

export const appState = new AppState();

// ── Named re-exports for backward-compatible imports ──
// These are getter properties that forward to the class instance.
export const daemonState = { get value() { return appState.daemonState; } };

// Svelte 5 tracks access through the class instance (appState.field).
// Components MUST import `appState` and use `appState.field` directly.
// The named exports below are provided as aliases for migration ease:
// use `import { appState } from '../state.svelte'` and reference `appState.tickets` etc.

// ── UI Actions ──

export function setActivePanel(panel: 'tickets' | 'detail' | 'feed' | 'health') {
  appState.activePanel = panel;
}

export function setFilter(f: 'all' | 'active' | 'done' | 'fail') {
  appState.filter = f;
}

export function setSearch(str: string) {
  appState.search = str;
}

export function setFeedCollapsed(v: boolean) {
  appState.feedCollapsed = v;
  localStorage.setItem('feed_collapsed', String(v));
}

export function setSettingsTab(tab: 'config' | 'usage') {
  appState.settingsTab = tab;
}

// ── Data Fetching ──

export async function loadStatus() {
  try {
    const data = await fetchJSON<StatusResponse>('/api/status');
    appState.daemonState = data.daemon_state || 'stopped';
    if (data.channels?.whatsapp) {
      appState.whatsapp = data.channels.whatsapp.connected;
    }
    if (data.mcp_servers) {
      appState.mcpServers = data.mcp_servers;
    }
  } catch {
    appState.daemonState = 'stopped';
  }
}

export async function loadTickets() {
  try {
    const data = await fetchJSON<TicketSummary[]>('/api/ticket-summaries');
    appState.tickets = data || [];
  } catch { /* ignore */ }
}

export async function loadCosts() {
  try {
    const [today, budgets, month, week] = await Promise.all([
      fetchJSON<{ cost_usd: number }>('/api/costs/today'),
      fetchJSON<{ max_daily_usd: number; max_monthly_usd: number }>('/api/costs/budgets'),
      fetchJSON<{ cost_usd: number }>('/api/costs/month'),
      fetchJSON<DayCost[]>('/api/costs/week'),
    ]);
    appState.dailyCost = today.cost_usd || 0;
    appState.dailyBudget = budgets.max_daily_usd || 0;
    appState.monthlyCost = month.cost_usd || 0;
    appState.monthlyBudget = budgets.max_monthly_usd || 0;
    appState.weekDays = week || [];
    appState.weeklyCost = (week || []).reduce((sum, d) => sum + (d.cost_usd || 0), 0);
  } catch { /* ignore */ }
}

export async function loadActive() {
  try {
    const data = await fetchJSON<unknown[]>('/api/pipeline/active');
    appState.activeCount = Array.isArray(data) ? data.length : 0;
  } catch { /* ignore */ }
}

export async function loadTicketDetail(id: string) {
  if (!id) return;
  try {
    const [ticket, tasks, llmCalls, evts] = await Promise.all([
      fetchJSON<Ticket>(`/api/tickets/${id}`),
      fetchJSON<Task[]>(`/api/tickets/${id}/tasks`),
      fetchJSON<LlmCallRecord[]>(`/api/tickets/${id}/llm-calls`),
      fetchJSON<EventRecord[]>(`/api/tickets/${id}/events`),
    ]);
    appState.ticketDetail = ticket;
    appState.ticketTasks = tasks || [];
    appState.ticketLlmCalls = llmCalls || [];
    appState.ticketEvents = evts || [];
    appState.expandedTasks = {};
  } catch { /* ignore */ }
}

export function selectTicket(id: string) {
  appState.selectedTicketId = id;
  loadTicketDetail(id);
  if (window.innerWidth < 768) {
    appState.activePanel = 'detail';
  }
  const url = new URL(window.location.href);
  url.searchParams.set('ticket', id);
  history.pushState({}, '', url);
}

export function deselectTicket() {
  appState.selectedTicketId = null;
  appState.ticketDetail = null;
  appState.ticketTasks = [];
  appState.ticketLlmCalls = [];
  appState.ticketEvents = [];
  appState.activePanel = 'tickets';
  const url = new URL(window.location.href);
  url.searchParams.delete('ticket');
  history.pushState({}, '', url);
}

export async function loadRecentEvents() {
  try {
    const data = await fetchJSON<EventRecord[]>('/api/events?limit=50');
    // Only populate if feed is empty (don't overwrite live events)
    if (appState.events.length === 0) {
      appState.events = data || [];
    }
  } catch { /* ignore */ }
}

export async function loadTeamStats() {
  try {
    const [stats, prs] = await Promise.all([
      fetchJSON<TeamStat[]>('/api/stats/team'),
      fetchJSON<Ticket[]>('/api/stats/recent-prs'),
    ]);
    appState.teamStats = stats || [];
    appState.recentPRs = prs || [];
  } catch { /* ignore */ }
}

export async function fetchConfigSummary(): Promise<void> {
  try {
    appState.configSummary = await fetchJSON<ConfigSummary>('/api/config/summary');
  } catch { /* ignore */ }
}

export async function fetchClaudeCodeUsage(): Promise<void> {
  try {
    appState.claudeCodeUsage = await fetchJSON<ClaudeCodeUsage>('/api/usage/claude-code');
  } catch { /* ignore */ }
}

export async function fetchActivityBreakdown(): Promise<void> {
  try {
    appState.activityBreakdown = await fetchJSON<ActivityBreakdown>('/api/usage/activity');
  } catch { /* ignore */ }
}

export function openSettings() {
  appState.settingsOpen = true;
  fetchConfigSummary();
  fetchClaudeCodeUsage();
  fetchActivityBreakdown();
}

export function closeSettings() {
  appState.settingsOpen = false;
}

// ── Actions ──

export async function pauseDaemon() {
  await postJSON('/api/daemon/pause');
}

export async function resumeDaemon() {
  await postJSON('/api/daemon/resume');
}

export async function syncTracker() {
  await postJSON('/api/daemon/sync');
  // Give the daemon a moment to ingest, then refresh the ticket list.
  setTimeout(() => {
    loadTickets();
    loadActive();
  }, 1800);
}

export async function retryTicket(id: string) {
  await postJSON(`/api/tickets/${id}/retry`);
  loadTicketDetail(id);
  loadTickets();
}

export async function retryTask(taskId: string) {
  await postJSON(`/api/tasks/${taskId}/retry`);
  if (appState.selectedTicketId) loadTicketDetail(appState.selectedTicketId);
}

export async function replyToTicket(id: string, message: string) {
  await postJSONBody(`/api/tickets/${id}/reply`, { message });
  loadTicketDetail(id);
  loadTickets();
}

export async function deleteTicketAction(id: string) {
  await deleteJSON(`/api/tickets/${id}`);
  deselectTicket();
  loadTickets();
}

// ── Toast Helpers ──

export function addToast(message: string, severity: string, ticketId?: string) {
  const id = crypto.randomUUID();
  appState.toasts = [...appState.toasts, { id, message, ticketId, severity, createdAt: Date.now() }];
  if (appState.toasts.length > 3) appState.toasts = appState.toasts.slice(-3);
  setTimeout(() => {
    appState.toasts = appState.toasts.filter(t => t.id !== id);
  }, 5000);
}

// ── WebSocket ──

let ws: WebSocket | null = null;
let reconnectDelay = 1000;
let debounceTimer: ReturnType<typeof setTimeout> | null = null;

export function connectWebSocket() {
  const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
  const token = getToken();
  ws = new WebSocket(
    `${proto}//${location.host}/ws/events`,
    [`bearer.${token}`],
  );

  ws.onopen = () => {
    appState.wsConnected = true;
    reconnectDelay = 1000;
  };

  ws.onmessage = (e) => {
    const evt: EventRecord = JSON.parse(e.data);
    evt.isNew = true;

    // Track active task progress from agent events
    if (evt.TaskID && evt.Details) {
      try {
        const details = JSON.parse(evt.Details);
        if (evt.EventType === 'agent_turn_start') {
          appState.activeTaskProgress[evt.TaskID] = {
            ...(appState.activeTaskProgress[evt.TaskID] || {}),
            turn: details.turn_number || 0,
            maxTurns: details.max_turns || 50,
            tokensUsed: details.tokens_used || 0,
            runner: evt.runner || details.runner || '',
            model: evt.model || details.model || '',
          };
        }
        if (evt.EventType === 'agent_tool_end') {
          if (appState.activeTaskProgress[evt.TaskID]) {
            appState.activeTaskProgress[evt.TaskID] = {
              ...appState.activeTaskProgress[evt.TaskID],
              lastTool: details.tool_name,
              lastToolTime: evt.CreatedAt,
            };
          }
        }
        if (evt.EventType === 'task_done' || evt.EventType === 'task_failed') {
          delete appState.activeTaskProgress[evt.TaskID];
        }
      } catch { /* ignore parse errors */ }
    }

    // Prepend, deduplicating by ID in case of reconnect replays
    if (!appState.events.some(e => e.ID === evt.ID)) {
      appState.events = [evt, ...appState.events.slice(0, 49)];
    }
    setTimeout(() => { evt.isNew = false; }, 1200);

    if (evt.TicketID && evt.TicketID === appState.selectedTicketId) {
      if (!appState.ticketEvents.some(e => e.ID === evt.ID)) {
        appState.ticketEvents = [evt, ...appState.ticketEvents];
      }

      if (debounceTimer) clearTimeout(debounceTimer);
      debounceTimer = setTimeout(() => {
        loadTicketDetail(appState.selectedTicketId!);
      }, 300);
    }

    if (evt.TicketID && evt.TicketID !== appState.selectedTicketId) {
      if (evt.EventType === 'ticket_completed' || evt.EventType === 'ticket_merged') {
        addToast(`${evt.ticket_title || 'Ticket'} completed`, 'success', evt.TicketID);
      } else if (evt.EventType === 'ticket_failed') {
        addToast(`${evt.ticket_title || 'Ticket'} failed`, 'error', evt.TicketID);
      }
    }

    if (evt.EventType?.includes('status')) {
      loadTickets();
    }
  };

  ws.onclose = () => {
    appState.wsConnected = false;
    // Only reconnect if still authenticated
    if (appState.authenticated && getToken()) {
      setTimeout(() => {
        reconnectDelay = Math.min(reconnectDelay * 2, 30000);
        connectWebSocket();
      }, reconnectDelay);
    }
  };
}

// ── Polling ──

let intervals: ReturnType<typeof setInterval>[] = [];

export function startPolling() {
  loadStatus();
  loadTickets();
  loadCosts();
  loadActive();
  loadTeamStats();
  loadRecentEvents();

  intervals = [
    setInterval(loadStatus, 15000),
    setInterval(loadTickets, 10000),
    setInterval(loadCosts, 60000),
    setInterval(loadActive, 30000),
    setInterval(loadTeamStats, 60000),
  ];
}

export function stopPolling() {
  intervals.forEach(clearInterval);
  intervals = [];
  ws?.close();
}

let loggedOut = false;

export function logout() {
  if (loggedOut) return;
  loggedOut = true;
  stopPolling();
  clearToken();
  appState.authenticated = false;
  appState.daemonState = 'stopped';
  appState.wsConnected = false;
  appState.tickets = [];
  appState.selectedTicketId = null;
  appState.ticketDetail = null;
  appState.ticketTasks = [];
  appState.ticketLlmCalls = [];
  appState.ticketEvents = [];
  appState.events = [];
  appState.teamStats = [];
  appState.recentPRs = [];
  appState.toasts = [];
  appState.settingsOpen = false;
  appState.configSummary = null;
  appState.claudeCodeUsage = null;
  appState.activityBreakdown = null;
  appState.activeTaskProgress = {};
  loggedOut = false;
}

// Wire up the 401 handler
setOnUnauthorized(logout);

// ── URL State ──

export function restoreFromURL() {
  const params = new URLSearchParams(window.location.search);
  const ticketId = params.get('ticket');
  if (ticketId) selectTicket(ticketId);
  const f = params.get('filter');
  if (f === 'active' || f === 'done' || f === 'fail') appState.filter = f;
}
