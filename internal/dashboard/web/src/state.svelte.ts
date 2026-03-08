import { fetchJSON, postJSON, deleteJSON, getToken } from './api';
import type {
  Ticket, TicketSummary, Task, EventRecord, LlmCallRecord,
  TeamStat, DayCost, StatusResponse,
} from './types';

// ── Daemon & System ──
export let daemonState = $state<string>('stopped');
export let wsConnected = $state(false);
export let whatsapp = $state<boolean | null>(null);
export let mcpServers = $state<Record<string, { status: string; error?: string }>>({});

// ── Costs ──
export let dailyCost = $state(0);
export let dailyBudget = $state(0);
export let monthlyCost = $state(0);
export let monthlyBudget = $state(0);
export let weeklyCost = $state(0);
export let weekDays = $state<DayCost[]>([]);

// ── Tickets ──
export let tickets = $state<TicketSummary[]>([]);
export let activeCount = $state(0);

// ── Selection ──
export let selectedTicketId = $state<string | null>(null);
export let ticketDetail = $state<Ticket | null>(null);
export let ticketTasks = $state<Task[]>([]);
export let ticketLlmCalls = $state<LlmCallRecord[]>([]);
export let ticketEvents = $state<EventRecord[]>([]);
export let expandedTasks = $state<Record<string, boolean>>({});

// ── Live Feed ──
export let events = $state<EventRecord[]>([]);
export let feedCollapsed = $state(localStorage.getItem('feed_collapsed') === 'true');

// ── Team Stats ──
export let teamStats = $state<TeamStat[]>([]);
export let recentPRs = $state<Ticket[]>([]);

// ── Toasts ──
export interface Toast {
  id: string;
  message: string;
  ticketId?: string;
  severity: string;
  createdAt: number;
}
export let toasts = $state<Toast[]>([]);

// ── UI ──
export let activePanel = $state<'tickets' | 'detail' | 'feed' | 'health'>('tickets');
export let filter = $state<'all' | 'active' | 'done' | 'fail'>('all');
export let search = $state('');

export function setActivePanel(panel: 'tickets' | 'detail' | 'feed' | 'health') {
  activePanel = panel;
}

export function setFilter(f: 'all' | 'active' | 'done' | 'fail') {
  filter = f;
}

export function setSearch(s: string) {
  search = s;
}

export function setFeedCollapsed(v: boolean) {
  feedCollapsed = v;
  localStorage.setItem('feed_collapsed', String(v));
}

// ── Data Fetching ──

export async function loadStatus() {
  try {
    const data = await fetchJSON<StatusResponse>('/api/status');
    daemonState = data.daemon_state || 'stopped';
    if (data.channels?.whatsapp) {
      whatsapp = data.channels.whatsapp.connected;
    }
    if (data.mcp_servers) {
      mcpServers = data.mcp_servers;
    }
  } catch {
    daemonState = 'stopped';
  }
}

export async function loadTickets() {
  try {
    const data = await fetchJSON<TicketSummary[]>('/api/ticket-summaries');
    tickets = data || [];
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
    dailyCost = today.cost_usd || 0;
    dailyBudget = budgets.max_daily_usd || 0;
    monthlyCost = month.cost_usd || 0;
    monthlyBudget = budgets.max_monthly_usd || 0;
    weekDays = week || [];
    weeklyCost = (week || []).reduce((sum, d) => sum + (d.cost_usd || 0), 0);
  } catch { /* ignore */ }
}

export async function loadActive() {
  try {
    const data = await fetchJSON<unknown[]>('/api/pipeline/active');
    activeCount = Array.isArray(data) ? data.length : 0;
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
    ticketDetail = ticket;
    ticketTasks = tasks || [];
    ticketLlmCalls = llmCalls || [];
    ticketEvents = evts || [];
    expandedTasks = {};
  } catch { /* ignore */ }
}

export function selectTicket(id: string) {
  selectedTicketId = id;
  loadTicketDetail(id);
  if (window.innerWidth < 768) {
    activePanel = 'detail';
  }
  // Update URL
  const url = new URL(window.location.href);
  url.searchParams.set('ticket', id);
  history.pushState({}, '', url);
}

export function deselectTicket() {
  selectedTicketId = null;
  ticketDetail = null;
  ticketTasks = [];
  ticketLlmCalls = [];
  ticketEvents = [];
  activePanel = 'tickets';
  const url = new URL(window.location.href);
  url.searchParams.delete('ticket');
  history.pushState({}, '', url);
}

export async function loadTeamStats() {
  try {
    const [stats, prs] = await Promise.all([
      fetchJSON<TeamStat[]>('/api/stats/team'),
      fetchJSON<Ticket[]>('/api/stats/recent-prs'),
    ]);
    teamStats = stats || [];
    recentPRs = prs || [];
  } catch { /* ignore */ }
}

// ── Actions ──

export async function pauseDaemon() {
  await postJSON('/api/daemon/pause');
}

export async function resumeDaemon() {
  await postJSON('/api/daemon/resume');
}

export async function retryTicket(id: string) {
  await postJSON(`/api/tickets/${id}/retry`);
  loadTicketDetail(id);
  loadTickets();
}

export async function retryTask(taskId: string) {
  await postJSON(`/api/tasks/${taskId}/retry`);
  if (selectedTicketId) loadTicketDetail(selectedTicketId);
}

export async function deleteTicketAction(id: string) {
  await deleteJSON(`/api/tickets/${id}`);
  deselectTicket();
  loadTickets();
}

// ── Toast Helpers ──

export function addToast(message: string, severity: string, ticketId?: string) {
  const id = crypto.randomUUID();
  toasts = [...toasts, { id, message, ticketId, severity, createdAt: Date.now() }];
  if (toasts.length > 3) toasts = toasts.slice(-3);
  setTimeout(() => {
    toasts = toasts.filter(t => t.id !== id);
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
    wsConnected = true;
    reconnectDelay = 1000;
  };

  ws.onmessage = (e) => {
    const evt: EventRecord = JSON.parse(e.data);
    evt.isNew = true;

    // Live feed
    events = [evt, ...events.slice(0, 49)];
    setTimeout(() => { evt.isNew = false; }, 1200);

    // If event belongs to selected ticket, update detail
    if (evt.TicketID && evt.TicketID === selectedTicketId) {
      // Append to ticket events immediately
      ticketEvents = [evt, ...ticketEvents];

      // Debounced full refresh for task/status changes
      if (debounceTimer) clearTimeout(debounceTimer);
      debounceTimer = setTimeout(() => {
        loadTicketDetail(selectedTicketId!);
      }, 300);
    }

    // Toast for ticket completion/failure if viewing different ticket
    if (evt.TicketID && evt.TicketID !== selectedTicketId) {
      if (evt.EventType === 'ticket_completed' || evt.EventType === 'ticket_merged') {
        addToast(`${evt.ticket_title || 'Ticket'} completed`, 'success', evt.TicketID);
      } else if (evt.EventType === 'ticket_failed') {
        addToast(`${evt.ticket_title || 'Ticket'} failed`, 'error', evt.TicketID);
      }
    }

    // Optimistic ticket list update for status changes
    if (evt.EventType?.includes('status')) {
      loadTickets();
    }
  };

  ws.onclose = () => {
    wsConnected = false;
    setTimeout(() => {
      reconnectDelay = Math.min(reconnectDelay * 2, 30000);
      connectWebSocket();
    }, reconnectDelay);
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

// ── URL State ──

export function restoreFromURL() {
  const params = new URLSearchParams(window.location.search);
  const ticketId = params.get('ticket');
  if (ticketId) selectTicket(ticketId);
  const f = params.get('filter');
  if (f === 'active' || f === 'done' || f === 'fail') filter = f;
}
