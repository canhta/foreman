import { fetchJSON, postJSON, postJSONBody, deleteJSON, getToken } from '../api';
import type {
  Ticket, TicketSummary, Task, EventRecord, LlmCallRecord,
  DayCost, ChatMessage,
} from '../types';
import { toasts } from './toasts.svelte';

class ProjectState {
  // Current project
  projectId = $state<string | null>(null);

  // Tickets
  tickets = $state<TicketSummary[]>([]);
  filter = $state<'all' | 'active' | 'done' | 'fail'>('all');
  search = $state('');

  // Selected ticket detail
  selectedTicketId = $state<string | null>(null);
  ticketDetail = $state<Ticket | null>(null);
  ticketTasks = $state<Task[]>([]);
  ticketLlmCalls = $state<LlmCallRecord[]>([]);
  ticketEvents = $state<EventRecord[]>([]);
  chatMessages = $state<ChatMessage[]>([]);
  expandedTasks = $state<Record<string, boolean>>({});
  panelExpanded = $state(false); // side panel vs full page

  // Live task progress from WebSocket events
  activeTaskProgress = $state<Record<string, {
    turn: number; maxTurns: number; tokensUsed: number;
    runner?: string; model?: string; lastTool?: string; lastToolTime?: string;
  }>>({});

  // Project dashboard metrics
  dailyCost = $state(0);
  monthlyCost = $state(0);
  weekDays = $state<DayCost[]>([]);

  // Events feed
  events = $state<EventRecord[]>([]);

  // WebSocket
  private ws: WebSocket | null = null;
  private pollIntervals: number[] = [];

  private base(): string {
    return `/api/projects/${this.projectId}`;
  }

  switchProject(pid: string) {
    if (this.projectId === pid) return;
    this.stopPolling();
    this.projectId = pid;
    this.tickets = [];
    this.selectedTicketId = null;
    this.ticketDetail = null;
    this.events = [];
    this.panelExpanded = false;
    this.startPolling();
  }

  async loadTickets() {
    if (!this.projectId) return;
    try {
      this.tickets = await fetchJSON<TicketSummary[]>(`${this.base()}/ticket-summaries`);
    } catch (e) {
      console.error('loadTickets', e);
    }
  }

  async loadTicketDetail(ticketId: string) {
    if (!this.projectId) return;
    this.selectedTicketId = ticketId;
    try {
      const [detail, tasks, llmCalls, events, chat] = await Promise.all([
        fetchJSON<Ticket>(`${this.base()}/tickets/${ticketId}`),
        fetchJSON<Task[]>(`${this.base()}/tickets/${ticketId}/tasks`),
        fetchJSON<LlmCallRecord[]>(`${this.base()}/tickets/${ticketId}/llm-calls`),
        fetchJSON<EventRecord[]>(`${this.base()}/tickets/${ticketId}/events`),
        fetchJSON<ChatMessage[]>(`${this.base()}/tickets/${ticketId}/chat`).catch(() => [] as ChatMessage[]),
      ]);
      this.ticketDetail = detail;
      this.ticketTasks = tasks;
      this.ticketLlmCalls = llmCalls;
      this.ticketEvents = events;
      this.chatMessages = chat;
    } catch (e) {
      console.error('loadTicketDetail', e);
    }
  }

  expandPanel() {
    this.panelExpanded = true;
  }

  collapsePanel() {
    this.panelExpanded = false;
  }

  deselectTicket() {
    this.selectedTicketId = null;
    this.ticketDetail = null;
    this.panelExpanded = false;
  }

  async retryTicket(ticketId: string) {
    await postJSON(`${this.base()}/tickets/${ticketId}/retry`);
    toasts.add('Ticket retried', 'success');
    await this.loadTickets();
  }

  async deleteTicket(ticketId: string) {
    await deleteJSON(`${this.base()}/tickets/${ticketId}`);
    this.deselectTicket();
    await this.loadTickets();
  }

  async sendChatMessage(ticketId: string, content: string) {
    await postJSONBody(`${this.base()}/tickets/${ticketId}/chat`, { content });
    await this.loadTicketDetail(ticketId);
  }

  async syncTracker() {
    await postJSON(`${this.base()}/sync`);
    toasts.add('Sync triggered', 'info');
  }

  async loadCosts() {
    if (!this.projectId) return;
    try {
      const [daily, monthly, week] = await Promise.all([
        fetchJSON<{ cost_usd: number }>(`${this.base()}/costs/today`),
        fetchJSON<{ cost_usd: number }>(`${this.base()}/costs/month`),
        fetchJSON<DayCost[]>(`${this.base()}/costs/week`),
      ]);
      this.dailyCost = daily.cost_usd;
      this.monthlyCost = monthly.cost_usd;
      this.weekDays = week;
    } catch (e) {
      console.error('loadCosts', e);
    }
  }

  async loadEvents() {
    if (!this.projectId) return;
    try {
      this.events = await fetchJSON<EventRecord[]>(`${this.base()}/events?limit=50`);
    } catch {}
  }

  connectWebSocket() {
    if (!this.projectId) return;
    const protocol = location.protocol === 'https:' ? 'wss:' : 'ws:';
    const url = `${protocol}//${location.host}/ws/projects/${this.projectId}`;
    this.ws = new WebSocket(url, [`bearer.${getToken()}`]);
    this.ws.onmessage = (ev) => {
      try {
        const event = JSON.parse(ev.data);
        this.events = [event, ...this.events].slice(0, 100);
        // Auto-refresh tickets on status changes
        if (['ticket_status_changed', 'task_done', 'task_failed'].includes(event.event_type)) {
          this.loadTickets();
          if (this.selectedTicketId) this.loadTicketDetail(this.selectedTicketId);
        }
      } catch {}
    };
    this.ws.onclose = () => {
      setTimeout(() => this.connectWebSocket(), 5000);
    };
  }

  startPolling() {
    this.loadTickets();
    this.loadCosts();
    this.loadEvents();
    this.pollIntervals.push(
      window.setInterval(() => this.loadTickets(), 10000),
      window.setInterval(() => this.loadCosts(), 60000),
    );
    this.connectWebSocket();
  }

  stopPolling() {
    this.pollIntervals.forEach(clearInterval);
    this.pollIntervals = [];
    this.ws?.close();
    this.ws = null;
  }
}

export const projectState = new ProjectState();
