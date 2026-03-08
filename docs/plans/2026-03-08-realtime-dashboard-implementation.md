# Real-Time Dashboard Redesign — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Replace the Alpine.js dashboard with a Svelte 5 + TypeScript + Tailwind CSS 4 frontend that provides real-time task detail updates, structured activity streams, DAG visualization, and system observability.

**Architecture:** Svelte 5 app built with Vite, outputting static assets to `internal/dashboard/web/dist/`. Go server embeds `web/dist/` via `//go:embed`. WebSocket events drive reactive state via Svelte 5 runes. All styling via Tailwind utility classes with a custom dark monospace theme.

**Tech Stack:** Svelte 5, TypeScript, Vite, Tailwind CSS 4, Go (backend API additions)

**Design doc:** `docs/plans/2026-03-08-realtime-dashboard-design.md`

---

### Task 1: Scaffold Svelte + Vite + Tailwind Project

**Files:**
- Create: `internal/dashboard/web/package.json`
- Create: `internal/dashboard/web/vite.config.ts`
- Create: `internal/dashboard/web/tsconfig.json`
- Create: `internal/dashboard/web/src/main.ts`
- Create: `internal/dashboard/web/src/App.svelte`
- Create: `internal/dashboard/web/src/app.css`
- Create: `internal/dashboard/web/index.html`
- Modify: `internal/dashboard/server.go:19-20` (change embed path)
- Modify: `internal/dashboard/server.go:119` (change fs.Sub path)
- Modify: `Makefile` (add frontend build target)
- Create: `internal/dashboard/web/.gitignore`

**Step 1: Initialize npm project**

```bash
cd internal/dashboard/web
npm init -y
npm install -D svelte @sveltejs/vite-plugin-svelte vite typescript tailwindcss @tailwindcss/vite
```

**Step 2: Create vite.config.ts**

```ts
import { defineConfig } from 'vite';
import { svelte } from '@sveltejs/vite-plugin-svelte';
import tailwindcss from '@tailwindcss/vite';

export default defineConfig({
  plugins: [svelte(), tailwindcss()],
  build: {
    outDir: '../dist',
    emptyOutDir: true,
  },
  server: {
    proxy: {
      '/api': 'http://localhost:8080',
      '/ws': { target: 'ws://localhost:8080', ws: true },
    },
  },
});
```

**Step 3: Create tsconfig.json**

```json
{
  "extends": "@sveltejs/vite-plugin-svelte/tsconfig.json",
  "compilerOptions": {
    "target": "ESNext",
    "module": "ESNext",
    "moduleResolution": "bundler",
    "strict": true,
    "noEmit": true
  },
  "include": ["src/**/*"]
}
```

**Step 4: Create index.html**

```html
<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>FOREMAN</title>
  <link rel="icon" type="image/svg+xml" href="data:image/svg+xml,<svg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 1 1'><rect fill='%23FFE600' width='1' height='1'/></svg>">
</head>
<body>
  <div id="app"></div>
  <script type="module" src="/src/main.ts"></script>
</body>
</html>
```

**Step 5: Create app.css with Tailwind + theme**

```css
@import "tailwindcss";

@theme {
  --color-bg: #0a0a0a;
  --color-surface: #111111;
  --color-surface-hover: #1a1a1a;
  --color-border: #2a2a2a;
  --color-accent: #FFE600;
  --color-accent-dim: #8B7D00;
  --color-text: #F0F0F0;
  --color-muted: #888888;
  --color-danger: #FF4444;
  --color-success: #00CC66;
  --color-warning: #FFB020;
  --font-family-mono: "JetBrains Mono", "SF Mono", "Fira Code", "Cascadia Code", ui-monospace, monospace;
}
```

**Step 6: Create main.ts**

```ts
import './app.css';
import App from './App.svelte';
import { mount } from 'svelte';

mount(App, { target: document.getElementById('app')! });
```

**Step 7: Create minimal App.svelte**

```svelte
<script lang="ts">
  let message = $state('FOREMAN');
</script>

<div class="min-h-screen bg-bg text-text font-mono flex items-center justify-center">
  <h1 class="text-accent text-2xl font-bold">{message}</h1>
</div>
```

**Step 8: Create .gitignore**

```
node_modules/
```

**Step 9: Update Go embed directive**

In `internal/dashboard/server.go`, change:
- Line 19-20: `//go:embed web` → `//go:embed dist`
- Line 119: `fs.Sub(webFS, "web")` → `fs.Sub(webFS, "dist")`

**Step 10: Add Makefile target**

Add to `Makefile`:
```makefile
.PHONY: dashboard-build dashboard-dev
dashboard-build:
	cd internal/dashboard/web && npm ci && npm run build
dashboard-dev:
	cd internal/dashboard/web && npm run dev
```

Update the existing `build` target to depend on `dashboard-build`.

**Step 11: Add build script to package.json**

Ensure `package.json` has:
```json
{
  "scripts": {
    "dev": "vite",
    "build": "vite build",
    "preview": "vite preview"
  }
}
```

**Step 12: Build and verify**

```bash
cd internal/dashboard/web && npm run build
ls ../dist/
# Should see index.html, assets/
```

**Step 13: Commit**

```bash
git add internal/dashboard/web/package.json internal/dashboard/web/vite.config.ts \
  internal/dashboard/web/tsconfig.json internal/dashboard/web/src/ \
  internal/dashboard/web/index.html internal/dashboard/web/app.css \
  internal/dashboard/web/.gitignore internal/dashboard/dist/ \
  internal/dashboard/server.go Makefile
git commit -m "feat(dashboard): scaffold Svelte 5 + Vite + Tailwind project"
```

---

### Task 2: Types, API Client, and Formatting Utilities

**Files:**
- Create: `internal/dashboard/web/src/types.ts`
- Create: `internal/dashboard/web/src/api.ts`
- Create: `internal/dashboard/web/src/format.ts`

**Step 1: Create types.ts**

Mirror the Go models. Reference: `internal/models/ticket.go:5-69`, `internal/models/pipeline.go`.

```ts
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
```

**Step 2: Create api.ts**

Reference: existing `app.js:1-18` for the pattern, `api.go:65-108` for endpoints.

```ts
let token = localStorage.getItem('foreman_token') || '';
const headers = () => ({ Authorization: `Bearer ${token}` });

export function setToken(t: string) {
  token = t;
  localStorage.setItem('foreman_token', t);
}

export function getToken(): string {
  return token;
}

export async function fetchJSON<T>(url: string): Promise<T> {
  const res = await fetch(url, { headers: headers() });
  if (!res.ok) throw new Error(res.statusText);
  return res.json();
}

export async function postJSON<T>(url: string): Promise<T> {
  const res = await fetch(url, { method: 'POST', headers: headers() });
  if (!res.ok) throw new Error(res.statusText);
  return res.json();
}

export async function deleteJSON(url: string): Promise<void> {
  const res = await fetch(url, { method: 'DELETE', headers: headers() });
  if (!res.ok) throw new Error(res.statusText);
}
```

**Step 3: Create format.ts**

Reference: existing `app.js:20-48`.

```ts
export function formatSender(jid: string): string {
  if (!jid) return '';
  return jid.replace(/@s\.whatsapp\.net$/, '');
}

export function formatTime(ts: string | null): string {
  if (!ts) return '';
  return new Date(ts).toLocaleTimeString();
}

export function formatRelative(ts: string | null): string {
  if (!ts) return '';
  const diff = Date.now() - new Date(ts).getTime();
  const mins = Math.floor(diff / 60000);
  if (mins < 1) return 'just now';
  if (mins < 60) return `${mins}m ago`;
  const hours = Math.floor(mins / 60);
  if (hours < 24) return `${hours}h ago`;
  return `${Math.floor(hours / 24)}d ago`;
}

export function formatCost(usd: number): string {
  return `$${usd.toFixed(2)}`;
}

export function formatTokens(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`;
  if (n >= 1_000) return `${Math.round(n / 1_000)}k`;
  return `${n}`;
}

export function severityIcon(severity: string): string {
  switch (severity) {
    case 'success': return '\u2713';
    case 'error': return '\u2717';
    case 'warning': return '\u26A0';
    default: return '\u25CF';
  }
}

export function taskIcon(status: string): string {
  switch (status) {
    case 'done': return '\u2713';
    case 'failed': return '\u2717';
    case 'implementing': case 'tdd_verifying': case 'testing':
    case 'spec_review': case 'quality_review': return '\u2699';
    case 'skipped': return '\u2298';
    default: return '\u25CB';
  }
}
```

**Step 4: Verify TypeScript compiles**

```bash
cd internal/dashboard/web && npx tsc --noEmit
```
Expected: no errors.

**Step 5: Commit**

```bash
git add internal/dashboard/web/src/types.ts internal/dashboard/web/src/api.ts internal/dashboard/web/src/format.ts
git commit -m "feat(dashboard): add TypeScript types, API client, and formatters"
```

---

### Task 3: Shared Reactive State (`state.svelte.ts`)

**Files:**
- Create: `internal/dashboard/web/src/state.svelte.ts`

**Step 1: Create state.svelte.ts**

This is the single source of truth for all dashboard state, using Svelte 5 runes.

```ts
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
```

**Step 2: Verify compilation**

```bash
cd internal/dashboard/web && npx tsc --noEmit
```

**Step 3: Commit**

```bash
git add internal/dashboard/web/src/state.svelte.ts
git commit -m "feat(dashboard): add shared reactive state with Svelte 5 runes"
```

---

### Task 4: Header Component

**Files:**
- Create: `internal/dashboard/web/src/components/Header.svelte`

**Step 1: Create Header.svelte**

Port the header from `index.html:12-31`. Reference state from `state.svelte.ts`.

```svelte
<script lang="ts">
  import {
    daemonState, wsConnected, whatsapp,
    dailyCost, dailyBudget, activeCount,
    activePanel, pauseDaemon, resumeDaemon,
  } from '../state.svelte';
  import { formatCost } from '../format';

  let dotClass = $derived(
    !wsConnected ? 'bg-danger' :
    daemonState === 'running' ? 'bg-success' :
    'bg-warning'
  );

  let label = $derived(!wsConnected ? 'DISCONNECTED' : daemonState.toUpperCase());

  let costLabel = $derived(
    dailyBudget > 0
      ? `COST: ${formatCost(dailyCost)} / $${Math.round(dailyBudget)}`
      : `COST: ${formatCost(dailyCost)}`
  );

  let costOverBudget = $derived(dailyBudget > 0 && (dailyCost / dailyBudget) * 100 >= 80);

  function handlePause() {
    if (confirm('Pause the daemon?')) pauseDaemon();
  }
  function handleResume() {
    if (confirm('Resume the daemon?')) resumeDaemon();
  }
</script>

<header class="flex items-center justify-between px-4 py-2 border-b border-border bg-surface sticky top-0 z-50">
  <button
    class="text-accent font-bold text-lg tracking-wider hover:opacity-80 cursor-pointer"
    onclick={() => { activePanel = 'tickets'; }}
  >FOREMAN</button>

  <div class="flex items-center gap-3 text-sm">
    <span class="inline-block w-2 h-2 rounded-full {dotClass}"></span>
    <span class="text-muted">{label}</span>

    {#if whatsapp !== null}
      <span class="text-border">|</span>
      <span class="inline-block w-2 h-2 rounded-full {whatsapp ? 'bg-success' : 'bg-danger'}"></span>
      <span class:text-danger={!whatsapp}>{whatsapp ? 'WA: OK' : 'WA: DOWN'}</span>
    {/if}

    <span class="text-border hidden md:inline">|</span>
    <span class="hidden md:inline {costOverBudget ? 'text-danger' : 'text-muted'}">{costLabel}</span>
    <span class="text-border hidden md:inline">|</span>
    <span class="hidden md:inline text-muted">ACTIVE: {activeCount}</span>

    <span class="text-border">|</span>
    {#if daemonState === 'running'}
      <button class="text-accent hover:text-text text-xs" onclick={handlePause}>PAUSE</button>
    {:else}
      <button class="text-accent hover:text-text text-xs" onclick={handleResume}>RESUME</button>
    {/if}

    <button
      class="text-muted hover:text-accent text-xs hidden md:inline"
      onclick={() => { activePanel = 'health'; }}
    >SYSTEM</button>
  </div>
</header>
```

**Step 2: Verify it renders by importing in App.svelte**

Update `App.svelte` to import and render `<Header />`.

**Step 3: Commit**

```bash
git add internal/dashboard/web/src/components/Header.svelte internal/dashboard/web/src/App.svelte
git commit -m "feat(dashboard): add Header component"
```

---

### Task 5: TicketList Component

**Files:**
- Create: `internal/dashboard/web/src/components/TicketList.svelte`

**Step 1: Create TicketList.svelte**

Port from `index.html:36-67`. Includes search, filter tabs, ticket cards, keyboard navigation (j/k/Enter).

```svelte
<script lang="ts">
  import {
    tickets, filter, search, selectedTicketId, selectTicket,
  } from '../state.svelte';
  import { ACTIVE_STATUSES, DONE_STATUSES, FAIL_STATUSES } from '../types';
  import type { TicketSummary } from '../types';
  import { formatSender, formatRelative, formatCost } from '../format';

  let focusIndex = $state(-1);

  let filteredTickets = $derived.by(() => {
    let list = tickets;
    if (filter === 'active') list = list.filter(t => ACTIVE_STATUSES.includes(t.Status));
    else if (filter === 'done') list = list.filter(t => DONE_STATUSES.includes(t.Status));
    else if (filter === 'fail') list = list.filter(t => FAIL_STATUSES.includes(t.Status));

    if (search) {
      const q = search.toLowerCase();
      list = list.filter(t =>
        t.Title?.toLowerCase().includes(q) ||
        t.ChannelSenderID?.toLowerCase().includes(q)
      );
    }

    return list.toSorted((a, b) => {
      const aFail = FAIL_STATUSES.includes(a.Status) ? 0 : 1;
      const bFail = FAIL_STATUSES.includes(b.Status) ? 0 : 1;
      if (aFail !== bFail) return aFail - bFail;
      return new Date(b.UpdatedAt).getTime() - new Date(a.UpdatedAt).getTime();
    });
  });

  function countByFilter(f: string): number {
    if (f === 'active') return tickets.filter(t => ACTIVE_STATUSES.includes(t.Status)).length;
    if (f === 'done') return tickets.filter(t => DONE_STATUSES.includes(t.Status)).length;
    if (f === 'fail') return tickets.filter(t => FAIL_STATUSES.includes(t.Status)).length;
    return tickets.length;
  }

  function statusClass(status: string): string {
    if (FAIL_STATUSES.includes(status as any)) return 'text-danger';
    if (ACTIVE_STATUSES.includes(status as any)) return 'text-accent';
    if (DONE_STATUSES.includes(status as any)) return 'text-success';
    return 'text-muted';
  }

  function handleKeydown(e: KeyboardEvent) {
    if (e.key === 'j') { focusIndex = Math.min(focusIndex + 1, filteredTickets.length - 1); e.preventDefault(); }
    if (e.key === 'k') { focusIndex = Math.max(focusIndex - 1, 0); e.preventDefault(); }
    if (e.key === 'Enter' && focusIndex >= 0) { selectTicket(filteredTickets[focusIndex].ID); e.preventDefault(); }
    if (e.key === '1') { filter = 'all'; e.preventDefault(); }
    if (e.key === '2') { filter = 'active'; e.preventDefault(); }
    if (e.key === '3') { filter = 'done'; e.preventDefault(); }
    if (e.key === '4') { filter = 'fail'; e.preventDefault(); }
  }
</script>

<svelte:window onkeydown={handleKeydown} />

<section class="flex flex-col h-full border-r border-border bg-surface">
  <div class="px-3 py-2 border-b border-border text-xs text-muted font-bold tracking-wider">
    TICKETS ({filteredTickets.length})
  </div>

  <div class="px-3 py-2 border-b border-border space-y-2">
    <input
      type="text"
      bind:value={search}
      placeholder="Search tickets..."
      class="w-full bg-bg border border-border px-2 py-1 text-xs text-text placeholder:text-muted focus:border-accent outline-none"
    />
    <div class="flex gap-1">
      {#each [['all', 'ALL'], ['active', 'ACT'], ['done', 'DONE'], ['fail', 'FAIL']] as [key, label]}
        <button
          class="flex-1 text-xs py-1 border {filter === key ? 'border-accent text-accent' : 'border-border text-muted hover:text-text'}"
          onclick={() => { filter = key as any; }}
        >{label} {countByFilter(key)}</button>
      {/each}
    </div>
  </div>

  <div class="flex-1 overflow-y-auto">
    {#each filteredTickets as t, i}
      <button
        class="w-full text-left px-3 py-2 border-b border-border hover:bg-surface-hover cursor-pointer
          {selectedTicketId === t.ID ? 'bg-surface-hover border-l-2 border-l-accent' : ''}
          {focusIndex === i ? 'ring-1 ring-accent ring-inset' : ''}"
        onclick={() => selectTicket(t.ID)}
      >
        <div class="text-sm text-text truncate">{t.Title || t.ID}</div>
        <div class="flex items-center gap-2 mt-1 text-xs">
          <span class={statusClass(t.Status)}>{t.Status.toUpperCase()}</span>
          <span class="text-muted">{formatSender(t.ChannelSenderID)}</span>
        </div>
        {#if t.tasks_total > 0}
          <div class="flex items-center gap-2 mt-1">
            <div class="flex-1 h-1 bg-border rounded overflow-hidden">
              <div class="h-full bg-accent" style="width:{(t.tasks_done / t.tasks_total) * 100}%"></div>
            </div>
            <span class="text-xs text-muted">{formatCost(t.CostUSD)} {t.tasks_done}/{t.tasks_total}</span>
          </div>
        {/if}
      </button>
    {/each}
  </div>
</section>
```

**Step 2: Commit**

```bash
git add internal/dashboard/web/src/components/TicketList.svelte
git commit -m "feat(dashboard): add TicketList component with search, filters, keyboard nav"
```

---

### Task 6: TaskCard Component

**Files:**
- Create: `internal/dashboard/web/src/components/TaskCard.svelte`

**Step 1: Create TaskCard.svelte**

Displays a single task with expandable detail, retry button, status badges, and activity sub-items. Reference: `index.html:231-250`, `app.js:274-300`.

```svelte
<script lang="ts">
  import type { Task, EventRecord } from '../types';
  import { retryTask, expandedTasks } from '../state.svelte';
  import { taskIcon, formatCost, formatRelative } from '../format';

  let { task, events = [] }: { task: Task; events?: EventRecord[] } = $props();

  let expanded = $derived(expandedTasks[task.ID] ?? false);

  let taskEvents = $derived(
    events.filter(e => e.TaskID === task.ID).slice(0, 10)
  );

  let isActive = $derived(
    ['implementing', 'tdd_verifying', 'testing', 'spec_review', 'quality_review'].includes(task.Status)
  );

  function toggle() {
    expandedTasks[task.ID] = !expanded;
  }

  function statusColor(): string {
    if (task.Status === 'done') return 'text-success';
    if (task.Status === 'failed') return 'text-danger';
    if (isActive) return 'text-accent';
    return 'text-muted';
  }

  function handleRetry(e: MouseEvent) {
    e.stopPropagation();
    if (confirm('Retry this task?')) retryTask(task.ID);
  }
</script>

<div class="border border-border {isActive ? 'border-l-2 border-l-accent' : ''} {task.Status === 'failed' ? 'border-l-2 border-l-danger' : ''}">
  <button
    class="w-full text-left px-3 py-2 hover:bg-surface-hover flex items-center gap-2 cursor-pointer"
    onclick={toggle}
    aria-expanded={expanded}
  >
    <span class="{statusColor()} {isActive ? 'animate-pulse' : ''}">{taskIcon(task.Status)}</span>
    <span class="text-sm flex-1 truncate">{task.Sequence}. {task.Title}</span>
    <span class="text-xs text-muted">{task.EstimatedComplexity}</span>
    {#if task.Status === 'failed'}
      <button class="text-xs text-danger hover:text-text" onclick={handleRetry}>[retry]</button>
    {/if}
  </button>

  {#if expanded}
    <div class="px-3 py-2 border-t border-border text-xs space-y-1 bg-bg">
      <div class="flex gap-4 text-muted">
        <span>Status: <span class={statusColor()}>{task.Status}</span></span>
        {#if task.ImplementationAttempts > 0}
          <span>Attempt {task.ImplementationAttempts}</span>
        {/if}
        <span>Cost: {formatCost(task.CostUSD)}</span>
      </div>

      {#if task.FilesToModify?.length}
        <div class="text-muted">
          Files: <span class="text-text">{task.FilesToModify.join(', ')}</span>
        </div>
      {/if}

      {#if task.ErrorMessage}
        <div class="text-danger mt-1 p-2 bg-danger/10 border border-danger/20">
          {task.ErrorMessage}
        </div>
      {/if}

      {#if task.AcceptanceCriteria?.length}
        <div class="mt-2">
          <div class="text-muted mb-1">Acceptance Criteria:</div>
          {#each task.AcceptanceCriteria as criterion}
            <div class="flex items-center gap-1">
              <span class="text-muted">{task.Status === 'done' ? '\u2713' : '\u25CB'}</span>
              <span>{criterion}</span>
            </div>
          {/each}
        </div>
      {/if}

      <!-- Activity stream for this task -->
      {#if taskEvents.length > 0}
        <div class="mt-2 border-t border-border pt-2">
          <div class="text-muted mb-1">Activity:</div>
          {#each taskEvents as evt}
            <div class="flex gap-2 py-0.5">
              <span class="text-muted shrink-0">{formatRelative(evt.CreatedAt)}</span>
              <span class="truncate">{evt.Message || evt.EventType}</span>
            </div>
          {/each}
        </div>
      {/if}
    </div>
  {/if}
</div>
```

**Step 2: Commit**

```bash
git add internal/dashboard/web/src/components/TaskCard.svelte
git commit -m "feat(dashboard): add TaskCard component with expandable detail and activity"
```

---

### Task 7: ActivityStream Component

**Files:**
- Create: `internal/dashboard/web/src/components/ActivityStream.svelte`

**Step 1: Create ActivityStream.svelte**

Chronological activity feed for the selected ticket. Shows meaningful milestones derived from events. Active items pulse, failed items are highlighted. Auto-scrolls when at bottom.

```svelte
<script lang="ts">
  import type { EventRecord, Task } from '../types';
  import { severityIcon, formatRelative, taskIcon } from '../format';

  let { events = [], tasks = [] }: { events: EventRecord[]; tasks: Task[] } = $props();

  let container: HTMLElement;
  let autoScroll = $state(true);

  function handleScroll() {
    if (!container) return;
    const { scrollTop, scrollHeight, clientHeight } = container;
    autoScroll = scrollHeight - scrollTop - clientHeight < 50;
  }

  $effect(() => {
    if (autoScroll && container && events.length) {
      container.scrollTop = 0; // Events prepend, so scroll to top
    }
  });

  function eventLabel(evt: EventRecord): string {
    // Derive human-readable labels from event types
    const type = evt.EventType;
    if (type === 'planning_started') return 'Planning started';
    if (type === 'planning_complete') return 'Planning complete';
    if (type === 'task_started') return `Task started`;
    if (type === 'task_completed') return `Task completed`;
    if (type === 'task_failed') return `Task failed`;
    if (type === 'tests_passed') return 'Tests passed';
    if (type === 'tests_failed') return 'Tests failed';
    if (type === 'pr_created') return 'PR created';
    if (type === 'ticket_completed') return 'Ticket completed';
    if (type === 'ticket_failed') return 'Ticket failed';
    return type?.replace(/_/g, ' ') || 'event';
  }

  function taskTitle(taskId: string): string {
    const task = tasks.find(t => t.ID === taskId);
    return task ? `${task.Sequence}. ${task.Title}` : '';
  }

  function expandDetails(details: string): Record<string, string> | null {
    if (!details) return null;
    try { return JSON.parse(details); } catch { return null; }
  }
</script>

<div
  bind:this={container}
  onscroll={handleScroll}
  class="flex-1 overflow-y-auto space-y-0"
  role="log"
  aria-label="Activity stream"
>
  {#each events as evt (evt.ID)}
    <div
      class="px-3 py-2 border-b border-border hover:bg-surface-hover
        {evt.Severity === 'error' ? 'bg-danger/5 border-l-2 border-l-danger' : ''}
        {evt.isNew ? 'animate-fade-in' : ''}"
    >
      <div class="flex items-start gap-2">
        <span class="shrink-0 {
          evt.Severity === 'success' ? 'text-success' :
          evt.Severity === 'error' ? 'text-danger' :
          evt.Severity === 'warning' ? 'text-warning' :
          'text-muted'
        }">{severityIcon(evt.Severity)}</span>

        <div class="flex-1 min-w-0">
          <div class="flex items-center gap-2">
            <span class="text-sm font-medium">{eventLabel(evt)}</span>
            <span class="text-xs text-muted ml-auto shrink-0">{formatRelative(evt.CreatedAt)}</span>
          </div>

          {#if evt.TaskID && taskTitle(evt.TaskID)}
            <div class="text-xs text-muted mt-0.5">{taskTitle(evt.TaskID)}</div>
          {/if}

          {#if evt.Message}
            <div class="text-xs text-text/80 mt-0.5">{evt.Message}</div>
          {/if}
        </div>
      </div>

      <!-- Expandable raw details -->
      {#if evt.Details}
        {@const details = expandDetails(evt.Details)}
        {#if details}
          <details class="mt-1 ml-5">
            <summary class="text-xs text-muted cursor-pointer hover:text-accent">raw detail</summary>
            <pre class="text-xs text-muted mt-1 overflow-x-auto">{JSON.stringify(details, null, 2)}</pre>
          </details>
        {/if}
      {/if}
    </div>
  {/each}

  {#if events.length === 0}
    <div class="px-3 py-8 text-center text-muted text-sm">No activity yet.</div>
  {/if}
</div>
```

**Step 2: Commit**

```bash
git add internal/dashboard/web/src/components/ActivityStream.svelte
git commit -m "feat(dashboard): add ActivityStream component with auto-scroll and detail expand"
```

---

### Task 8: DagView Component

**Files:**
- Create: `internal/dashboard/web/src/components/DagView.svelte`

**Step 1: Create DagView.svelte**

SVG-based DAG visualization. Renders tasks as nodes with dependency edges. Rank-based layout.

```svelte
<script lang="ts">
  import type { Task } from '../types';
  import { taskIcon } from '../format';
  import { selectTicket } from '../state.svelte';

  let { tasks = [] }: { tasks: Task[] } = $props();

  // Only show when there are dependencies
  let hasDeps = $derived(tasks.some(t => t.DependsOn?.length > 0));

  // Build ranks (depth) by topological sort
  interface DagNode {
    task: Task;
    rank: number;
    x: number;
    y: number;
  }

  let nodes = $derived.by(() => {
    if (!hasDeps) return [];

    const taskMap = new Map(tasks.map(t => [t.ID, t]));
    const ranks = new Map<string, number>();

    function getRank(id: string): number {
      if (ranks.has(id)) return ranks.get(id)!;
      const task = taskMap.get(id);
      if (!task || !task.DependsOn?.length) {
        ranks.set(id, 0);
        return 0;
      }
      const maxDep = Math.max(...task.DependsOn.map(d => getRank(d)));
      const rank = maxDep + 1;
      ranks.set(id, rank);
      return rank;
    }

    tasks.forEach(t => getRank(t.ID));

    // Group by rank
    const byRank = new Map<number, Task[]>();
    tasks.forEach(t => {
      const r = ranks.get(t.ID) || 0;
      if (!byRank.has(r)) byRank.set(r, []);
      byRank.get(r)!.push(t);
    });

    const nodeWidth = 160;
    const nodeHeight = 40;
    const gapX = 60;
    const gapY = 20;

    const result: DagNode[] = [];
    for (const [rank, rankTasks] of byRank) {
      rankTasks.forEach((task, i) => {
        result.push({
          task,
          rank,
          x: rank * (nodeWidth + gapX) + 20,
          y: i * (nodeHeight + gapY) + 20,
        });
      });
    }
    return result;
  });

  let nodeMap = $derived(new Map(nodes.map(n => [n.task.ID, n])));
  let maxRank = $derived(Math.max(0, ...nodes.map(n => n.rank)));

  let svgWidth = $derived((maxRank + 1) * 220 + 40);
  let svgHeight = $derived(Math.max(100, ...nodes.map(n => n.y + 60)));

  function statusColor(status: string): string {
    if (status === 'done') return '#00CC66';
    if (status === 'failed') return '#FF4444';
    if (['implementing', 'tdd_verifying', 'testing', 'spec_review', 'quality_review'].includes(status)) return '#FFE600';
    return '#2a2a2a';
  }

  // Build edges
  let edges = $derived.by(() => {
    const result: { from: DagNode; to: DagNode }[] = [];
    for (const node of nodes) {
      for (const depId of node.task.DependsOn || []) {
        const from = nodeMap.get(depId);
        if (from) result.push({ from, to: node });
      }
    }
    return result;
  });
</script>

{#if hasDeps}
  <div class="overflow-x-auto border border-border bg-bg p-2">
    <svg width={svgWidth} height={svgHeight} class="block">
      <defs>
        <marker id="arrow" viewBox="0 0 10 6" refX="10" refY="3" markerWidth="8" markerHeight="6" orient="auto-start-reverse">
          <path d="M 0 0 L 10 3 L 0 6 z" fill="#888" />
        </marker>
      </defs>

      <!-- Edges -->
      {#each edges as edge}
        <line
          x1={edge.from.x + 160} y1={edge.from.y + 20}
          x2={edge.to.x} y2={edge.to.y + 20}
          stroke="#444" stroke-width="1.5" marker-end="url(#arrow)"
        />
      {/each}

      <!-- Nodes -->
      {#each nodes as node}
        <g transform="translate({node.x},{node.y})" class="cursor-pointer">
          <rect
            width="160" height="40" rx="4"
            fill="#111" stroke={statusColor(node.task.Status)} stroke-width="2"
          />
          <text x="8" y="16" fill="#F0F0F0" font-size="10" font-family="monospace">
            {taskIcon(node.task.Status)} {node.task.Sequence}. {node.task.Title.slice(0, 18)}
          </text>
          <text x="8" y="30" fill="#888" font-size="9" font-family="monospace">
            {node.task.Status}
          </text>
        </g>
      {/each}
    </svg>
  </div>
{/if}
```

**Step 2: Commit**

```bash
git add internal/dashboard/web/src/components/DagView.svelte
git commit -m "feat(dashboard): add DagView SVG component for task dependency visualization"
```

---

### Task 9: CostBreakdown Component

**Files:**
- Create: `internal/dashboard/web/src/components/CostBreakdown.svelte`

**Step 1: Create CostBreakdown.svelte**

Port from `index.html:255-277`. Shows cost by role, LLM call summary, token usage.

```svelte
<script lang="ts">
  import type { Ticket, LlmCallRecord } from '../types';
  import { formatCost, formatTokens } from '../format';

  let { ticket, llmCalls = [] }: { ticket: Ticket | null; llmCalls: LlmCallRecord[] } = $props();

  let costByRole = $derived.by(() => {
    const roles: Record<string, number> = {};
    let total = 0;
    for (const c of llmCalls) {
      roles[c.Role] = (roles[c.Role] || 0) + (c.CostUSD || 0);
      total += c.CostUSD || 0;
    }
    return Object.entries(roles)
      .map(([role, cost]) => ({ role, cost, pct: total > 0 ? (cost / total) * 100 : 0 }))
      .sort((a, b) => b.cost - a.cost);
  });

  let summary = $derived.by(() => {
    let totalTokens = 0;
    const models = new Set<string>();
    let ok = 0, retried = 0;
    for (const c of llmCalls) {
      totalTokens += (c.TokensInput || 0) + (c.TokensOutput || 0);
      models.add(c.Model);
      if (c.Status === 'success') ok++; else retried++;
    }
    return {
      totalCalls: llmCalls.length,
      ok, retried, totalTokens,
      model: [...models].join(', ') || '--',
    };
  });
</script>

<div class="space-y-2">
  <div class="flex justify-between text-xs text-muted font-bold tracking-wider">
    <span>COST BREAKDOWN</span>
    <span>{formatCost(ticket?.CostUSD || 0)}</span>
  </div>

  {#each costByRole as item}
    <div class="flex items-center gap-2 text-xs">
      <span class="w-24 text-muted truncate">{item.role}</span>
      <span class="text-text">{formatCost(item.cost)}</span>
      <div class="flex-1 h-1 bg-border rounded overflow-hidden">
        <div class="h-full bg-accent" style="width:{item.pct}%"></div>
      </div>
    </div>
  {/each}

  <div class="text-xs text-muted space-x-2 pt-1 border-t border-border">
    <span>Model: {summary.model}</span>
    <span>|</span>
    <span>{formatTokens(summary.totalTokens)} tokens</span>
    <span>|</span>
    <span>{summary.totalCalls} calls ({summary.ok} ok, {summary.retried} retried)</span>
  </div>
</div>
```

**Step 2: Commit**

```bash
git add internal/dashboard/web/src/components/CostBreakdown.svelte
git commit -m "feat(dashboard): add CostBreakdown component"
```

---

### Task 10: TicketDetail Component

**Files:**
- Create: `internal/dashboard/web/src/components/TicketDetail.svelte`

**Step 1: Create TicketDetail.svelte**

Assembles the detail panel: header, DAG view, task cards, activity stream, cost breakdown. Port from `index.html:192-293`, but structured as composed components.

```svelte
<script lang="ts">
  import {
    ticketDetail, ticketTasks, ticketEvents, ticketLlmCalls,
    selectedTicketId, deselectTicket, retryTicket, deleteTicketAction,
  } from '../state.svelte';
  import { FAIL_STATUSES } from '../types';
  import { formatSender, formatRelative, formatCost } from '../format';
  import TaskCard from './TaskCard.svelte';
  import DagView from './DagView.svelte';
  import ActivityStream from './ActivityStream.svelte';
  import CostBreakdown from './CostBreakdown.svelte';

  let activeTab = $state<'tasks' | 'activity' | 'cost'>('tasks');

  let isFailed = $derived(
    ticketDetail ? FAIL_STATUSES.includes(ticketDetail.Status) : false
  );

  let tasksDone = $derived(ticketTasks.filter(t => t.Status === 'done').length);

  function handleRetry() {
    if (selectedTicketId && confirm('Retry this ticket?')) retryTicket(selectedTicketId);
  }

  function handleDelete() {
    if (selectedTicketId && confirm('Permanently delete this ticket and all its data?'))
      deleteTicketAction(selectedTicketId);
  }

  function handleKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape') { deselectTicket(); e.preventDefault(); }
  }
</script>

<svelte:window onkeydown={handleKeydown} />

{#if ticketDetail}
  <div class="flex flex-col h-full">
    <!-- Header -->
    <div class="px-4 py-3 border-b border-border">
      <div class="flex items-center gap-2 mb-2">
        <button
          class="text-xs text-muted hover:text-accent"
          onclick={deselectTicket}
          aria-label="Back to ticket list"
        >&larr; BACK</button>
        <div class="ml-auto flex gap-2">
          {#if isFailed || ticketDetail.Status === 'partial'}
            <button class="text-xs text-accent hover:text-text" onclick={handleRetry}>&#8635; RETRY</button>
          {/if}
          <button class="text-xs text-danger hover:text-text" onclick={handleDelete}>&#10007; DELETE</button>
        </div>
      </div>
      <h2 class="text-base font-bold text-text">{ticketDetail.Title}</h2>
      <div class="text-xs text-muted mt-1 flex gap-2 flex-wrap">
        <span class="{isFailed ? 'text-danger' : 'text-accent'}">{ticketDetail.Status.toUpperCase()}</span>
        <span>&middot; {formatSender(ticketDetail.ChannelSenderID)}</span>
        <span>&middot; {formatRelative(ticketDetail.StartedAt || ticketDetail.CreatedAt)}</span>
        {#if ticketDetail.PRURL}
          <a href={ticketDetail.PRURL} target="_blank" class="text-accent hover:underline">
            PR #{ticketDetail.PRNumber || 'link'}
          </a>
        {/if}
      </div>
    </div>

    <!-- Clarification -->
    {#if ticketDetail.ClarificationRequestedAt}
      <div class="mx-4 mt-3 p-2 border border-warning/30 bg-warning/5 text-xs">
        <div class="text-warning">&#10067; {ticketDetail.ErrorMessage || 'Clarification requested'}</div>
        {#if ticketDetail.Comments?.length}
          <div class="text-text mt-1">{ticketDetail.Comments[ticketDetail.Comments.length - 1].Body}</div>
        {/if}
      </div>
    {/if}

    <!-- DAG View -->
    <div class="px-4 mt-3">
      <DagView tasks={ticketTasks} />
    </div>

    <!-- Tab bar -->
    <div class="flex px-4 mt-3 gap-1 border-b border-border">
      {#each [['tasks', `TASKS ${tasksDone}/${ticketTasks.length}`], ['activity', 'ACTIVITY'], ['cost', 'COST']] as [key, label]}
        <button
          class="text-xs py-2 px-3 border-b-2 {activeTab === key ? 'border-accent text-accent' : 'border-transparent text-muted hover:text-text'}"
          onclick={() => { activeTab = key as any; }}
        >{label}</button>
      {/each}
    </div>

    <!-- Tab content -->
    <div class="flex-1 overflow-y-auto">
      {#if activeTab === 'tasks'}
        <div class="p-4 space-y-1">
          {#each ticketTasks as task (task.ID)}
            <TaskCard {task} events={ticketEvents} />
          {/each}
          {#if ticketTasks.length === 0}
            <div class="text-center text-muted text-sm py-8">No tasks yet.</div>
          {/if}
        </div>
      {:else if activeTab === 'activity'}
        <ActivityStream events={ticketEvents} tasks={ticketTasks} />
      {:else if activeTab === 'cost'}
        <div class="p-4">
          <CostBreakdown ticket={ticketDetail} llmCalls={ticketLlmCalls} />
        </div>
      {/if}
    </div>
  </div>
{/if}
```

**Step 2: Commit**

```bash
git add internal/dashboard/web/src/components/TicketDetail.svelte
git commit -m "feat(dashboard): add TicketDetail component with tabs for tasks, activity, cost"
```

---

### Task 11: LiveFeed Component

**Files:**
- Create: `internal/dashboard/web/src/components/LiveFeed.svelte`

**Step 1: Create LiveFeed.svelte**

Port from `index.html:298-321`. Collapsible right panel with dot visualization when collapsed.

```svelte
<script lang="ts">
  import { events, feedCollapsed, selectTicket } from '../state.svelte';
  import { formatTime, severityIcon, formatSender } from '../format';

  function toggleCollapse() {
    feedCollapsed = !feedCollapsed;
    localStorage.setItem('feed_collapsed', String(feedCollapsed));
  }
</script>

<section class="flex flex-col h-full border-l border-border bg-surface {feedCollapsed ? 'w-8' : 'w-72'}
  transition-[width] duration-200">
  <div class="flex items-center justify-between px-2 py-2 border-b border-border">
    {#if !feedCollapsed}
      <span class="text-xs text-muted font-bold tracking-wider">LIVE FEED</span>
    {/if}
    <button
      class="text-xs text-muted hover:text-accent"
      onclick={toggleCollapse}
      aria-label={feedCollapsed ? 'Expand feed' : 'Collapse feed'}
    >{feedCollapsed ? '\u25B6' : '\u25C0'}</button>
  </div>

  {#if !feedCollapsed}
    <div class="flex-1 overflow-y-auto">
      {#each events as evt (evt.ID)}
        <div class="px-2 py-1.5 border-b border-border text-xs hover:bg-surface-hover
          {evt.isNew ? 'animate-fade-in bg-accent/5' : ''}">
          <div class="flex gap-1.5 items-start">
            <span class="text-muted shrink-0">{formatTime(evt.CreatedAt)}</span>
            <span class="{
              evt.Severity === 'success' ? 'text-success' :
              evt.Severity === 'error' ? 'text-danger' :
              evt.Severity === 'warning' ? 'text-warning' :
              'text-muted'
            }">{severityIcon(evt.Severity)}</span>
            <span class="text-text">{evt.event_type || evt.EventType}</span>
          </div>
          {#if evt.Message}
            <div class="text-muted ml-5 truncate">{evt.Message}</div>
          {/if}
          {#if evt.ticket_title || evt.TicketTitle}
            <div class="ml-5 mt-0.5">
              <button
                class="text-accent/70 hover:text-accent text-xs cursor-pointer"
                onclick={() => selectTicket(evt.TicketID)}
              >[{evt.ticket_title || evt.TicketTitle}]</button>
              <span class="text-muted">{formatSender(evt.submitter || '')}</span>
            </div>
          {/if}
        </div>
      {/each}
    </div>
  {:else}
    <!-- Collapsed: severity dots -->
    <div class="flex flex-col items-center gap-0.5 py-2 overflow-hidden">
      {#each events.slice(0, 50) as evt (evt.ID)}
        <span class="w-1.5 h-1.5 rounded-full {
          evt.Severity === 'success' ? 'bg-success' :
          evt.Severity === 'error' ? 'bg-danger' :
          evt.Severity === 'warning' ? 'bg-warning' :
          'bg-muted'
        }"></span>
      {/each}
    </div>
  {/if}
</section>
```

**Step 2: Commit**

```bash
git add internal/dashboard/web/src/components/LiveFeed.svelte
git commit -m "feat(dashboard): add LiveFeed component with collapsible dot view"
```

---

### Task 12: TeamSummary Component (Home View)

**Files:**
- Create: `internal/dashboard/web/src/components/TeamSummary.svelte`

**Step 1: Create TeamSummary.svelte**

Port from `index.html:74-188`. Shows needs-attention, today stats, weekly costs, team stats, recent PRs.

```svelte
<script lang="ts">
  import {
    tickets, teamStats, recentPRs, weekDays,
    dailyCost, dailyBudget, monthlyCost, monthlyBudget, weeklyCost,
    selectTicket,
  } from '../state.svelte';
  import { ACTIVE_STATUSES, DONE_STATUSES, FAIL_STATUSES } from '../types';
  import { formatSender, formatRelative, formatCost } from '../format';

  const STUCK_THRESHOLD_MS = 30 * 60 * 1000;

  let needsAttention = $derived(
    tickets.filter(t => {
      if (FAIL_STATUSES.includes(t.Status)) return true;
      if (t.Status === 'clarification_needed') return true;
      if (ACTIVE_STATUSES.includes(t.Status) && t.UpdatedAt) {
        return Date.now() - new Date(t.UpdatedAt).getTime() > STUCK_THRESHOLD_MS;
      }
      return false;
    })
  );

  let todayStr = new Date().toISOString().slice(0, 10);
  let todayTickets = $derived(tickets.filter(t => t.CreatedAt?.slice(0, 10) === todayStr));
  let todayActive = $derived(todayTickets.filter(t => ACTIVE_STATUSES.includes(t.Status)).length);
  let todayDone = $derived(todayTickets.filter(t => DONE_STATUSES.includes(t.Status)).length);
  let todayFailed = $derived(todayTickets.filter(t => FAIL_STATUSES.includes(t.Status)).length);

  let maxWeekCost = $derived(Math.max(1, ...weekDays.map(d => d.cost_usd || 0)));

  function barWidth(cost: number): string {
    const chars = Math.round((cost / maxWeekCost) * 16);
    return '\u2588'.repeat(chars) || '\u00B7';
  }

  function dayLabel(dateStr: string): string {
    return ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'][new Date(dateStr).getDay()];
  }
</script>

<div class="p-4 space-y-4 overflow-y-auto h-full">
  <!-- Needs Attention -->
  {#if needsAttention.length > 0}
    <section>
      <div class="flex justify-between text-xs text-muted font-bold tracking-wider mb-2">
        <span>&#9888; NEEDS ATTENTION</span>
        <span>{needsAttention.length}</span>
      </div>
      {#each needsAttention as t}
        <button
          class="w-full text-left px-2 py-1 text-xs hover:bg-surface-hover flex justify-between cursor-pointer"
          onclick={() => selectTicket(t.ID)}
        >
          <span>
            {#if FAIL_STATUSES.includes(t.Status)}<span class="text-danger">&#10007; </span>{/if}
            {#if t.Status === 'clarification_needed'}<span class="text-warning">&#10067; </span>{/if}
            {t.Title}
          </span>
          <span class="text-muted">{t.Status} &middot; {formatSender(t.ChannelSenderID)}</span>
        </button>
      {/each}
    </section>
  {/if}

  <!-- Today -->
  <section>
    <div class="flex justify-between text-xs text-muted font-bold tracking-wider mb-2">
      <span>TODAY</span>
      <span>{todayStr}</span>
    </div>
    <div class="grid grid-cols-4 gap-2 text-xs">
      <div><span class="text-muted">Tickets</span><br>{todayTickets.length}</div>
      <div><span class="text-muted">Active</span><br>{todayActive}</div>
      <div><span class="text-muted">Merged</span><br><span class="text-success">&#10003; {todayDone}</span></div>
      <div><span class="text-muted">Failed</span><br>
        <span class="{todayFailed > 0 ? 'text-danger' : 'text-muted'}">{todayFailed > 0 ? `\u2717 ${todayFailed}` : '--'}</span>
      </div>
    </div>
    <div class="text-xs text-muted mt-2 space-y-0.5">
      <div>Daily:   {formatCost(dailyCost)} / ${Math.round(dailyBudget)}</div>
      <div>Monthly: {formatCost(monthlyCost)} / ${Math.round(monthlyBudget)}</div>
    </div>
  </section>

  <!-- This Week -->
  <section>
    <div class="flex justify-between text-xs text-muted font-bold tracking-wider mb-2">
      <span>THIS WEEK</span>
      <span>{formatCost(weeklyCost)}</span>
    </div>
    {#if weekDays.length === 0}
      <div class="text-xs text-muted">No cost data yet.</div>
    {:else}
      {#each weekDays as day}
        <div class="flex gap-2 text-xs items-center">
          <span class="w-8 text-muted">{dayLabel(day.date)}</span>
          <span class="text-accent font-mono flex-1">{barWidth(day.cost_usd || 0)}</span>
          <span class="text-muted">{formatCost(day.cost_usd || 0)}</span>
        </div>
      {/each}
    {/if}
  </section>

  <!-- Team -->
  <section>
    <div class="flex justify-between text-xs text-muted font-bold tracking-wider mb-2">
      <span>TEAM</span>
      <span>{teamStats.length} submitters</span>
    </div>
    {#if teamStats.length === 0}
      <div class="text-xs text-muted">No team activity this week.</div>
    {:else}
      {#each teamStats as stat}
        <div class="flex gap-2 text-xs items-center py-0.5">
          <span class="flex-1 truncate">{formatSender(stat.channel_sender_id)}</span>
          <span class="text-muted">{stat.ticket_count} tickets</span>
          <span>{formatCost(stat.cost_usd)}</span>
          {#if stat.failed_count > 0}
            <span class="text-danger">&#10007;{stat.failed_count}</span>
          {/if}
        </div>
      {/each}
    {/if}
  </section>

  <!-- Recent PRs -->
  <section>
    <div class="flex justify-between text-xs text-muted font-bold tracking-wider mb-2">
      <span>RECENT PRS</span>
      <span>{recentPRs.length}</span>
    </div>
    {#if recentPRs.length === 0}
      <div class="text-xs text-muted">No merged PRs yet.</div>
    {:else}
      {#each recentPRs as pr}
        <div class="text-xs py-0.5">
          <a href={pr.PRURL} target="_blank" class="text-accent hover:underline">
            {pr.PRNumber ? `#${pr.PRNumber} ` : ''}{pr.Title}
          </a>
          <span class="text-muted">{pr.Status} {formatRelative(pr.UpdatedAt)}</span>
        </div>
      {/each}
    {/if}
  </section>
</div>
```

**Step 2: Commit**

```bash
git add internal/dashboard/web/src/components/TeamSummary.svelte
git commit -m "feat(dashboard): add TeamSummary home view component"
```

---

### Task 13: SystemHealth Component

**Files:**
- Create: `internal/dashboard/web/src/components/SystemHealth.svelte`

**Step 1: Create SystemHealth.svelte**

New panel showing agent health, MCP servers, queue depth, cost gauges, throughput. Data from existing `/api/status` and cost endpoints.

```svelte
<script lang="ts">
  import {
    daemonState, wsConnected, whatsapp, mcpServers,
    dailyCost, dailyBudget, monthlyCost, monthlyBudget, weeklyCost,
    activeCount, tickets, weekDays, deselectTicket,
  } from '../state.svelte';
  import { DONE_STATUSES, FAIL_STATUSES, ACTIVE_STATUSES } from '../types';
  import { formatCost } from '../format';

  let queuedCount = $derived(tickets.filter(t => t.Status === 'queued').length);
  let doneToday = $derived(
    tickets.filter(t => DONE_STATUSES.includes(t.Status) && t.CreatedAt?.slice(0, 10) === new Date().toISOString().slice(0, 10)).length
  );
  let failedCount = $derived(tickets.filter(t => FAIL_STATUSES.includes(t.Status)).length);
  let successRate = $derived(
    tickets.length > 0 ? Math.round((tickets.filter(t => DONE_STATUSES.includes(t.Status)).length / tickets.length) * 100) : 0
  );

  function budgetPct(used: number, budget: number): number {
    if (!budget) return 0;
    return Math.min(100, (used / budget) * 100);
  }

  function budgetColor(pct: number): string {
    if (pct >= 90) return 'bg-danger';
    if (pct >= 80) return 'bg-warning';
    return 'bg-accent';
  }
</script>

<div class="p-4 space-y-4 overflow-y-auto h-full">
  <div class="flex items-center justify-between mb-2">
    <h2 class="text-xs text-muted font-bold tracking-wider">SYSTEM HEALTH</h2>
    <button class="text-xs text-muted hover:text-accent" onclick={deselectTicket}>&larr; BACK</button>
  </div>

  <!-- Daemon -->
  <section class="border border-border p-3 space-y-2">
    <div class="text-xs font-bold text-muted">DAEMON</div>
    <div class="flex items-center gap-2 text-sm">
      <span class="w-2 h-2 rounded-full {daemonState === 'running' ? 'bg-success' : 'bg-warning'}"></span>
      <span>{daemonState.toUpperCase()}</span>
      <span class="text-muted">|</span>
      <span class="w-2 h-2 rounded-full {wsConnected ? 'bg-success' : 'bg-danger'}"></span>
      <span class="text-xs text-muted">WebSocket {wsConnected ? 'connected' : 'disconnected'}</span>
    </div>
    {#if whatsapp !== null}
      <div class="flex items-center gap-2 text-xs">
        <span class="w-2 h-2 rounded-full {whatsapp ? 'bg-success' : 'bg-danger'}"></span>
        <span>WhatsApp {whatsapp ? 'connected' : 'disconnected'}</span>
      </div>
    {/if}
  </section>

  <!-- MCP Servers -->
  {#if Object.keys(mcpServers).length > 0}
    <section class="border border-border p-3 space-y-2">
      <div class="text-xs font-bold text-muted">MCP SERVERS</div>
      {#each Object.entries(mcpServers) as [name, info]}
        <div class="flex items-center gap-2 text-xs">
          <span class="w-2 h-2 rounded-full {info.status === 'ok' ? 'bg-success' : 'bg-danger'}"></span>
          <span>{name}</span>
          {#if info.error}
            <span class="text-danger">{info.error}</span>
          {/if}
        </div>
      {/each}
    </section>
  {/if}

  <!-- Queue -->
  <section class="border border-border p-3">
    <div class="text-xs font-bold text-muted mb-2">PIPELINE</div>
    <div class="grid grid-cols-3 gap-2 text-xs">
      <div><span class="text-muted">Queued</span><br><span class="text-lg">{queuedCount}</span></div>
      <div><span class="text-muted">Active</span><br><span class="text-lg text-accent">{activeCount}</span></div>
      <div><span class="text-muted">Failed</span><br><span class="text-lg {failedCount > 0 ? 'text-danger' : 'text-muted'}">{failedCount}</span></div>
    </div>
  </section>

  <!-- Cost Gauges -->
  <section class="border border-border p-3 space-y-3">
    <div class="text-xs font-bold text-muted">COST BUDGETS</div>
    {#each [
      { label: 'Daily', used: dailyCost, budget: dailyBudget },
      { label: 'Monthly', used: monthlyCost, budget: monthlyBudget },
    ] as gauge}
      {@const pct = budgetPct(gauge.used, gauge.budget)}
      <div class="text-xs">
        <div class="flex justify-between mb-1">
          <span class="text-muted">{gauge.label}</span>
          <span>{formatCost(gauge.used)} / ${Math.round(gauge.budget)}</span>
        </div>
        <div class="h-2 bg-border rounded overflow-hidden">
          <div class="h-full {budgetColor(pct)} transition-all" style="width:{pct}%"></div>
        </div>
      </div>
    {/each}
  </section>

  <!-- Throughput -->
  <section class="border border-border p-3">
    <div class="text-xs font-bold text-muted mb-2">THROUGHPUT</div>
    <div class="grid grid-cols-2 gap-2 text-xs">
      <div><span class="text-muted">Done today</span><br>{doneToday}</div>
      <div><span class="text-muted">Success rate</span><br>{successRate}%</div>
    </div>
  </section>
</div>
```

**Step 2: Commit**

```bash
git add internal/dashboard/web/src/components/SystemHealth.svelte
git commit -m "feat(dashboard): add SystemHealth observability panel"
```

---

### Task 14: Toast Notifications Component

**Files:**
- Create: `internal/dashboard/web/src/components/Toasts.svelte`

**Step 1: Create Toasts.svelte**

Floating toast notifications at bottom-right. Auto-dismiss, click to navigate.

```svelte
<script lang="ts">
  import { toasts, selectTicket } from '../state.svelte';
  import { severityIcon } from '../format';
</script>

{#if toasts.length > 0}
  <div class="fixed bottom-16 right-4 z-50 space-y-2 md:bottom-4" role="alert">
    {#each toasts as toast (toast.id)}
      <div
        class="flex items-center gap-2 px-3 py-2 bg-surface border border-border text-xs shadow-lg
          animate-fade-in max-w-xs
          {toast.severity === 'error' ? 'border-l-2 border-l-danger' : 'border-l-2 border-l-success'}"
      >
        <span class="{toast.severity === 'error' ? 'text-danger' : 'text-success'}">
          {severityIcon(toast.severity)}
        </span>
        {#if toast.ticketId}
          <button
            class="text-text hover:text-accent cursor-pointer"
            onclick={() => selectTicket(toast.ticketId!)}
          >{toast.message}</button>
        {:else}
          <span>{toast.message}</span>
        {/if}
      </div>
    {/each}
  </div>
{/if}
```

**Step 2: Commit**

```bash
git add internal/dashboard/web/src/components/Toasts.svelte
git commit -m "feat(dashboard): add Toast notification component"
```

---

### Task 15: Assemble App.svelte (Full Layout)

**Files:**
- Modify: `internal/dashboard/web/src/App.svelte`

**Step 1: Rewrite App.svelte**

Wire all components together. Three-panel desktop layout, mobile tabs, auth gate, lifecycle.

```svelte
<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import { setToken, getToken } from './api';
  import {
    activePanel, selectedTicketId,
    startPolling, stopPolling, connectWebSocket, restoreFromURL,
  } from './state.svelte';
  import Header from './components/Header.svelte';
  import TicketList from './components/TicketList.svelte';
  import TicketDetail from './components/TicketDetail.svelte';
  import TeamSummary from './components/TeamSummary.svelte';
  import SystemHealth from './components/SystemHealth.svelte';
  import LiveFeed from './components/LiveFeed.svelte';
  import Toasts from './components/Toasts.svelte';

  let authenticated = $state(!!getToken());

  let tokenInput = $state('');

  function handleAuth() {
    if (tokenInput.trim()) {
      setToken(tokenInput.trim());
      authenticated = true;
    }
  }

  onMount(() => {
    if (authenticated) {
      startPolling();
      connectWebSocket();
      restoreFromURL();
    }

    // Handle browser back/forward
    window.addEventListener('popstate', restoreFromURL);
  });

  onDestroy(() => {
    stopPolling();
    window.removeEventListener('popstate', restoreFromURL);
  });

  function handleKeydown(e: KeyboardEvent) {
    // Don't handle shortcuts when typing in inputs
    if (e.target instanceof HTMLInputElement || e.target instanceof HTMLTextAreaElement) return;
    if (e.key === '?' && !e.ctrlKey && !e.metaKey) {
      // TODO: show keyboard shortcut overlay
    }
  }
</script>

<svelte:window onkeydown={handleKeydown} />

{#if !authenticated}
  <!-- Auth gate -->
  <div class="min-h-screen bg-bg text-text font-mono flex items-center justify-center">
    <div class="border border-border p-6 bg-surface w-80 space-y-4">
      <h1 class="text-accent font-bold text-lg text-center">FOREMAN</h1>
      <input
        type="password"
        bind:value={tokenInput}
        placeholder="Enter auth token..."
        class="w-full bg-bg border border-border px-3 py-2 text-sm text-text placeholder:text-muted focus:border-accent outline-none"
        onkeydown={(e) => e.key === 'Enter' && handleAuth()}
      />
      <button
        class="w-full py-2 bg-accent text-bg font-bold text-sm hover:opacity-90"
        onclick={handleAuth}
      >AUTHENTICATE</button>
    </div>
  </div>
{:else}
  <div class="min-h-screen bg-bg text-text font-mono flex flex-col">
    <Header />

    <main class="flex-1 flex overflow-hidden">
      <!-- Left: Ticket List -->
      <div class="hidden md:flex w-64 shrink-0 {activePanel === 'tickets' ? 'flex' : ''} md:!flex">
        <TicketList />
      </div>
      <!-- Mobile: show based on activePanel -->
      <div class="flex md:hidden w-full {activePanel === 'tickets' ? 'flex' : 'hidden'}">
        <TicketList />
      </div>

      <!-- Center: Detail / Team Summary / System Health -->
      <div class="hidden md:flex flex-1 min-w-0 {activePanel === 'detail' ? 'flex' : ''} md:!flex">
        {#if activePanel === 'health'}
          <SystemHealth />
        {:else if selectedTicketId}
          <TicketDetail />
        {:else}
          <TeamSummary />
        {/if}
      </div>
      <div class="flex md:hidden w-full {activePanel === 'detail' || activePanel === 'health' ? 'flex' : 'hidden'}">
        {#if activePanel === 'health'}
          <SystemHealth />
        {:else if selectedTicketId}
          <TicketDetail />
        {:else}
          <TeamSummary />
        {/if}
      </div>

      <!-- Right: Live Feed -->
      <div class="hidden md:flex shrink-0">
        <LiveFeed />
      </div>
      <div class="flex md:hidden w-full {activePanel === 'feed' ? 'flex' : 'hidden'}">
        <LiveFeed />
      </div>
    </main>

    <!-- Mobile tab bar -->
    <nav class="flex md:hidden border-t border-border bg-surface" aria-label="Navigation">
      {#each [
        { key: 'tickets', icon: '\u2630', label: 'TICKETS' },
        { key: 'detail', icon: '\u25B6', label: 'DETAIL' },
        { key: 'feed', icon: '\u26A1', label: 'FEED' },
        { key: 'health', icon: '\u2699', label: 'SYSTEM' },
      ] as tab}
        <button
          class="flex-1 py-2 text-center text-xs {activePanel === tab.key ? 'text-accent border-t-2 border-accent' : 'text-muted'}"
          onclick={() => { activePanel = tab.key as any; }}
        >{tab.icon}<br>{tab.label}</button>
      {/each}
    </nav>

    <!-- Footer -->
    <footer class="hidden md:flex items-center justify-center gap-4 px-4 py-1 border-t border-border text-xs text-muted bg-surface">
      <span>DAILY: {dailyCost.toFixed(2)} / ${dailyBudget.toFixed(0)}</span>
      <span>|</span>
      <span>WEEKLY: ${weeklyCost.toFixed(2)}</span>
      <span>|</span>
      <span>MONTHLY: ${monthlyCost.toFixed(2)} / ${monthlyBudget.toFixed(0)}</span>
    </footer>

    <Toasts />
  </div>
{/if}
```

Note: The footer references cost variables directly — import them in the script block:
```ts
import { dailyCost, dailyBudget, weeklyCost, monthlyCost, monthlyBudget } from './state.svelte';
```

**Step 2: Build and verify**

```bash
cd internal/dashboard/web && npm run build
```
Expected: successful build, `dist/` contains `index.html` + `assets/`.

**Step 3: Commit**

```bash
git add internal/dashboard/web/src/App.svelte
git commit -m "feat(dashboard): assemble full App layout with all components"
```

---

### Task 16: Add Tailwind Animations

**Files:**
- Modify: `internal/dashboard/web/src/app.css`

**Step 1: Add custom animations**

```css
@import "tailwindcss";

@theme {
  --color-bg: #0a0a0a;
  --color-surface: #111111;
  --color-surface-hover: #1a1a1a;
  --color-border: #2a2a2a;
  --color-accent: #FFE600;
  --color-accent-dim: #8B7D00;
  --color-text: #F0F0F0;
  --color-muted: #888888;
  --color-danger: #FF4444;
  --color-success: #00CC66;
  --color-warning: #FFB020;
  --font-family-mono: "JetBrains Mono", "SF Mono", "Fira Code", "Cascadia Code", ui-monospace, monospace;
  --animate-fade-in: fade-in 0.3s ease-out;
}

@keyframes fade-in {
  from { opacity: 0; transform: translateY(-4px); }
  to { opacity: 1; transform: translateY(0); }
}

/* Base styles */
body {
  @apply bg-bg text-text font-mono m-0 p-0;
  -webkit-font-smoothing: antialiased;
}

/* Scrollbar styling */
::-webkit-scrollbar { width: 6px; }
::-webkit-scrollbar-track { background: var(--color-bg); }
::-webkit-scrollbar-thumb { background: var(--color-border); border-radius: 3px; }
::-webkit-scrollbar-thumb:hover { background: var(--color-muted); }

/* Respect reduced motion */
@media (prefers-reduced-motion: reduce) {
  *, *::before, *::after {
    animation-duration: 0.01ms !important;
    transition-duration: 0.01ms !important;
  }
}
```

**Step 2: Commit**

```bash
git add internal/dashboard/web/src/app.css
git commit -m "feat(dashboard): add Tailwind theme, animations, and base styles"
```

---

### Task 17: Backend — Activity Endpoint

**Files:**
- Modify: `internal/dashboard/api.go` (add handler)
- Modify: `internal/dashboard/server.go:67-85` (add route)
- Modify: `internal/db/db.go` (add interface method)
- Modify: `internal/db/sqlite.go` (add implementation)

**Step 1: Add `GetTicketActivity` to DashboardDB interface**

In `internal/dashboard/api.go`, add to `DashboardDB` interface (after line 48):

```go
GetTicketActivity(ctx context.Context, ticketID string, limit int) ([]models.EventRecord, error)
```

**Step 2: Add handler in api.go**

Add a new handler method:

```go
func (a *API) handleGetTicketActivity(w http.ResponseWriter, r *http.Request) {
	id := extractTicketID(r.URL.Path)
	if id == "" {
		http.Error(w, "missing ticket ID", http.StatusBadRequest)
		return
	}

	limit := 100
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 && n <= 500 {
			limit = n
		}
	}

	events, err := a.db.GetTicketActivity(r.Context(), id, limit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, events)
}
```

**Step 3: Register route in server.go**

In the `tickets/` handler switch (line 69-84), add before the default case:

```go
case strings.HasSuffix(path, "/activity"):
	api.handleGetTicketActivity(w, r)
```

**Step 4: Add Database interface method and implement in sqlite.go**

Add `GetTicketActivity` to `db.Database` interface in `db/db.go`.

Implement in `sqlite.go` — this is essentially the same as `GetEvents` but ordered chronologically (ASC) for the activity stream:

```go
func (s *SQLiteDB) GetTicketActivity(ctx context.Context, ticketID string, limit int) ([]models.EventRecord, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, created_at, ticket_id, task_id, event_type, severity, message, details
		 FROM events WHERE ticket_id = ? ORDER BY created_at DESC LIMIT ?`,
		ticketID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	// ... scan rows same as GetEvents
}
```

**Step 5: Verify build**

```bash
go build ./...
```

**Step 6: Write test for the new endpoint**

Create or extend the API test file to test `GET /api/tickets/{id}/activity`.

**Step 7: Run tests**

```bash
go test ./internal/dashboard/... -v
```

**Step 8: Commit**

```bash
git add internal/dashboard/api.go internal/dashboard/server.go internal/db/db.go internal/db/sqlite.go
git commit -m "feat(dashboard): add GET /api/tickets/{id}/activity endpoint"
```

---

### Task 18: Backend — WebSocket Sequence Numbers

**Files:**
- Modify: `internal/models/ticket.go` (add Seq to EventRecord)
- Modify: `internal/telemetry/events.go` (add atomic sequence counter)
- Modify: `internal/dashboard/ws.go` (include seq in enriched event)

**Step 1: Add Seq field to EventRecord**

In `internal/models/ticket.go`, add to `EventRecord` struct:

```go
Seq int64 `json:"seq,omitempty"`
```

**Step 2: Add atomic sequence counter to EventEmitter**

In `internal/telemetry/events.go`, add field to `EventEmitter`:

```go
seq int64 // monotonic sequence number for WebSocket gap detection
```

In the `Emit` method, before broadcasting, set:

```go
evt.Seq = atomic.AddInt64(&e.seq, 1)
```

**Step 3: Verify build and tests pass**

```bash
go build ./... && go test ./internal/telemetry/... -v
```

**Step 4: Commit**

```bash
git add internal/models/ticket.go internal/telemetry/events.go internal/dashboard/ws.go
git commit -m "feat(dashboard): add sequence numbers to WebSocket events for gap detection"
```

---

### Task 19: Remove Old Frontend Files

**Files:**
- Delete: `internal/dashboard/web/app.js`
- Delete: `internal/dashboard/web/style.css`
- Delete: `internal/dashboard/web/index.html` (the old one at web/ root — new one is at `web/index.html` for Vite entry)

**Step 1: Verify the new build works end-to-end**

```bash
cd internal/dashboard/web && npm run build && cd ../../.. && go build ./...
```

**Step 2: Remove old files**

```bash
git rm internal/dashboard/web/app.js internal/dashboard/web/style.css
```

Note: Keep `internal/dashboard/web/index.html` as it's now the Vite entry point. The old Alpine.js/HTMX script tags are replaced with the Vite module entry.

**Step 3: Run full test suite**

```bash
go test ./... -race -count=1
```

**Step 4: Commit**

```bash
git add -A
git commit -m "chore(dashboard): remove old Alpine.js frontend files"
```

---

### Task 20: Integration Testing & Polish

**Files:**
- Potentially any component files for fixes

**Step 1: Build the full stack**

```bash
cd internal/dashboard/web && npm ci && npm run build && cd ../../.. && go build -o foreman .
```

**Step 2: Start the daemon and verify dashboard**

```bash
./foreman start &
# Open browser to http://localhost:8080
# Verify:
# - Auth gate appears
# - After auth: three-panel layout renders
# - WebSocket connects (header shows RUNNING)
# - Ticket list loads with search/filter
# - Clicking a ticket shows detail with tabs
# - Activity tab shows events
# - DAG view renders for tickets with task dependencies
# - Live feed shows events in real-time
# - System Health panel accessible via SYSTEM button
# - Mobile layout works (resize browser)
# - Keyboard nav: j/k, Enter, Escape, 1-4
# - Toast notifications appear for ticket completion
```

**Step 3: Fix any rendering/styling issues**

Adjust Tailwind classes as needed for visual polish.

**Step 4: Verify TypeScript has no errors**

```bash
cd internal/dashboard/web && npx tsc --noEmit
```

**Step 5: Final commit**

```bash
git add -A
git commit -m "feat(dashboard): complete Svelte 5 real-time dashboard migration"
```

---

## Summary

| Task | Description | Key Files |
|------|-------------|-----------|
| 1 | Scaffold Svelte + Vite + Tailwind | package.json, vite.config.ts, server.go |
| 2 | Types, API client, formatters | types.ts, api.ts, format.ts |
| 3 | Shared reactive state | state.svelte.ts |
| 4 | Header component | Header.svelte |
| 5 | TicketList component | TicketList.svelte |
| 6 | TaskCard component | TaskCard.svelte |
| 7 | ActivityStream component | ActivityStream.svelte |
| 8 | DagView component | DagView.svelte |
| 9 | CostBreakdown component | CostBreakdown.svelte |
| 10 | TicketDetail component | TicketDetail.svelte |
| 11 | LiveFeed component | LiveFeed.svelte |
| 12 | TeamSummary component | TeamSummary.svelte |
| 13 | SystemHealth component | SystemHealth.svelte |
| 14 | Toast notifications | Toasts.svelte |
| 15 | App.svelte full assembly | App.svelte |
| 16 | Tailwind animations & theme | app.css |
| 17 | Backend: activity endpoint | api.go, server.go, db.go, sqlite.go |
| 18 | Backend: WS sequence numbers | ticket.go, events.go, ws.go |
| 19 | Remove old frontend | app.js, style.css |
| 20 | Integration testing & polish | all |
