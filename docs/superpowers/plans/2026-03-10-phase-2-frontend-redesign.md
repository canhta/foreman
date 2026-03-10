# Phase 2: Frontend Redesign — Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Redesign the Svelte frontend from a single-project SPA to a multi-project dashboard with sidebar navigation, project boards, project dashboards, and a project settings editor.

**Architecture:** The current frontend is a single-page Svelte 5 app with no routing — everything lives in `App.svelte` with URL query params for state. We'll add `svelte-spa-router` for client-side routing, restructure the layout into a persistent sidebar + content area, and create new page components for the global overview, project board, project dashboard, project settings, and project creation wizard. Existing components (TicketDetail, TaskCard, CostBreakdown, etc.) will be adapted and reused within the new project-scoped pages.

**Tech Stack:** Svelte 5, TypeScript, Vite 6, Tailwind CSS 4, svelte-spa-router

**Spec:** `docs/superpowers/specs/2026-03-10-multi-project-refactor-design.md` (Sections 4-10)

**Prerequisite:** Phase 1b (multi-project backend) must be completed. The backend already has project-scoped API endpoints (`/api/projects`, `/api/projects/:pid/tickets`, etc.) and WebSocket channels (`/ws/projects/:pid`, `/ws/global`).

---

## File Structure

```
src/
├── main.ts                         # Entry point (mount App)
├── App.svelte                      # Shell: sidebar + router outlet
├── app.css                         # Global styles (keep existing theme)
├── api.ts                          # HTTP client (updated for project-scoped URLs)
├── types.ts                        # TypeScript types (add Project types)
├── format.ts                       # Formatting utilities (keep)
├── state/
│   ├── global.svelte.ts            # Global state (projects list, auth, overview metrics)
│   ├── project.svelte.ts           # Per-project state (tickets, tasks, costs, WebSocket)
│   └── toasts.svelte.ts            # Toast notifications (extracted from old state)
├── pages/
│   ├── Login.svelte                # Auth gate (extracted from App.svelte)
│   ├── GlobalSetup.svelte          # First-time global config setup
│   ├── GlobalOverview.svelte       # Cross-project dashboard
│   ├── ProjectBoard.svelte         # Kanban board with ticket cards
│   ├── ProjectDashboard.svelte     # Per-project metrics
│   ├── ProjectSettings.svelte      # Config editor with test buttons
│   └── ProjectWizard.svelte        # New project creation wizard
├── components/
│   ├── Sidebar.svelte              # Persistent sidebar navigation
│   ├── ProjectTabs.svelte          # Board / Dashboard / Settings tabs
│   ├── TicketCard.svelte           # Board card (new, compact)
│   ├── TicketPanel.svelte          # Right side panel (ticket detail)
│   ├── TicketFullView.svelte       # Full page ticket view (with chat placeholder)
│   ├── TaskCard.svelte             # Existing (adapt)
│   ├── DagView.svelte              # Existing (keep)
│   ├── ActivityStream.svelte       # Existing (keep)
│   ├── CostBreakdown.svelte        # Existing (keep)
│   ├── SystemHealth.svelte         # Existing (adapt for per-project)
│   ├── LiveFeed.svelte             # Existing (adapt for project-scoped events)
│   ├── Toasts.svelte               # Existing (keep)
│   └── ConfirmDialog.svelte        # Existing (keep)
```

---

## Chunk 1: Routing & Layout Shell

### Task 1: Install Router and Set Up Routes

**Files:**
- Modify: `internal/dashboard/web/package.json`
- Create: `internal/dashboard/web/src/routes.ts`

- [ ] **Step 1: Install svelte-spa-router**

```bash
cd internal/dashboard/web && npm install svelte-spa-router
```

- [ ] **Step 2: Create routes.ts**

Create `src/routes.ts`:

```typescript
import { wrap } from 'svelte-spa-router/wrap';

import GlobalOverview from './pages/GlobalOverview.svelte';
import ProjectBoard from './pages/ProjectBoard.svelte';
import ProjectDashboard from './pages/ProjectDashboard.svelte';
import ProjectSettings from './pages/ProjectSettings.svelte';
import ProjectWizard from './pages/ProjectWizard.svelte';

export default {
  '/': GlobalOverview,
  '/projects/new': ProjectWizard,
  '/projects/:pid/board': ProjectBoard,
  '/projects/:pid/dashboard': ProjectDashboard,
  '/projects/:pid/settings': ProjectSettings,
};
```

- [ ] **Step 3: Create placeholder page components**

Create stub files for each page so routes compile:

`src/pages/GlobalOverview.svelte`:
```svelte
<script lang="ts">
</script>
<div class="p-6">
  <h1 class="text-lg font-bold tracking-widest text-[var(--color-accent)]">GLOBAL OVERVIEW</h1>
  <p class="text-[var(--color-muted)] mt-2">Coming soon</p>
</div>
```

Create identical stubs for `ProjectBoard.svelte`, `ProjectDashboard.svelte`, `ProjectSettings.svelte`, `ProjectWizard.svelte` with different titles.

- [ ] **Step 4: Verify build**

```bash
cd internal/dashboard/web && npm run build
```
Expected: SUCCESS

- [ ] **Step 5: Commit**

```bash
git add internal/dashboard/web/package.json internal/dashboard/web/package-lock.json internal/dashboard/web/src/routes.ts internal/dashboard/web/src/pages/
git commit -m "feat(ui): install router and create page stubs"
```

---

### Task 2: Create Sidebar Component

**Files:**
- Create: `internal/dashboard/web/src/components/Sidebar.svelte`

- [ ] **Step 1: Build sidebar component**

Create `src/components/Sidebar.svelte`:

```svelte
<script lang="ts">
  import { link } from 'svelte-spa-router';
  import { location } from 'svelte-spa-router';
  import { globalState } from '../state/global.svelte';

  let collapsed = $state(localStorage.getItem('sidebar_collapsed') === 'true');

  function toggle() {
    collapsed = !collapsed;
    localStorage.setItem('sidebar_collapsed', String(collapsed));
  }

  function statusIndicator(status: string): string {
    switch (status) {
      case 'running': return '●';
      case 'paused': return '⏸';
      case 'error': return '⚠';
      default: return '○';
    }
  }

  function statusColor(status: string): string {
    switch (status) {
      case 'running': return 'text-[var(--color-success)]';
      case 'error': return 'text-[var(--color-danger)]';
      case 'paused': return 'text-[var(--color-warning)]';
      default: return 'text-[var(--color-muted)]';
    }
  }

  function isActive(path: string): boolean {
    return $location === path || $location.startsWith(path + '/');
  }
</script>

<aside
  class="h-screen border-r border-[var(--color-border)] bg-[var(--color-bg)] flex flex-col transition-all duration-200"
  class:w-52={!collapsed}
  class:w-14={collapsed}
>
  <!-- Logo -->
  <div class="h-12 flex items-center px-4 border-b border-[var(--color-border)]">
    {#if !collapsed}
      <span class="text-sm font-bold tracking-[0.3em] text-[var(--color-accent)]">FOREMAN</span>
    {:else}
      <span class="text-sm font-bold text-[var(--color-accent)]">F</span>
    {/if}
  </div>

  <!-- Overview -->
  <nav class="flex-1 overflow-y-auto py-2">
    <a
      href="/"
      use:link
      class="flex items-center gap-2 px-4 py-2 text-xs tracking-widest hover:bg-[var(--color-surface-hover)] transition-colors"
      class:bg-[var(--color-accent-bg)]={isActive('/')}
      class:text-[var(--color-accent)]={isActive('/')}
      class:text-[var(--color-muted-bright)]={!isActive('/')}
    >
      {#if !collapsed}OVERVIEW{:else}◈{/if}
    </a>

    <!-- Projects section -->
    {#if !collapsed}
      <div class="px-4 pt-4 pb-1 text-[10px] tracking-[0.2em] text-[var(--color-muted)] uppercase">
        Projects
      </div>
    {:else}
      <div class="border-b border-[var(--color-border)] mx-2 my-2"></div>
    {/if}

    {#each globalState.projects as project}
      <a
        href="/projects/{project.id}/board"
        use:link
        class="flex items-center gap-2 px-4 py-2 text-xs hover:bg-[var(--color-surface-hover)] transition-colors group"
        class:bg-[var(--color-accent-bg)]={isActive(`/projects/${project.id}`)}
        class:text-[var(--color-text)]={isActive(`/projects/${project.id}`)}
        class:text-[var(--color-muted-bright)]={!isActive(`/projects/${project.id}`)}
      >
        <span class={statusColor(project.status)}>{statusIndicator(project.status)}</span>
        {#if !collapsed}
          <span class="truncate">{project.name}</span>
          {#if project.needsInput > 0}
            <span class="ml-auto text-[10px] bg-[var(--color-warning-bg)] text-[var(--color-warning)] px-1.5 rounded-sm">
              {project.needsInput}
            </span>
          {/if}
        {/if}
      </a>
    {/each}

    <a
      href="/projects/new"
      use:link
      class="flex items-center gap-2 px-4 py-2 text-xs text-[var(--color-muted)] hover:text-[var(--color-accent)] hover:bg-[var(--color-surface-hover)] transition-colors"
    >
      {#if !collapsed}+ Add Project{:else}+{/if}
    </a>
  </nav>

  <!-- Bottom section -->
  <div class="border-t border-[var(--color-border)] py-2">
    <button
      onclick={toggle}
      class="flex items-center gap-2 px-4 py-2 text-xs text-[var(--color-muted)] hover:text-[var(--color-text)] w-full text-left"
    >
      {#if !collapsed}◁ Collapse{:else}▷{/if}
    </button>
    <button
      onclick={() => globalState.logout()}
      class="flex items-center gap-2 px-4 py-2 text-xs text-[var(--color-muted)] hover:text-[var(--color-danger)] w-full text-left"
    >
      {#if !collapsed}Logout{:else}✕{/if}
    </button>
  </div>
</aside>
```

- [ ] **Step 2: Verify build**

```bash
cd internal/dashboard/web && npm run build
```
Expected: May fail until global state is created (Task 3). That's OK — move to Task 3.

- [ ] **Step 3: Commit**

```bash
git add internal/dashboard/web/src/components/Sidebar.svelte
git commit -m "feat(ui): add sidebar navigation component"
```

---

### Task 3: Refactor State Management

**Files:**
- Create: `internal/dashboard/web/src/state/global.svelte.ts`
- Create: `internal/dashboard/web/src/state/project.svelte.ts`
- Create: `internal/dashboard/web/src/state/toasts.svelte.ts`
- Modify: `internal/dashboard/web/src/types.ts`

- [ ] **Step 1: Add project types to types.ts**

Add to `src/types.ts`:

```typescript
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
```

- [ ] **Step 2: Create toasts state (extracted from old state)**

Create `src/state/toasts.svelte.ts`:

```typescript
export interface Toast {
  id: string;
  message: string;
  ticketId?: string;
  severity: string;
  createdAt: number;
}

class ToastState {
  toasts = $state<Toast[]>([]);

  add(message: string, severity = 'info', ticketId?: string) {
    const toast: Toast = {
      id: crypto.randomUUID(),
      message,
      ticketId,
      severity,
      createdAt: Date.now(),
    };
    this.toasts = [toast, ...this.toasts].slice(0, 10);
    setTimeout(() => this.remove(toast.id), 8000);
  }

  remove(id: string) {
    this.toasts = this.toasts.filter(t => t.id !== id);
  }
}

export const toasts = new ToastState();
```

- [ ] **Step 3: Create global state**

Create `src/state/global.svelte.ts`:

```typescript
import { fetchJSON, postJSON, clearToken, getToken, setOnUnauthorized } from '../api';
import type { ProjectEntry, ProjectOverview, StatusResponse } from '../types';
import { toasts } from './toasts.svelte';

class GlobalState {
  // Auth
  authenticated = $state(!!getToken());

  // Projects
  projects = $state<ProjectEntry[]>([]);

  // Overview metrics
  overview = $state<ProjectOverview>({ active_tickets: 0, open_prs: 0, need_input: 0, cost_today: 0, projects: 0 });

  // Global status
  daemonState = $state<string>('stopped');
  wsConnected = $state(false);

  // Global WebSocket
  private ws: WebSocket | null = null;
  private pollIntervals: number[] = [];

  async loadProjects() {
    try {
      const entries = await fetchJSON<ProjectEntry[]>('/api/projects');
      this.projects = entries;
    } catch (e) {
      console.error('loadProjects', e);
    }
  }

  async loadOverview() {
    try {
      this.overview = await fetchJSON<ProjectOverview>('/api/overview');
    } catch (e) {
      console.error('loadOverview', e);
    }
  }

  async createProject(config: Record<string, unknown>): Promise<string> {
    const result = await fetchJSON<{ id: string }>('/api/projects');
    // POST with body
    const res = await fetch('/api/projects', {
      method: 'POST',
      headers: { Authorization: `Bearer ${getToken()}`, 'Content-Type': 'application/json' },
      body: JSON.stringify(config),
    });
    if (!res.ok) throw new Error(await res.text());
    const data = await res.json();
    await this.loadProjects();
    return data.id;
  }

  async deleteProject(id: string) {
    await fetch(`/api/projects/${id}`, {
      method: 'DELETE',
      headers: { Authorization: `Bearer ${getToken()}` },
    });
    await this.loadProjects();
  }

  connectGlobalWebSocket() {
    const protocol = location.protocol === 'https:' ? 'wss:' : 'ws:';
    const url = `${protocol}//${location.host}/ws/global`;
    this.ws = new WebSocket(url, [`bearer.${getToken()}`]);
    this.ws.onopen = () => { this.wsConnected = true; };
    this.ws.onclose = () => {
      this.wsConnected = false;
      setTimeout(() => this.connectGlobalWebSocket(), 5000);
    };
    this.ws.onmessage = (ev) => {
      try {
        const event = JSON.parse(ev.data);
        // Route to project-specific handlers or global overview refresh
        if (event.severity === 'warning' || event.severity === 'error') {
          toasts.add(event.message, event.severity, event.ticket_id);
        }
      } catch {}
    };
  }

  startPolling() {
    this.loadProjects();
    this.loadOverview();
    this.pollIntervals.push(
      window.setInterval(() => this.loadProjects(), 30000),
      window.setInterval(() => this.loadOverview(), 15000),
    );
    this.connectGlobalWebSocket();
  }

  stopPolling() {
    this.pollIntervals.forEach(clearInterval);
    this.pollIntervals = [];
    this.ws?.close();
  }

  logout() {
    this.stopPolling();
    clearToken();
    this.authenticated = false;
    this.projects = [];
  }
}

export const globalState = new GlobalState();

setOnUnauthorized(() => globalState.logout());
```

- [ ] **Step 4: Create project state**

Create `src/state/project.svelte.ts`:

```typescript
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
        fetchJSON<ChatMessage[]>(`${this.base()}/tickets/${ticketId}/chat`).catch(() => []),
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
```

- [ ] **Step 5: Verify build**

```bash
cd internal/dashboard/web && npm run build
```
Expected: SUCCESS (or type warnings to fix)

- [ ] **Step 6: Commit**

```bash
git add internal/dashboard/web/src/state/ internal/dashboard/web/src/types.ts
git commit -m "feat(ui): add multi-project state management (global + project)"
```

---

### Task 4: Rewrite App.svelte as Shell

**Files:**
- Modify: `internal/dashboard/web/src/App.svelte`

- [ ] **Step 1: Rewrite App.svelte with sidebar + router**

Replace `src/App.svelte` with:

```svelte
<script lang="ts">
  import Router from 'svelte-spa-router';
  import routes from './routes';
  import Sidebar from './components/Sidebar.svelte';
  import Login from './pages/Login.svelte';
  import Toasts from './components/Toasts.svelte';
  import { globalState } from './state/global.svelte';
  import { setToken, getToken } from './api';

  function handleLogin(token: string) {
    setToken(token);
    globalState.authenticated = true;
    globalState.startPolling();
  }

  // Auto-start polling if already authenticated
  if (getToken()) {
    globalState.startPolling();
  }
</script>

{#if !globalState.authenticated}
  <Login onLogin={handleLogin} />
{:else}
  <div class="flex h-screen bg-[var(--color-bg)] text-[var(--color-text)]">
    <Sidebar />
    <main class="flex-1 overflow-y-auto">
      <Router {routes} />
    </main>
  </div>
  <Toasts />
{/if}
```

- [ ] **Step 2: Create Login page (extracted from old App.svelte auth gate)**

Create `src/pages/Login.svelte`:

```svelte
<script lang="ts">
  interface Props {
    onLogin: (token: string) => void;
  }
  let { onLogin }: Props = $props();

  let tokenInput = $state('');
  let error = $state('');

  async function handleSubmit() {
    error = '';
    try {
      const res = await fetch('/api/status', {
        headers: { Authorization: `Bearer ${tokenInput}` },
      });
      if (res.ok) {
        onLogin(tokenInput);
      } else {
        error = 'Invalid token';
      }
    } catch {
      error = 'Connection failed';
    }
  }
</script>

<div class="min-h-screen flex items-center justify-center bg-[var(--color-bg)]">
  <div class="w-80 border-2 border-[var(--color-border)] p-6">
    <h1 class="text-sm font-bold tracking-[0.3em] text-[var(--color-accent)] mb-6">FOREMAN</h1>
    <form onsubmit|preventDefault={handleSubmit}>
      <input
        type="password"
        bind:value={tokenInput}
        placeholder="ACCESS TOKEN"
        class="w-full bg-[var(--color-surface)] border border-[var(--color-border)] px-3 py-2 text-xs tracking-wider text-[var(--color-text)] placeholder-[var(--color-muted)] focus:border-[var(--color-accent)] focus:outline-none"
      />
      {#if error}
        <p class="text-[var(--color-danger)] text-xs mt-2">{error}</p>
      {/if}
      <button
        type="submit"
        class="w-full mt-4 bg-[var(--color-accent)] text-[var(--color-bg)] py-2 text-xs font-bold tracking-widest hover:opacity-90"
      >
        AUTHENTICATE
      </button>
    </form>
  </div>
</div>
```

- [ ] **Step 3: Verify build**

```bash
cd internal/dashboard/web && npm run build
```
Expected: SUCCESS

- [ ] **Step 4: Commit**

```bash
git add internal/dashboard/web/src/App.svelte internal/dashboard/web/src/pages/Login.svelte
git commit -m "feat(ui): rewrite App.svelte as sidebar+router shell with login page"
```

---

## Chunk 2: Global Overview Page

### Task 5: Build Global Overview

**Files:**
- Modify: `internal/dashboard/web/src/pages/GlobalOverview.svelte`

- [ ] **Step 1: Implement global overview page**

Replace `src/pages/GlobalOverview.svelte`:

```svelte
<script lang="ts">
  import { globalState } from '../state/global.svelte';
  import { link } from 'svelte-spa-router';

  function statusColor(status: string): string {
    switch (status) {
      case 'running': return 'text-[var(--color-success)]';
      case 'error': return 'text-[var(--color-danger)]';
      case 'paused': return 'text-[var(--color-warning)]';
      default: return 'text-[var(--color-muted)]';
    }
  }
</script>

<div class="p-6 max-w-6xl">
  <h1 class="text-sm font-bold tracking-[0.3em] text-[var(--color-accent)] mb-6">OVERVIEW</h1>

  <!-- Summary cards -->
  <div class="grid grid-cols-2 lg:grid-cols-4 gap-4 mb-8">
    <div class="border border-[var(--color-border)] p-4">
      <div class="text-[10px] tracking-widest text-[var(--color-muted)] uppercase">Cost Today</div>
      <div class="text-2xl font-bold mt-1">${globalState.overview.cost_today.toFixed(2)}</div>
    </div>
    <div class="border border-[var(--color-border)] p-4">
      <div class="text-[10px] tracking-widest text-[var(--color-muted)] uppercase">Active Tickets</div>
      <div class="text-2xl font-bold mt-1">{globalState.overview.active_tickets}</div>
    </div>
    <div class="border border-[var(--color-border)] p-4">
      <div class="text-[10px] tracking-widest text-[var(--color-muted)] uppercase">Open PRs</div>
      <div class="text-2xl font-bold mt-1">{globalState.overview.open_prs}</div>
    </div>
    <div class="border border-[var(--color-border)] p-4" class:border-[var(--color-warning)]={globalState.overview.need_input > 0}>
      <div class="text-[10px] tracking-widest uppercase" class:text-[var(--color-warning)]={globalState.overview.need_input > 0} class:text-[var(--color-muted)]={globalState.overview.need_input === 0}>Needs Input</div>
      <div class="text-2xl font-bold mt-1" class:text-[var(--color-warning)]={globalState.overview.need_input > 0}>{globalState.overview.need_input}</div>
    </div>
  </div>

  <!-- Project summary table -->
  <div class="border border-[var(--color-border)]">
    <div class="px-4 py-3 border-b border-[var(--color-border)]">
      <span class="text-[10px] tracking-widest text-[var(--color-muted)] uppercase">Projects</span>
    </div>
    <table class="w-full text-xs">
      <thead>
        <tr class="text-[var(--color-muted)] text-[10px] tracking-widest uppercase border-b border-[var(--color-border)]">
          <th class="text-left px-4 py-2">Project</th>
          <th class="text-right px-4 py-2">Active</th>
          <th class="text-right px-4 py-2">Input</th>
          <th class="text-right px-4 py-2">Status</th>
        </tr>
      </thead>
      <tbody>
        {#each globalState.projects as project}
          <tr class="border-b border-[var(--color-border)] hover:bg-[var(--color-surface-hover)] cursor-pointer"
              onclick={() => window.location.hash = `/projects/${project.id}/board`}>
            <td class="px-4 py-3">{project.name}</td>
            <td class="text-right px-4 py-3">{project.active ?? 0}</td>
            <td class="text-right px-4 py-3">
              {#if (project.needsInput ?? 0) > 0}
                <span class="text-[var(--color-warning)]">{project.needsInput}</span>
              {:else}
                0
              {/if}
            </td>
            <td class="text-right px-4 py-3">
              <span class={statusColor(project.status ?? 'stopped')}>
                {project.status ?? 'stopped'}
              </span>
            </td>
          </tr>
        {/each}
        {#if globalState.projects.length === 0}
          <tr>
            <td colspan="4" class="px-4 py-8 text-center text-[var(--color-muted)]">
              No projects yet. <a href="/projects/new" use:link class="text-[var(--color-accent)] hover:underline">Create one</a>
            </td>
          </tr>
        {/if}
      </tbody>
    </table>
  </div>
</div>
```

- [ ] **Step 2: Verify build**

```bash
cd internal/dashboard/web && npm run build
```
Expected: SUCCESS

- [ ] **Step 3: Commit**

```bash
git add internal/dashboard/web/src/pages/GlobalOverview.svelte
git commit -m "feat(ui): implement global overview page with summary cards and project table"
```

---

## Chunk 3: Project Board

### Task 6: Build Project Board with Kanban Columns

**Files:**
- Modify: `internal/dashboard/web/src/pages/ProjectBoard.svelte`
- Create: `internal/dashboard/web/src/components/TicketCard.svelte`
- Create: `internal/dashboard/web/src/components/ProjectTabs.svelte`

- [ ] **Step 1: Create ProjectTabs component**

Create `src/components/ProjectTabs.svelte`:

```svelte
<script lang="ts">
  import { link, location } from 'svelte-spa-router';

  interface Props {
    projectId: string;
    projectName: string;
  }
  let { projectId, projectName }: Props = $props();

  const tabs = [
    { label: 'Board', path: 'board' },
    { label: 'Dashboard', path: 'dashboard' },
    { label: 'Settings', path: 'settings' },
  ] as const;

  function isActive(path: string): boolean {
    return $location.endsWith(`/${path}`);
  }
</script>

<div class="border-b border-[var(--color-border)] px-6 flex items-center gap-6">
  <span class="text-xs font-bold tracking-widest text-[var(--color-text)] py-3">{projectName.toUpperCase()}</span>
  <div class="flex gap-1">
    {#each tabs as tab}
      <a
        href="/projects/{projectId}/{tab.path}"
        use:link
        class="px-3 py-3 text-[10px] tracking-widest uppercase border-b-2 transition-colors"
        class:border-[var(--color-accent)]={isActive(tab.path)}
        class:text-[var(--color-accent)]={isActive(tab.path)}
        class:border-transparent={!isActive(tab.path)}
        class:text-[var(--color-muted)]={!isActive(tab.path)}
        class:hover:text-[var(--color-text)]={!isActive(tab.path)}
      >
        {tab.label}
      </a>
    {/each}
  </div>
</div>
```

- [ ] **Step 2: Create TicketCard component (compact board card)**

Create `src/components/TicketCard.svelte`:

```svelte
<script lang="ts">
  import type { TicketSummary } from '../types';

  interface Props {
    ticket: TicketSummary;
    onclick: () => void;
  }
  let { ticket, onclick }: Props = $props();

  $: progress = ticket.tasks_done != null && ticket.tasks_total
    ? Math.round((ticket.tasks_done / ticket.tasks_total) * 100)
    : 0;

  $: needsInput = ticket.status === 'clarification_needed' || ticket.status === 'clarification_pending';
</script>

<button
  {onclick}
  class="w-full text-left border border-[var(--color-border)] p-3 hover:bg-[var(--color-surface-hover)] transition-colors cursor-pointer"
  class:border-l-[var(--color-warning)]={needsInput}
  class:border-l-2={needsInput}
>
  <div class="text-[10px] text-[var(--color-muted)] tracking-wider">{ticket.external_id || ticket.id.slice(0, 8)}</div>
  <div class="text-xs mt-1 leading-tight line-clamp-2">{ticket.title}</div>

  {#if ticket.tasks_total > 0}
    <div class="mt-2 flex items-center gap-2">
      <div class="flex-1 h-1 bg-[var(--color-surface)]">
        <div class="h-full bg-[var(--color-accent)]" style="width: {progress}%"></div>
      </div>
      <span class="text-[10px] text-[var(--color-muted)]">{ticket.tasks_done}/{ticket.tasks_total}</span>
    </div>
  {/if}

  <div class="mt-2 flex items-center gap-3 text-[10px] text-[var(--color-muted)]">
    {#if ticket.cost_usd > 0}
      <span>${ticket.cost_usd.toFixed(2)}</span>
    {/if}
    {#if ticket.pr_url}
      <span>PR</span>
    {/if}
    {#if needsInput}
      <span class="text-[var(--color-warning)]">needs input</span>
    {/if}
  </div>
</button>
```

- [ ] **Step 3: Implement ProjectBoard page with kanban columns**

Replace `src/pages/ProjectBoard.svelte`:

```svelte
<script lang="ts">
  import { projectState } from '../state/project.svelte';
  import { globalState } from '../state/global.svelte';
  import ProjectTabs from '../components/ProjectTabs.svelte';
  import TicketCard from '../components/TicketCard.svelte';
  import TicketPanel from '../components/TicketPanel.svelte';
  import type { TicketSummary } from '../types';

  export let params: { pid: string } = { pid: '' };

  const project = $derived(globalState.projects.find(p => p.id === params.pid));

  $effect(() => {
    if (params.pid) {
      projectState.switchProject(params.pid);
    }
  });

  const columns = [
    { label: 'Queued', statuses: ['queued', 'clarification_needed', 'clarification_pending'] },
    { label: 'Planning', statuses: ['planning', 'plan_validating', 'decomposing'] },
    { label: 'In Progress', statuses: ['implementing'] },
    { label: 'In Review', statuses: ['reviewing', 'spec_review', 'quality_review'] },
    { label: 'Awaiting Merge', statuses: ['awaiting_merge', 'pr_created'] },
    { label: 'Done', statuses: ['done', 'merged'] },
    { label: 'Failed', statuses: ['failed', 'blocked', 'partial'] },
  ] as const;

  function ticketsForColumn(statuses: readonly string[]): TicketSummary[] {
    return projectState.tickets.filter(t => statuses.includes(t.status));
  }
</script>

{#if project}
  <ProjectTabs projectId={params.pid} projectName={project.name} />
{/if}

<div class="flex h-[calc(100vh-theme(spacing.12))]">
  <!-- Board columns -->
  <div class="flex-1 overflow-x-auto">
    <div class="flex gap-0 min-w-max h-full">
      {#each columns as col}
        {@const tickets = ticketsForColumn(col.statuses)}
        <div class="w-56 border-r border-[var(--color-border)] flex flex-col">
          <div class="px-3 py-2 border-b border-[var(--color-border)] flex items-center gap-2">
            <span class="text-[10px] tracking-widest text-[var(--color-muted)] uppercase">{col.label}</span>
            {#if tickets.length > 0}
              <span class="text-[10px] text-[var(--color-muted)]">{tickets.length}</span>
            {/if}
          </div>
          <div class="flex-1 overflow-y-auto p-2 space-y-2">
            {#each tickets as ticket (ticket.id)}
              <TicketCard {ticket} onclick={() => projectState.loadTicketDetail(ticket.id)} />
            {/each}
          </div>
        </div>
      {/each}
    </div>
  </div>

  <!-- Side panel -->
  {#if projectState.selectedTicketId && !projectState.panelExpanded}
    <div class="w-[40%] min-w-96 border-l border-[var(--color-border)] overflow-y-auto">
      <TicketPanel />
    </div>
  {/if}
</div>

{#if projectState.panelExpanded}
  <!-- Full page ticket view overlays the board -->
  <!-- Implemented in Phase 3 with chat interface -->
{/if}
```

- [ ] **Step 4: Create TicketPanel (side panel detail)**

Create `src/components/TicketPanel.svelte`:

```svelte
<script lang="ts">
  import { projectState } from '../state/project.svelte';
  import TaskCard from './TaskCard.svelte';

  function statusLabel(status: string): string {
    return status.replace(/_/g, ' ').toUpperCase();
  }

  function statusColor(status: string): string {
    if (['done', 'merged'].includes(status)) return 'text-[var(--color-success)]';
    if (['failed', 'blocked'].includes(status)) return 'text-[var(--color-danger)]';
    if (status.includes('clarification')) return 'text-[var(--color-warning)]';
    return 'text-[var(--color-accent)]';
  }
</script>

{#if projectState.ticketDetail}
  {@const ticket = projectState.ticketDetail}
  <div class="p-4">
    <!-- Header -->
    <div class="flex items-center justify-between mb-4">
      <div>
        <span class="text-[10px] text-[var(--color-muted)] tracking-wider">{ticket.external_id || ticket.id.slice(0, 8)}</span>
        <span class={`text-[10px] ml-2 ${statusColor(ticket.status)}`}>{statusLabel(ticket.status)}</span>
      </div>
      <div class="flex items-center gap-2">
        <button
          onclick={() => projectState.panelExpanded = true}
          class="text-[10px] text-[var(--color-muted)] hover:text-[var(--color-text)] px-2 py-1 border border-[var(--color-border)]"
        >
          Expand ▸
        </button>
        <button
          onclick={() => projectState.deselectTicket()}
          class="text-[var(--color-muted)] hover:text-[var(--color-text)] text-sm"
        >✕</button>
      </div>
    </div>

    <h2 class="text-sm font-bold mb-3">{ticket.title}</h2>

    <!-- Progress -->
    {#if projectState.ticketTasks.length > 0}
      {@const done = projectState.ticketTasks.filter(t => t.status === 'done').length}
      {@const total = projectState.ticketTasks.length}
      <div class="flex items-center gap-2 mb-4">
        <div class="flex-1 h-1.5 bg-[var(--color-surface)]">
          <div class="h-full bg-[var(--color-accent)]" style="width: {(done/total)*100}%"></div>
        </div>
        <span class="text-[10px] text-[var(--color-muted)]">{done}/{total} tasks</span>
      </div>
    {/if}

    <!-- PR link -->
    {#if ticket.pr_url}
      <div class="mb-4 text-xs">
        <a href={ticket.pr_url} target="_blank" rel="noopener" class="text-[var(--color-accent)] hover:underline">
          PR #{ticket.pr_number} →
        </a>
      </div>
    {/if}

    <!-- Cost -->
    <div class="mb-4 text-xs text-[var(--color-muted)]">
      Cost: <span class="text-[var(--color-text)]">${ticket.cost_usd?.toFixed(2) ?? '0.00'}</span>
    </div>

    <!-- Description -->
    {#if ticket.description}
      <div class="mb-4">
        <div class="text-[10px] tracking-widest text-[var(--color-muted)] uppercase mb-1">Description</div>
        <div class="text-xs text-[var(--color-muted-bright)] whitespace-pre-wrap">{ticket.description}</div>
      </div>
    {/if}

    <!-- Tasks -->
    <div class="text-[10px] tracking-widest text-[var(--color-muted)] uppercase mb-2">Tasks</div>
    <div class="space-y-2">
      {#each projectState.ticketTasks as task (task.id)}
        <TaskCard {task} />
      {/each}
    </div>

    <!-- Actions -->
    <div class="mt-4 flex gap-2">
      {#if ['failed', 'blocked'].includes(ticket.status)}
        <button
          onclick={() => projectState.retryTicket(ticket.id)}
          class="text-[10px] px-3 py-1.5 bg-[var(--color-accent)] text-[var(--color-bg)] font-bold tracking-wider"
        >RETRY</button>
      {/if}
    </div>
  </div>
{/if}
```

- [ ] **Step 5: Verify build**

```bash
cd internal/dashboard/web && npm run build
```
Expected: SUCCESS

- [ ] **Step 6: Commit**

```bash
git add internal/dashboard/web/src/pages/ProjectBoard.svelte internal/dashboard/web/src/components/TicketCard.svelte internal/dashboard/web/src/components/ProjectTabs.svelte internal/dashboard/web/src/components/TicketPanel.svelte
git commit -m "feat(ui): implement project board with kanban columns and ticket side panel"
```

---

## Chunk 4: Project Dashboard & Settings

### Task 7: Build Project Dashboard (Metrics Page)

**Files:**
- Modify: `internal/dashboard/web/src/pages/ProjectDashboard.svelte`

- [ ] **Step 1: Implement project dashboard**

Replace `src/pages/ProjectDashboard.svelte` with a page that shows:
- Cost summary cards (daily, monthly, per-ticket average)
- 7-day cost trend (reuse CostBreakdown pattern or simple bar chart)
- Ticket throughput stats (completed vs failed)
- Model usage summary

The page reads from `projectState` (costs, tickets) and makes additional API calls for breakdowns (e.g., `GET /api/projects/:pid/usage/activity`).

Key sections:
- Summary cards at top (daily cost, monthly cost, active tickets, success rate)
- Cost trend chart (7-day bars from `projectState.weekDays`)
- Ticket status breakdown table
- LLM model usage table (from activity breakdown API)

Reuse existing `CostBreakdown.svelte` component where applicable — pass project-scoped data.

- [ ] **Step 2: Wire up ProjectTabs and route params**

Same pattern as ProjectBoard:
```svelte
export let params: { pid: string };
$effect(() => { if (params.pid) projectState.switchProject(params.pid); });
```

- [ ] **Step 3: Verify build and commit**

```bash
cd internal/dashboard/web && npm run build
git add internal/dashboard/web/src/pages/ProjectDashboard.svelte
git commit -m "feat(ui): implement project dashboard with cost and throughput metrics"
```

---

### Task 8: Build Project Settings Page

**Files:**
- Modify: `internal/dashboard/web/src/pages/ProjectSettings.svelte`

- [ ] **Step 1: Implement project settings as a form**

Replace `src/pages/ProjectSettings.svelte` with a page that:
- Fetches current project config from `GET /api/projects/:pid`
- Displays editable fields grouped by section (Project, Git, Tracker, Models, Limits, Agent Runner)
- "Test Connection" buttons for git and tracker that hit `POST /api/projects/:pid/config/test`
- "Save" button that `PUT /api/projects/:pid` with updated config
- "Delete Project" section at the bottom with confirmation dialog

Field groups:
```
[Project]     name, description
[Git]         clone_url, default_branch, provider, token — [Test Connection]
[Tracker]     provider, labels, credentials — [Test Connection]
[Models]      planner, implementer, reviewers (dropdowns)
[Limits]      max_parallel_tickets, max_tasks_per_ticket, max_cost_per_ticket
[Agent]       provider (builtin/claudecode/copilot)
[Danger]      Delete Project button
```

Each section is a collapsible card. Use the existing brutalist design system.

- [ ] **Step 2: Wire up route params and verify build**

```bash
cd internal/dashboard/web && npm run build
git add internal/dashboard/web/src/pages/ProjectSettings.svelte
git commit -m "feat(ui): implement project settings page with config editor"
```

---

### Task 9: Build Project Wizard (New Project)

**Files:**
- Modify: `internal/dashboard/web/src/pages/ProjectWizard.svelte`

- [ ] **Step 1: Implement step-by-step wizard**

Replace `src/pages/ProjectWizard.svelte` with a multi-step form:

Steps:
1. **Basics** — project name, description
2. **Repository** — git provider (GitHub/GitLab), clone URL, default branch, access token + "Test" button
3. **Tracker** — provider (GitHub/Jira/Linear/Local), credentials, project key/labels + "Test" button
4. **Configuration** — agent runner, model selection (with defaults pre-filled)
5. **Review** — summary of all fields, "Create" button

Navigation: Previous / Next buttons, step indicator at top.

On create:
```typescript
const id = await globalState.createProject(config);
window.location.hash = `/projects/${id}/board`;
```

- [ ] **Step 2: Verify build and commit**

```bash
cd internal/dashboard/web && npm run build
git add internal/dashboard/web/src/pages/ProjectWizard.svelte
git commit -m "feat(ui): implement project creation wizard"
```

---

## Chunk 5: Cleanup & Integration

### Task 10: Remove Old Single-Project Components

**Files:**
- Delete or archive: `internal/dashboard/web/src/state.svelte.ts` (old monolithic state)
- Modify: Components that imported old state to use new `state/` modules
- Remove: `Header.svelte` (replaced by Sidebar), `TicketList.svelte` (replaced by board columns), `SettingsDrawer.svelte` (replaced by settings page), `TeamSummary.svelte` (replaced by project dashboard)

- [ ] **Step 1: Delete old state.svelte.ts**

This file is superseded by `state/global.svelte.ts`, `state/project.svelte.ts`, and `state/toasts.svelte.ts`.

```bash
rm internal/dashboard/web/src/state.svelte.ts
```

- [ ] **Step 2: Update remaining components to use new state imports**

Any component that still imports from `../state.svelte` should be updated:
- `import { appState } from '../state.svelte'` → `import { projectState } from '../state/project.svelte'` or `import { globalState } from '../state/global.svelte'`

Components to check and update:
- `TaskCard.svelte` — likely uses appState for expandedTasks → use projectState
- `ActivityStream.svelte` — uses events → use projectState.ticketEvents
- `CostBreakdown.svelte` — uses ticket/llmCalls → passed as props
- `DagView.svelte` — uses tasks → passed as props
- `LiveFeed.svelte` — uses events → use projectState.events
- `SystemHealth.svelte` — uses daemon state → use globalState
- `Toasts.svelte` — uses toasts → use toasts from state/toasts.svelte

- [ ] **Step 3: Remove old components no longer needed**

```bash
rm internal/dashboard/web/src/components/Header.svelte
rm internal/dashboard/web/src/components/TicketList.svelte
rm internal/dashboard/web/src/components/SettingsDrawer.svelte
rm internal/dashboard/web/src/components/TeamSummary.svelte
```

Keep: TaskCard, DagView, ActivityStream, CostBreakdown, SystemHealth, LiveFeed, Toasts, ConfirmDialog.

- [ ] **Step 4: Verify build**

```bash
cd internal/dashboard/web && npm run build
```
Expected: SUCCESS — no broken imports

- [ ] **Step 5: Commit**

```bash
git add -A internal/dashboard/web/src/
git commit -m "refactor(ui): remove old single-project components and state management"
```

---

### Task 11: Update Vite WebSocket Proxy for Project Routes

**Files:**
- Modify: `internal/dashboard/web/vite.config.ts`

- [ ] **Step 1: Update proxy config for project-scoped WebSocket**

In `vite.config.ts`, ensure the proxy handles `/ws/global` and `/ws/projects/`:

```typescript
server: {
  proxy: {
    '/api': `http://127.0.0.1:${port}`,
    '/ws/global': { target: `ws://127.0.0.1:${port}`, ws: true },
    '/ws/projects': { target: `ws://127.0.0.1:${port}`, ws: true },
    '/ws/events': { target: `ws://127.0.0.1:${port}`, ws: true }, // keep for backward compat
  }
}
```

- [ ] **Step 2: Verify dev server proxy works**

```bash
cd internal/dashboard/web && npm run dev
```
Expected: Vite starts, proxies work

- [ ] **Step 3: Commit**

```bash
git add internal/dashboard/web/vite.config.ts
git commit -m "fix(ui): update Vite proxy for project-scoped WebSocket routes"
```

---

### Task 12: Final Verification

- [ ] **Step 1: Full frontend build**

```bash
cd internal/dashboard/web && npm run build
```
Expected: SUCCESS — no errors

- [ ] **Step 2: Type check**

```bash
cd internal/dashboard/web && npm run lint
```
Expected: No critical type errors

- [ ] **Step 3: Full backend build (includes embedded frontend)**

```bash
go build ./...
```
Expected: SUCCESS

- [ ] **Step 4: Manual smoke test**

Start the daemon, open the dashboard:
1. Login screen appears → enter token → redirected to global overview
2. Sidebar shows project list
3. Click a project → navigates to board
4. Board shows ticket columns
5. Click a ticket → side panel opens with detail
6. Navigate to Dashboard tab → metrics page
7. Navigate to Settings tab → config editor
8. Click "+ Add Project" → wizard appears
9. Overview page shows aggregated metrics

- [ ] **Step 5: Commit any remaining fixes**

```bash
git add -A internal/dashboard/web/
git commit -m "fix(ui): address issues from final verification"
```
