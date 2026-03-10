# Frontend Production Refactor — Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix all bugs, typos, dead code, and incomplete components in the Phase 2 Svelte frontend to make it production-ready.

**Architecture:** Surgical fixes for bugs/typos in Chunk 0, type-system improvements in Chunk 1, full rewrites of TicketPanel + TicketFullView in Chunk 2, board expansion wiring in Chunk 3, and dashboard/overview enhancements in Chunk 4. All navigation uses `push()` from `svelte-spa-router`. No backward-compat constraints.

**Tech Stack:** Svelte 5, TypeScript, Vite 6, Tailwind CSS 4, svelte-spa-router

**Spec:** `docs/superpowers/specs/2026-03-10-frontend-refactor-production-ready.md`

**Verification command:** `cd internal/dashboard/web && npm run test`
Expected output: `svelte-check` with 0 errors, then `vite build` SUCCESS.

---

## File Structure

```
src/
├── types.ts                    # Add ProjectConfig interface
├── routes.ts                   # Remove unused import, add NotFound route
├── state/
│   └── global.svelte.ts        # Wire WS onmessage to refresh overview+projects
├── pages/
│   ├── GlobalOverview.svelte   # Fix active_tickets, fix nav, loading state
│   ├── ProjectBoard.svelte     # Wire panelExpanded to TicketFullView
│   ├── ProjectDashboard.svelte # Chart labels + tooltip
│   ├── ProjectSettings.svelte  # Fix typos, fix nav, use ProjectConfig type
│   ├── ProjectWizard.svelte    # Fix nav (2x window.location.hash → push)
│   └── NotFound.svelte         # New: 404 page
└── components/
    ├── TaskCard.svelte          # Fix retryTicket(task.ID) → retryTicket(task.TicketID)
    ├── TicketPanel.svelte       # Full rewrite: tabbed tasks/events/chat
    └── TicketFullView.svelte    # New: full-page overlay shell
```

---

## Chunk 0: Bug Fixes, Typos, Dead Code

### Task 0: Fix routes.ts — remove unused import, add NotFound

**Files:**
- Modify: `internal/dashboard/web/src/routes.ts`
- Create: `internal/dashboard/web/src/pages/NotFound.svelte`

- [ ] **Step 1: Create NotFound.svelte**

Create `src/pages/NotFound.svelte`:

```svelte
<script lang="ts">
  import { push } from 'svelte-spa-router';
</script>

<div class="flex flex-col items-center justify-center h-full p-12 text-center">
  <div class="text-[var(--color-muted)] text-[10px] tracking-[0.4em] uppercase mb-2">404</div>
  <div class="text-sm font-bold text-[var(--color-text)] mb-6">Page not found</div>
  <button
    onclick={() => push('/')}
    class="text-[10px] tracking-widest text-[var(--color-accent)] hover:underline"
  >
    ← Back to overview
  </button>
</div>
```

- [ ] **Step 2: Update routes.ts**

Replace the entire file:

```typescript
import GlobalOverview from './pages/GlobalOverview.svelte';
import ProjectBoard from './pages/ProjectBoard.svelte';
import ProjectDashboard from './pages/ProjectDashboard.svelte';
import ProjectSettings from './pages/ProjectSettings.svelte';
import ProjectWizard from './pages/ProjectWizard.svelte';
import NotFound from './pages/NotFound.svelte';

export default {
  '/': GlobalOverview,
  '/projects/new': ProjectWizard,
  '/projects/:pid/board': ProjectBoard,
  '/projects/:pid/dashboard': ProjectDashboard,
  '/projects/:pid/settings': ProjectSettings,
  '*': NotFound,
};
```

- [ ] **Step 3: Verify build**

```bash
cd internal/dashboard/web && npm run test
```
Expected: 0 errors, build SUCCESS.

- [ ] **Step 4: Commit**

```bash
cd internal/dashboard/web && git add src/routes.ts src/pages/NotFound.svelte
git commit -m "fix(ui): remove unused wrap import, add 404 NotFound route"
```

---

### Task 1: Fix raw `window.location.hash` navigation (3 files)

**Files:**
- Modify: `internal/dashboard/web/src/pages/GlobalOverview.svelte`
- Modify: `internal/dashboard/web/src/pages/ProjectSettings.svelte`
- Modify: `internal/dashboard/web/src/pages/ProjectWizard.svelte`

- [ ] **Step 1: Fix GlobalOverview.svelte**

In `src/pages/GlobalOverview.svelte`, change the import line and the onclick handler.

Add `push` to the imports at the top of the `<script>` block:
```svelte
<script lang="ts">
  import { globalState } from '../state/global.svelte';
  import { link, push } from 'svelte-spa-router';
```

Find and replace:
```
onclick={() => window.location.hash = `/projects/${project.id}/board`}
```
With:
```
onclick={() => push(`/projects/${project.id}/board`)}
```

- [ ] **Step 2: Fix ProjectSettings.svelte**

In `src/pages/ProjectSettings.svelte`, add `push` to imports:
```svelte
<script lang="ts">
  import { projectState } from '../state/project.svelte';
  import { globalState } from '../state/global.svelte';
  import { fetchJSON, getToken } from '../api';
  import { toasts } from '../state/toasts.svelte';
  import { push } from 'svelte-spa-router';
  import ProjectTabs from '../components/ProjectTabs.svelte';
  import ConfirmDialog from '../components/ConfirmDialog.svelte';
```

Find and replace in `deleteProject()`:
```
  async function deleteProject() {
    await globalState.deleteProject(params.pid);
    window.location.hash = '/';
  }
```
With:
```
  async function deleteProject() {
    await globalState.deleteProject(params.pid);
    push('/');
  }
```

- [ ] **Step 3: Fix ProjectWizard.svelte**

In `src/pages/ProjectWizard.svelte`, add `push` to imports:
```svelte
<script lang="ts">
  import { globalState } from '../state/global.svelte';
  import { getToken } from '../api';
  import { toasts } from '../state/toasts.svelte';
  import { push } from 'svelte-spa-router';
```

Find and replace in `createProject()`:
```
      window.location.hash = `/projects/${id}/board`;
```
With:
```
      push(`/projects/${id}/board`);
```

Find and replace the Cancel button onclick:
```
          onclick={() => { window.location.hash = '/'; }}
```
With:
```
          onclick={() => push('/')}
```

- [ ] **Step 4: Verify build**

```bash
cd internal/dashboard/web && npm run test
```
Expected: 0 errors, build SUCCESS.

- [ ] **Step 5: Commit**

```bash
cd internal/dashboard/web && git add src/pages/GlobalOverview.svelte src/pages/ProjectSettings.svelte src/pages/ProjectWizard.svelte
git commit -m "fix(ui): replace window.location.hash navigation with push() router calls"
```

---

### Task 2: Fix typos (`tracking-widests`) in 2 files

**Files:**
- Modify: `internal/dashboard/web/src/pages/ProjectSettings.svelte`
- Modify: `internal/dashboard/web/src/pages/GlobalOverview.svelte`

- [ ] **Step 1: Fix ProjectSettings.svelte typos**

In `src/pages/ProjectSettings.svelte`, there are two occurrences of `tracking-widests`. Find and replace both:

Line ~194:
```
      <span class="text-[10px] tracking-widests text-[var(--color-muted)] uppercase">Limits</span>
```
→
```
      <span class="text-[10px] tracking-widest text-[var(--color-muted)] uppercase">Limits</span>
```

Line ~200:
```
          <span class="text-[10px] tracking-widests text-[var(--color-muted)] uppercase">Max Parallel Tickets</span>
```
→
```
          <span class="text-[10px] tracking-widest text-[var(--color-muted)] uppercase">Max Parallel Tickets</span>
```

- [ ] **Step 2: Fix GlobalOverview.svelte typo**

In `src/pages/GlobalOverview.svelte`, find:
```
      <div class="text-[10px] tracking-widests text-[var(--color-muted)] uppercase">Open PRs</div>
```
Replace with:
```
      <div class="text-[10px] tracking-widest text-[var(--color-muted)] uppercase">Open PRs</div>
```

- [ ] **Step 3: Verify build**

```bash
cd internal/dashboard/web && npm run test
```
Expected: 0 errors, build SUCCESS.

- [ ] **Step 4: Commit**

```bash
cd internal/dashboard/web && git add src/pages/ProjectSettings.svelte src/pages/GlobalOverview.svelte
git commit -m "fix(ui): fix tracking-widests typo in ProjectSettings and GlobalOverview"
```

---

### Task 3: Fix `TaskCard.svelte` — wrong ID passed to `retryTicket`

**Files:**
- Modify: `internal/dashboard/web/src/components/TaskCard.svelte`

The bug: `projectState.retryTicket(task.ID)` passes the Task's ID, but `retryTicket` expects a Ticket ID. The correct field is `task.TicketID`.

- [ ] **Step 1: Fix the ConfirmDialog onconfirm handler**

In `src/components/TaskCard.svelte`, find the ConfirmDialog at the bottom:

```svelte
<ConfirmDialog
  open={confirmOpen}
  title="RETRY TASK"
  message="Re-run this task through the pipeline again?"
  confirmLabel="↺ RETRY"
  confirmClass="bg-warning text-bg hover:bg-text"
  onconfirm={() => { projectState.retryTicket(task.ID); confirmOpen = false; }}
  oncancel={() => { confirmOpen = false; }}
/>
```

Replace `task.ID` with `task.TicketID`:

```svelte
<ConfirmDialog
  open={confirmOpen}
  title="RETRY TASK"
  message="Re-run this task through the pipeline again?"
  confirmLabel="↺ RETRY"
  confirmClass="bg-warning text-bg hover:bg-text"
  onconfirm={() => { projectState.retryTicket(task.TicketID); confirmOpen = false; }}
  oncancel={() => { confirmOpen = false; }}
/>
```

- [ ] **Step 2: Verify build**

```bash
cd internal/dashboard/web && npm run test
```
Expected: 0 errors, build SUCCESS.

- [ ] **Step 3: Commit**

```bash
cd internal/dashboard/web && git add src/components/TaskCard.svelte
git commit -m "fix(ui): pass task.TicketID (not task.ID) to retryTicket"
```

---

## Chunk 1: Type Safety + Global State WS Wiring

### Task 4: Add `ProjectConfig` type to `types.ts`

**Files:**
- Modify: `internal/dashboard/web/src/types.ts`

- [ ] **Step 1: Add ProjectConfig interface**

Open `src/types.ts` and add after the `ChatMessage` interface at the end of the file:

```typescript
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
  agent_runner: string;
  model_planner: string;
  model_implementer: string;
  max_parallel_tickets: number;
  max_tasks_per_ticket: number;
  max_cost_per_ticket: number;
}
```

- [ ] **Step 2: Update ProjectSettings.svelte — fix type and legacy $props syntax**

In `src/pages/ProjectSettings.svelte`, replace the entire `<script>` opening block.

Current opening (first ~12 lines of script):
```svelte
<script lang="ts">
  import { projectState } from '../state/project.svelte';
  import { globalState } from '../state/global.svelte';
  import { fetchJSON, getToken } from '../api';
  import { toasts } from '../state/toasts.svelte';
  import ProjectTabs from '../components/ProjectTabs.svelte';
  import ConfirmDialog from '../components/ConfirmDialog.svelte';

  let { params } = $props<{ params: { pid: string } }>();
```

Replace with:
```svelte
<script lang="ts">
  import { projectState } from '../state/project.svelte';
  import { globalState } from '../state/global.svelte';
  import { fetchJSON, getToken } from '../api';
  import { toasts } from '../state/toasts.svelte';
  import { push } from 'svelte-spa-router';
  import type { ProjectConfig } from '../types';
  import ProjectTabs from '../components/ProjectTabs.svelte';
  import ConfirmDialog from '../components/ConfirmDialog.svelte';

  let { params }: { params: { pid: string } } = $props();
```

Also change:
```typescript
  let config = $state<Record<string, any>>({});
```
To:
```typescript
  let config = $state<Partial<ProjectConfig>>({});
```

- [ ] **Step 3: Verify build**

```bash
cd internal/dashboard/web && npm run test
```
Expected: 0 errors, build SUCCESS.

- [ ] **Step 4: Commit**

```bash
cd internal/dashboard/web && git add src/types.ts src/pages/ProjectSettings.svelte
git commit -m "feat(ui): add ProjectConfig type, remove any from ProjectSettings"
```

---

### Task 5: Wire global WebSocket to refresh overview and projects

**Files:**
- Modify: `internal/dashboard/web/src/state/global.svelte.ts`

Currently the global WS `onmessage` only fires toasts for warning/error events. It never refreshes project status or overview metrics, so the overview goes stale until the 15s/30s poll interval.

- [ ] **Step 1: Update connectGlobalWebSocket in global.svelte.ts**

Replace the entire `connectGlobalWebSocket` method:

```typescript
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
        if (event.severity === 'warning' || event.severity === 'error') {
          toasts.add(event.message, event.severity, event.ticket_id);
        }
        // Refresh project list and overview on any status-changing events
        if (['ticket_status_changed', 'project_status_changed', 'task_done', 'task_failed'].includes(event.event_type)) {
          this.loadProjects();
          this.loadOverview();
        }
      } catch {}
    };
  }
```

- [ ] **Step 2: Verify build**

```bash
cd internal/dashboard/web && npm run test
```
Expected: 0 errors, build SUCCESS.

- [ ] **Step 3: Commit**

```bash
cd internal/dashboard/web && git add src/state/global.svelte.ts
git commit -m "fix(ui): wire global WebSocket to refresh overview and projects on events"
```

---

## Chunk 2: TicketPanel Full Rewrite

### Task 6: Rewrite TicketPanel with tabbed layout (Tasks / Events / Chat)

**Files:**
- Modify: `internal/dashboard/web/src/components/TicketPanel.svelte`

The current panel is bare: no events tab, no chat tab, no comments. Full rewrite. Tabs: Tasks (default), Events, Chat.

- [ ] **Step 1: Replace TicketPanel.svelte entirely**

Replace the entire content of `src/components/TicketPanel.svelte`:

```svelte
<script lang="ts">
  import { projectState } from '../state/project.svelte';
  import { formatRelative, formatCost, severityIcon, linkifyParts } from '../format';
  import { PR_STATUSES } from '../types';
  import TaskCard from './TaskCard.svelte';
  import ConfirmDialog from './ConfirmDialog.svelte';

  type Tab = 'tasks' | 'events' | 'chat';
  let activeTab = $state<Tab>('tasks');
  let showDeleteConfirm = $state(false);
  let chatInput = $state('');
  let sendingChat = $state(false);

  const ticket = $derived(projectState.ticketDetail);
  const hasPR = $derived(ticket ? PR_STATUSES.includes(ticket.Status) : false);

  function statusLabel(status: string): string {
    return status.replace(/_/g, ' ').toUpperCase();
  }

  function statusColor(status: string): string {
    if (['done', 'merged'].includes(status)) return 'text-[var(--color-success)]';
    if (['failed', 'blocked', 'partial'].includes(status)) return 'text-[var(--color-danger)]';
    if (status.includes('clarification')) return 'text-[var(--color-warning)]';
    return 'text-[var(--color-accent)]';
  }

  function severityColor(sev: string): string {
    if (sev === 'success') return 'text-[var(--color-success)]';
    if (sev === 'error') return 'text-[var(--color-danger)]';
    if (sev === 'warning') return 'text-[var(--color-warning)]';
    return 'text-[var(--color-muted-bright)]';
  }

  async function sendChat() {
    if (!ticket || !chatInput.trim()) return;
    sendingChat = true;
    try {
      await projectState.sendChatMessage(ticket.ID, chatInput.trim());
      chatInput = '';
    } finally {
      sendingChat = false;
    }
  }

  async function handleDelete() {
    if (!ticket) return;
    await projectState.deleteTicket(ticket.ID);
    showDeleteConfirm = false;
  }
</script>

{#if ticket}
  <div class="flex flex-col h-full animate-[slide-in-right_0.2s_ease-out]">
    <!-- Header -->
    <div class="px-4 pt-4 pb-3 border-b border-[var(--color-border)] shrink-0">
      <div class="flex items-start justify-between gap-2 mb-2">
        <div class="min-w-0">
          <div class="flex items-center gap-2 flex-wrap">
            <span class="text-[10px] text-[var(--color-muted)] tracking-wider font-mono">
              {ticket.ExternalID || ticket.ID.slice(0, 8)}
            </span>
            <span class="text-[10px] font-bold tracking-wider {statusColor(ticket.Status)}">
              {statusLabel(ticket.Status)}
            </span>
          </div>
          <h2 class="text-xs font-bold mt-1 leading-snug">{ticket.Title}</h2>
        </div>
        <div class="flex items-center gap-1 shrink-0">
          <button
            onclick={() => projectState.expandPanel()}
            class="text-[10px] text-[var(--color-muted)] hover:text-[var(--color-text)] px-2 py-1 border border-[var(--color-border)] hover:border-[var(--color-border-strong)] transition-colors"
            title="Expand full view"
          >⤢</button>
          <button
            onclick={() => projectState.deselectTicket()}
            class="text-[var(--color-muted)] hover:text-[var(--color-text)] px-2 py-1 border border-[var(--color-border)] hover:border-[var(--color-border-strong)] transition-colors text-sm leading-none"
          >✕</button>
        </div>
      </div>

      <!-- Progress bar -->
      {#if projectState.ticketTasks.length > 0}
        {@const done = projectState.ticketTasks.filter(t => t.Status === 'done').length}
        {@const total = projectState.ticketTasks.length}
        <div class="flex items-center gap-2 mt-2">
          <div class="flex-1 h-1 bg-[var(--color-surface)]">
            <div class="h-full bg-[var(--color-accent)] transition-all duration-300"
                 style="width: {total > 0 ? (done / total) * 100 : 0}%"></div>
          </div>
          <span class="text-[10px] text-[var(--color-muted)] shrink-0">{done}/{total}</span>
        </div>
      {/if}

      <!-- Meta row -->
      <div class="flex items-center gap-3 mt-2 text-[10px] text-[var(--color-muted)] flex-wrap">
        <span>Cost: <span class="text-[var(--color-text)]">{formatCost(ticket.CostUSD ?? 0)}</span></span>
        {#if hasPR && ticket.PRURL}
          <a href={ticket.PRURL} target="_blank" rel="noopener"
             class="text-[var(--color-accent)] hover:underline">
            PR #{ticket.PRNumber} →
          </a>
        {/if}
        {#if ticket.ChannelSenderID}
          <span class="truncate max-w-[120px]" title={ticket.ChannelSenderID}>
            by {ticket.ChannelSenderID.split('@')[0]}
          </span>
        {/if}
      </div>
    </div>

    <!-- Tabs -->
    <div class="flex border-b border-[var(--color-border)] shrink-0">
      {#each (['tasks', 'events', 'chat'] as Tab[]) as tab}
        {@const count = tab === 'tasks' ? projectState.ticketTasks.length
                      : tab === 'events' ? projectState.ticketEvents.length
                      : projectState.chatMessages.length}
        <button
          onclick={() => activeTab = tab}
          class="px-4 py-2 text-[10px] tracking-widest uppercase border-b-2 transition-colors"
          class:border-[var(--color-accent)]={activeTab === tab}
          class:text-[var(--color-accent)]={activeTab === tab}
          class:border-transparent={activeTab !== tab}
          class:text-[var(--color-muted)]={activeTab !== tab}
          class:hover:text-[var(--color-muted-bright)]={activeTab !== tab}
        >
          {tab}{count > 0 ? ` · ${count}` : ''}
        </button>
      {/each}
    </div>

    <!-- Tab content -->
    <div class="flex-1 overflow-y-auto">

      <!-- Tasks tab -->
      {#if activeTab === 'tasks'}
        <div class="p-3 space-y-2">
          {#if ticket.Description}
            <div class="mb-3 p-3 bg-[var(--color-surface)] border border-[var(--color-border)]">
              <div class="text-[10px] tracking-widest text-[var(--color-muted)] uppercase mb-1">Description</div>
              <div class="text-xs text-[var(--color-muted-bright)] whitespace-pre-wrap leading-relaxed">
                {ticket.Description}
              </div>
            </div>
          {/if}
          {#if ticket.ErrorMessage}
            <div class="mb-3 border-l-4 border-l-[var(--color-danger)] bg-[var(--color-danger-bg)] p-3">
              <div class="text-[var(--color-danger)] font-bold text-[10px] tracking-wider mb-1">ERROR</div>
              <div class="text-xs text-[var(--color-text)]/80">{ticket.ErrorMessage}</div>
            </div>
          {/if}
          {#each projectState.ticketTasks as task (task.ID)}
            <TaskCard
              {task}
              events={projectState.ticketEvents}
              llmCalls={projectState.ticketLlmCalls}
            />
          {/each}
          {#if projectState.ticketTasks.length === 0}
            <div class="text-center text-[var(--color-muted)] text-xs py-8">No tasks yet</div>
          {/if}
        </div>

      <!-- Events tab -->
      {:else if activeTab === 'events'}
        <div class="divide-y divide-[var(--color-border)]">
          {#each projectState.ticketEvents as evt (evt.ID)}
            <div class="px-4 py-2.5 flex gap-3 items-start hover:bg-[var(--color-surface-hover)]">
              <span class="shrink-0 text-xs {severityColor(evt.Severity)} mt-0.5">
                {severityIcon(evt.Severity)}
              </span>
              <div class="min-w-0 flex-1">
                <div class="text-xs text-[var(--color-text)] leading-snug">
                  {#each linkifyParts(evt.Message || evt.EventType) as part}
                    {#if part.type === 'url'}
                      <a href={part.content} target="_blank" rel="noopener"
                         class="text-[var(--color-accent)] hover:underline break-all">{part.content}</a>
                    {:else}
                      {part.content}
                    {/if}
                  {/each}
                </div>
                {#if evt.Details}
                  <div class="text-[10px] text-[var(--color-muted)] mt-0.5 truncate" title={evt.Details}>
                    {evt.Details}
                  </div>
                {/if}
              </div>
              <span class="shrink-0 text-[10px] text-[var(--color-muted)] whitespace-nowrap">
                {formatRelative(evt.CreatedAt)}
              </span>
            </div>
          {/each}
          {#if projectState.ticketEvents.length === 0}
            <div class="text-center text-[var(--color-muted)] text-xs py-8">No events yet</div>
          {/if}
        </div>

      <!-- Chat tab -->
      {:else if activeTab === 'chat'}
        <div class="flex flex-col h-full">
          <div class="flex-1 overflow-y-auto divide-y divide-[var(--color-border)]">
            {#each projectState.chatMessages as msg (msg.id)}
              <div class="px-4 py-3"
                   class:bg-[var(--color-accent-bg)]={msg.sender === 'user'}>
                <div class="flex items-center gap-2 mb-1">
                  <span class="text-[10px] font-bold tracking-wider uppercase"
                        class:text-[var(--color-accent)]={msg.sender === 'user'}
                        class:text-[var(--color-muted-bright)]={msg.sender !== 'user'}>
                    {msg.sender}
                  </span>
                  <span class="text-[10px] text-[var(--color-muted)]">
                    {formatRelative(msg.created_at)}
                  </span>
                </div>
                <div class="text-xs text-[var(--color-text)] whitespace-pre-wrap leading-relaxed">
                  {msg.content}
                </div>
              </div>
            {/each}
            {#if projectState.chatMessages.length === 0}
              <div class="text-center text-[var(--color-muted)] text-xs py-8">No messages</div>
            {/if}
          </div>

          <!-- Chat input — only shown when ticket needs clarification -->
          {#if ticket.Status === 'clarification_needed'}
            <div class="border-t border-[var(--color-border)] p-3 shrink-0">
              <div class="text-[10px] text-[var(--color-warning)] tracking-wider uppercase mb-2">
                Agent is waiting for your input
              </div>
              <textarea
                bind:value={chatInput}
                rows="3"
                placeholder="Type your reply..."
                class="w-full bg-[var(--color-surface)] border border-[var(--color-border)] px-3 py-2 text-xs
                       text-[var(--color-text)] focus:border-[var(--color-accent)] focus:outline-none resize-none"
                onkeydown={(e) => { if (e.key === 'Enter' && (e.ctrlKey || e.metaKey)) sendChat(); }}
              ></textarea>
              <div class="flex justify-between items-center mt-2">
                <span class="text-[10px] text-[var(--color-muted)]">Ctrl+Enter to send</span>
                <button
                  onclick={sendChat}
                  disabled={sendingChat || !chatInput.trim()}
                  class="px-4 py-1.5 bg-[var(--color-accent)] text-[var(--color-bg)] text-[10px] font-bold
                         tracking-widest disabled:opacity-40 hover:opacity-90 transition-opacity"
                >
                  {sendingChat ? 'SENDING...' : 'SEND'}
                </button>
              </div>
            </div>
          {/if}
        </div>
      {/if}
    </div>

    <!-- Actions footer -->
    <div class="border-t border-[var(--color-border)] px-4 py-3 flex gap-2 shrink-0">
      {#if ['failed', 'blocked', 'partial'].includes(ticket.Status)}
        <button
          onclick={() => projectState.retryTicket(ticket.ID)}
          class="text-[10px] px-3 py-1.5 bg-[var(--color-accent)] text-[var(--color-bg)] font-bold tracking-wider hover:opacity-90"
        >↺ RETRY</button>
      {/if}
      <button
        onclick={() => showDeleteConfirm = true}
        class="text-[10px] px-3 py-1.5 border border-[var(--color-danger)] text-[var(--color-danger)] hover:bg-[var(--color-danger-bg)] tracking-wider ml-auto"
      >DELETE</button>
    </div>
  </div>

  <ConfirmDialog
    open={showDeleteConfirm}
    title="Delete Ticket"
    message="Permanently delete this ticket and all its data?"
    confirmLabel="DELETE"
    onconfirm={handleDelete}
    oncancel={() => showDeleteConfirm = false}
  />
{/if}
```

- [ ] **Step 2: Verify build**

```bash
cd internal/dashboard/web && npm run test
```
Expected: 0 errors, build SUCCESS.

- [ ] **Step 3: Commit**

```bash
cd internal/dashboard/web && git add src/components/TicketPanel.svelte
git commit -m "feat(ui): rewrite TicketPanel with tabbed tasks/events/chat layout"
```

---

## Chunk 3: TicketFullView + Board Expansion

### Task 7: Create TicketFullView component

**Files:**
- Create: `internal/dashboard/web/src/components/TicketFullView.svelte`

Full-page overlay that renders when `panelExpanded === true`. Shows all ticket detail in a wide layout. Phase 3 chat will be added later — this is the shell.

- [ ] **Step 1: Create TicketFullView.svelte**

Create `src/components/TicketFullView.svelte`:

```svelte
<script lang="ts">
  import { projectState } from '../state/project.svelte';
  import { formatRelative, formatCost, severityIcon, linkifyParts } from '../format';
  import { PR_STATUSES } from '../types';
  import TaskCard from './TaskCard.svelte';

  const ticket = $derived(projectState.ticketDetail);
  const hasPR = $derived(ticket ? PR_STATUSES.includes(ticket.Status) : false);

  function statusLabel(status: string): string {
    return status.replace(/_/g, ' ').toUpperCase();
  }

  function statusColor(status: string): string {
    if (['done', 'merged'].includes(status)) return 'text-[var(--color-success)]';
    if (['failed', 'blocked', 'partial'].includes(status)) return 'text-[var(--color-danger)]';
    if (status.includes('clarification')) return 'text-[var(--color-warning)]';
    return 'text-[var(--color-accent)]';
  }

  function severityColor(sev: string): string {
    if (sev === 'success') return 'text-[var(--color-success)]';
    if (sev === 'error') return 'text-[var(--color-danger)]';
    if (sev === 'warning') return 'text-[var(--color-warning)]';
    return 'text-[var(--color-muted-bright)]';
  }
</script>

{#if ticket}
  <div class="absolute inset-0 bg-[var(--color-bg)] z-20 flex flex-col animate-[zoom-in_0.15s_ease-out] overflow-hidden">
    <!-- Top bar -->
    <div class="h-12 border-b border-[var(--color-border)] px-6 flex items-center gap-4 shrink-0">
      <button
        onclick={() => projectState.collapsePanel()}
        class="text-[10px] text-[var(--color-muted)] hover:text-[var(--color-text)] tracking-wider flex items-center gap-1"
      >
        ← BACK TO BOARD
      </button>
      <div class="h-4 w-px bg-[var(--color-border)]"></div>
      <span class="text-[10px] font-mono text-[var(--color-muted)]">
        {ticket.ExternalID || ticket.ID.slice(0, 8)}
      </span>
      <span class="text-[10px] font-bold tracking-wider {statusColor(ticket.Status)}">
        {statusLabel(ticket.Status)}
      </span>
      <div class="ml-auto flex items-center gap-3 text-[10px] text-[var(--color-muted)]">
        <span>Cost: <span class="text-[var(--color-text)]">{formatCost(ticket.CostUSD ?? 0)}</span></span>
        {#if hasPR && ticket.PRURL}
          <a href={ticket.PRURL} target="_blank" rel="noopener"
             class="text-[var(--color-accent)] hover:underline">PR #{ticket.PRNumber} →</a>
        {/if}
      </div>
    </div>

    <!-- Content: two columns -->
    <div class="flex flex-1 overflow-hidden">
      <!-- Left: ticket info + tasks -->
      <div class="flex-1 overflow-y-auto p-6 border-r border-[var(--color-border)]">
        <h1 class="text-sm font-bold mb-4 leading-snug">{ticket.Title}</h1>

        <!-- Progress -->
        {#if projectState.ticketTasks.length > 0}
          {@const done = projectState.ticketTasks.filter(t => t.Status === 'done').length}
          {@const total = projectState.ticketTasks.length}
          <div class="flex items-center gap-2 mb-6">
            <div class="flex-1 h-1.5 bg-[var(--color-surface)]">
              <div class="h-full bg-[var(--color-accent)] transition-all duration-300"
                   style="width: {total > 0 ? (done / total) * 100 : 0}%"></div>
            </div>
            <span class="text-[10px] text-[var(--color-muted)] shrink-0">{done}/{total} tasks</span>
          </div>
        {/if}

        {#if ticket.Description}
          <div class="mb-6 p-4 bg-[var(--color-surface)] border border-[var(--color-border)]">
            <div class="text-[10px] tracking-widest text-[var(--color-muted)] uppercase mb-2">Description</div>
            <div class="text-xs text-[var(--color-muted-bright)] whitespace-pre-wrap leading-relaxed">
              {ticket.Description}
            </div>
          </div>
        {/if}

        {#if ticket.ErrorMessage}
          <div class="mb-6 border-l-4 border-l-[var(--color-danger)] bg-[var(--color-danger-bg)] p-4">
            <div class="text-[var(--color-danger)] font-bold text-[10px] tracking-wider mb-1">ERROR</div>
            <div class="text-xs text-[var(--color-text)]/80">{ticket.ErrorMessage}</div>
          </div>
        {/if}

        <div class="text-[10px] tracking-widest text-[var(--color-muted)] uppercase mb-3">Tasks</div>
        <div class="space-y-2">
          {#each projectState.ticketTasks as task (task.ID)}
            <TaskCard {task} events={projectState.ticketEvents} llmCalls={projectState.ticketLlmCalls} />
          {/each}
          {#if projectState.ticketTasks.length === 0}
            <div class="text-center text-[var(--color-muted)] text-xs py-8">No tasks yet</div>
          {/if}
        </div>
      </div>

      <!-- Right: events log -->
      <div class="w-80 shrink-0 overflow-y-auto">
        <div class="px-4 py-3 border-b border-[var(--color-border)]">
          <span class="text-[10px] tracking-widest text-[var(--color-muted)] uppercase">Events</span>
        </div>
        <div class="divide-y divide-[var(--color-border)]">
          {#each projectState.ticketEvents as evt (evt.ID)}
            <div class="px-4 py-2.5 flex gap-2 items-start hover:bg-[var(--color-surface-hover)]">
              <span class="shrink-0 text-xs {severityColor(evt.Severity)} mt-0.5">
                {severityIcon(evt.Severity)}
              </span>
              <div class="min-w-0 flex-1">
                <div class="text-xs text-[var(--color-text)] leading-snug">
                  {#each linkifyParts(evt.Message || evt.EventType) as part}
                    {#if part.type === 'url'}
                      <a href={part.content} target="_blank" rel="noopener"
                         class="text-[var(--color-accent)] hover:underline break-all">{part.content}</a>
                    {:else}
                      {part.content}
                    {/if}
                  {/each}
                </div>
                <div class="text-[10px] text-[var(--color-muted)] mt-0.5">
                  {formatRelative(evt.CreatedAt)}
                </div>
              </div>
            </div>
          {/each}
          {#if projectState.ticketEvents.length === 0}
            <div class="text-center text-[var(--color-muted)] text-xs py-8">No events</div>
          {/if}
        </div>
      </div>
    </div>
  </div>
{/if}
```

- [ ] **Step 2: Add `collapsePanel()` method to ProjectState**

In `src/state/project.svelte.ts`, the `expandPanel()` method exists but there is no `collapsePanel()`. Add it after `expandPanel()`:

```typescript
  collapsePanel() {
    this.panelExpanded = false;
  }
```

- [ ] **Step 3: Verify build**

```bash
cd internal/dashboard/web && npm run test
```
Expected: 0 errors, build SUCCESS.

- [ ] **Step 4: Commit**

```bash
cd internal/dashboard/web && git add src/components/TicketFullView.svelte src/state/project.svelte.ts
git commit -m "feat(ui): add TicketFullView overlay shell and collapsePanel method"
```

---

### Task 8: Wire ProjectBoard to render TicketFullView overlay

**Files:**
- Modify: `internal/dashboard/web/src/pages/ProjectBoard.svelte`

Currently `panelExpanded` is set but the board has an empty comment stub. Wire it to render `TicketFullView`.

- [ ] **Step 1: Update ProjectBoard.svelte**

Replace the entire file:

```svelte
<script lang="ts">
  import { projectState } from '../state/project.svelte';
  import { globalState } from '../state/global.svelte';
  import ProjectTabs from '../components/ProjectTabs.svelte';
  import TicketCard from '../components/TicketCard.svelte';
  import TicketPanel from '../components/TicketPanel.svelte';
  import TicketFullView from '../components/TicketFullView.svelte';
  import type { TicketSummary } from '../types';

  let { params }: { params: { pid: string } } = $props();

  const project = $derived(globalState.projects.find((p: { id: string; name: string }) => p.id === params.pid));

  $effect(() => {
    if (params.pid) {
      projectState.switchProject(params.pid);
    }
  });

  const columns = [
    { label: 'Queued', statuses: ['queued', 'clarification_needed', 'decomposed'] },
    { label: 'Planning', statuses: ['planning', 'plan_validating', 'decomposing'] },
    { label: 'In Progress', statuses: ['implementing'] },
    { label: 'In Review', statuses: ['reviewing'] },
    { label: 'Awaiting Merge', statuses: ['pr_created', 'pr_updated', 'awaiting_merge'] },
    { label: 'Done', statuses: ['done', 'merged'] },
    { label: 'Failed', statuses: ['failed', 'blocked', 'partial'] },
  ] as const;

  function ticketsForColumn(statuses: readonly string[]): TicketSummary[] {
    return projectState.tickets.filter(t => statuses.includes(t.Status));
  }
</script>

{#if project}
  <ProjectTabs projectId={params.pid} projectName={project.name} />
{/if}

<div class={`flex relative ${project ? 'h-[calc(100vh-3rem)]' : 'h-screen'}`}>
  <!-- Board columns -->
  <div class="flex-1 overflow-x-auto" class:opacity-0={projectState.panelExpanded}>
    <div class="flex min-w-max h-full">
      {#each columns as col}
        {@const tickets = ticketsForColumn(col.statuses)}
        <div class="w-60 border-r border-[var(--color-border)] flex flex-col">
          <div class="px-3 py-2 border-b border-[var(--color-border)] flex items-center gap-2">
            <span class="text-[10px] tracking-widest text-[var(--color-muted)] uppercase">{col.label}</span>
            {#if tickets.length > 0}
              <span class="text-[10px] text-[var(--color-bg)] bg-[var(--color-muted)] px-1.5 min-w-[1.25rem] text-center">
                {tickets.length}
              </span>
            {/if}
          </div>
          <div class="flex-1 overflow-y-auto p-2 space-y-2 board-column">
            {#each tickets as ticket, i (ticket.ID)}
              <div style="animation-delay: {i * 30}ms" class="animate-fade-in opacity-0 [animation-fill-mode:forwards]">
                <TicketCard {ticket} onclick={() => projectState.loadTicketDetail(ticket.ID)} />
              </div>
            {/each}
            {#if tickets.length === 0}
              <div class="text-center text-[var(--color-muted)] text-[10px] py-4">—</div>
            {/if}
          </div>
        </div>
      {/each}
    </div>
  </div>

  <!-- Side panel (collapsed when full view is open) -->
  {#if projectState.selectedTicketId && !projectState.panelExpanded}
    <div class="w-[38%] min-w-96 border-l border-[var(--color-border)] overflow-y-auto shrink-0">
      <TicketPanel />
    </div>
  {/if}

  <!-- Full page overlay -->
  {#if projectState.panelExpanded && projectState.selectedTicketId}
    <TicketFullView />
  {/if}
</div>
```

- [ ] **Step 2: Verify build**

```bash
cd internal/dashboard/web && npm run test
```
Expected: 0 errors, build SUCCESS.

- [ ] **Step 3: Commit**

```bash
cd internal/dashboard/web && git add src/pages/ProjectBoard.svelte
git commit -m "feat(ui): wire ProjectBoard to render TicketFullView overlay on panel expand"
```

---

## Chunk 4: GlobalOverview + ProjectDashboard Enhancements

### Task 9: Fix GlobalOverview — active ticket count + WS-connected indicator

**Files:**
- Modify: `internal/dashboard/web/src/pages/GlobalOverview.svelte`

The `Active` column currently shows `project.active ? 1 : 0` which is wrong. The `ProjectEntry` type does not carry active ticket counts — those live in `ProjectSummary`. The overview data already has `active_tickets` at the global level; for per-project counts the current API doesn't expose them through the `/api/projects` listing endpoint. The fix: remove the broken `Active` column and replace with `Status` (which is useful), or use the data that is actually available. Looking at `ProjectEntry`, we have `needsInput`, `status`, `active`. Replace `Active` with `Tickets needing input`, and keep `Status`.

Also add a WS connection indicator in the header.

- [ ] **Step 1: Replace GlobalOverview.svelte**

Replace the entire file:

```svelte
<script lang="ts">
  import { globalState } from '../state/global.svelte';
  import { link, push } from 'svelte-spa-router';

  // True on first load before any data has arrived
  const loading = $derived(
    globalState.projects.length === 0 &&
    globalState.overview.active_tickets === 0 &&
    globalState.overview.cost_today === 0
  );

  function statusColor(status: string): string {
    switch (status) {
      case 'running': return 'text-[var(--color-success)]';
      case 'error': return 'text-[var(--color-danger)]';
      case 'paused': return 'text-[var(--color-warning)]';
      default: return 'text-[var(--color-muted)]';
    }
  }

  function statusDot(status: string): string {
    switch (status) {
      case 'running': return '●';
      case 'paused': return '⏸';
      case 'error': return '⚠';
      default: return '○';
    }
  }
</script>

<div class="p-6 max-w-6xl">
  <!-- Header row -->
  <div class="flex items-center justify-between mb-6">
    <h1 class="text-sm font-bold tracking-[0.3em] text-[var(--color-accent)]">OVERVIEW</h1>
    <div class="flex items-center gap-1.5 text-[10px]"
         class:text-[var(--color-success)]={globalState.wsConnected}
         class:text-[var(--color-muted)]={!globalState.wsConnected}>
      <span class="text-[8px]" class:animate-pulse={globalState.wsConnected}>●</span>
      <span class="tracking-wider">{globalState.wsConnected ? 'LIVE' : 'POLLING'}</span>
    </div>
  </div>

  {#if loading}
    <!-- Skeleton loading state -->
    <div class="grid grid-cols-2 lg:grid-cols-4 gap-4 mb-8">
      {#each [0, 1, 2, 3] as i}
        <div class="border border-[var(--color-border)] p-4 relative animate-pulse-slow" style="animation-delay: {i * 50}ms">
          <div class="absolute top-0 left-0 right-0 h-0.5 bg-[var(--color-border-strong)]"></div>
          <div class="h-2 w-16 bg-[var(--color-surface-hover)] mb-3 rounded-none"></div>
          <div class="h-7 w-12 bg-[var(--color-surface-hover)] rounded-none"></div>
        </div>
      {/each}
    </div>
    <div class="border border-[var(--color-border)]">
      <div class="px-4 py-3 border-b border-[var(--color-border)]">
        <div class="h-2 w-16 bg-[var(--color-surface-hover)] animate-pulse-slow"></div>
      </div>
      {#each [0, 1, 2] as i}
        <div class="border-b border-[var(--color-border)] px-4 py-3 flex justify-between"
             style="animation-delay: {i * 50}ms">
          <div class="h-2 w-24 bg-[var(--color-surface-hover)] animate-pulse-slow"></div>
          <div class="h-2 w-12 bg-[var(--color-surface-hover)] animate-pulse-slow"></div>
        </div>
      {/each}
    </div>
  {:else}

  <!-- Summary cards -->
  <div class="grid grid-cols-2 lg:grid-cols-4 gap-4 mb-8">
    <div class="border border-[var(--color-border)] p-4 relative animate-fade-in">
      <div class="absolute top-0 left-0 right-0 h-0.5 bg-[var(--color-accent)]"></div>
      <div class="text-[10px] tracking-widest text-[var(--color-muted)] uppercase">Cost Today</div>
      <div class="text-2xl font-bold mt-1 text-[var(--color-accent)]">${globalState.overview.cost_today.toFixed(2)}</div>
    </div>
    <div class="border border-[var(--color-border)] p-4 relative animate-fade-in" style="animation-delay: 50ms">
      <div class="absolute top-0 left-0 right-0 h-0.5 bg-[var(--color-border-strong)]"></div>
      <div class="text-[10px] tracking-widest text-[var(--color-muted)] uppercase">Active Tickets</div>
      <div class="text-2xl font-bold mt-1">{globalState.overview.active_tickets}</div>
    </div>
    <div class="border border-[var(--color-border)] p-4 relative animate-fade-in" style="animation-delay: 100ms">
      <div class="absolute top-0 left-0 right-0 h-0.5 bg-[var(--color-border-strong)]"></div>
      <div class="text-[10px] tracking-widest text-[var(--color-muted)] uppercase">Open PRs</div>
      <div class="text-2xl font-bold mt-1">{globalState.overview.open_prs}</div>
    </div>
    <div class="border p-4 relative animate-fade-in transition-colors" style="animation-delay: 150ms"
         class:border-[var(--color-warning)]={globalState.overview.need_input > 0}
         class:border-[var(--color-border)]={globalState.overview.need_input === 0}
         class:shadow-[0_0_20px_rgba(255,170,32,0.08)]={globalState.overview.need_input > 0}>
      {#if globalState.overview.need_input > 0}
        <div class="absolute top-0 left-0 right-0 h-0.5 bg-[var(--color-warning)] animate-pulse-slow"></div>
      {:else}
        <div class="absolute top-0 left-0 right-0 h-0.5 bg-[var(--color-border-strong)]"></div>
      {/if}
      <div class="text-[10px] tracking-widest uppercase"
           class:text-[var(--color-warning)]={globalState.overview.need_input > 0}
           class:text-[var(--color-muted)]={globalState.overview.need_input === 0}>Needs Input</div>
      <div class="text-2xl font-bold mt-1"
           class:text-[var(--color-warning)]={globalState.overview.need_input > 0}>
        {globalState.overview.need_input}
      </div>
    </div>
  </div>

  <!-- Project summary table -->
  <div class="border border-[var(--color-border)]">
    <div class="px-4 py-3 border-b border-[var(--color-border)] flex items-center justify-between">
      <span class="text-[10px] tracking-widest text-[var(--color-muted)] uppercase">Projects</span>
      <a href="/projects/new" use:link
         class="text-[10px] tracking-widest text-[var(--color-accent)] hover:underline">+ New</a>
    </div>
    <table class="w-full text-xs">
      <thead>
        <tr class="text-[var(--color-muted)] text-[10px] tracking-widest uppercase border-b border-[var(--color-border)]">
          <th class="text-left px-4 py-2">Project</th>
          <th class="text-right px-4 py-2">Needs Input</th>
          <th class="text-right px-4 py-2">Status</th>
        </tr>
      </thead>
      <tbody>
        {#each globalState.projects as project}
          <tr class="border-b border-[var(--color-border)] hover:bg-[var(--color-surface-hover)] cursor-pointer"
              onclick={() => push(`/projects/${project.id}/board`)}>
            <td class="px-4 py-3 font-bold">{project.name}</td>
            <td class="text-right px-4 py-3">
              {#if (project.needsInput ?? 0) > 0}
                <span class="text-[var(--color-warning)] font-bold">{project.needsInput}</span>
              {:else}
                <span class="text-[var(--color-muted)]">—</span>
              {/if}
            </td>
            <td class="text-right px-4 py-3">
              <span class="flex items-center justify-end gap-1.5 {statusColor(project.status ?? 'stopped')}">
                <span class="text-[10px]">{statusDot(project.status ?? 'stopped')}</span>
                <span>{project.status ?? 'stopped'}</span>
              </span>
            </td>
          </tr>
        {/each}
        {#if globalState.projects.length === 0}
          <tr>
            <td colspan="3" class="px-4 py-8 text-center text-[var(--color-muted)]">
              No projects yet.
              <a href="/projects/new" use:link class="text-[var(--color-accent)] hover:underline ml-1">Create one →</a>
            </td>
          </tr>
        {/if}
      </tbody>
    </table>
  </div>

  {/if}
</div>
```

- [ ] **Step 2: Verify build**

```bash
cd internal/dashboard/web && npm run test
```
Expected: 0 errors, build SUCCESS.

- [ ] **Step 3: Commit**

```bash
cd internal/dashboard/web && git add src/pages/GlobalOverview.svelte
git commit -m "feat(ui): fix GlobalOverview — skeleton loader, WS indicator, fix nav, remove broken active count"
```

---

### Task 10: Improve ProjectDashboard cost chart

**Files:**
- Modify: `internal/dashboard/web/src/pages/ProjectDashboard.svelte`

Current chart has no labels, no value display, and bars with `height: 0%` are invisible. Add: day labels already exist (ok), cost value on hover via title attribute, minimum bar height, and `$0.00` label on bars with cost.

- [ ] **Step 1: Update the chart section in ProjectDashboard.svelte**

Find the `<!-- 7-day cost trend -->` section and replace it:

```svelte
  <!-- 7-day cost trend -->
  <div class="border border-[var(--color-border)] p-4 mb-8">
    <div class="text-[10px] tracking-widest text-[var(--color-muted)] uppercase mb-4">7-Day Cost Trend</div>
    {#if projectState.weekDays.length === 0}
      <div class="flex items-center justify-center h-32 text-xs text-[var(--color-muted)]">No cost data yet</div>
    {:else}
      <div class="flex items-end gap-1 h-36">
        {#each projectState.weekDays as day, i}
          {@const pct = Math.max((day.cost_usd / maxDayCost) * 100, day.cost_usd > 0 ? 4 : 1)}
          <div class="flex-1 flex flex-col items-center gap-1 group" title="${day.cost_usd.toFixed(4)}">
            <div class="w-full flex flex-col justify-end" style="height: 112px">
              <div class="w-full bg-[var(--color-accent)] opacity-80 group-hover:opacity-100 transition-opacity
                          animate-fade-in relative"
                   style="height: {pct}%; animation-delay: {i * 60}ms; min-height: 2px">
                {#if day.cost_usd > 0}
                  <div class="absolute -top-4 left-0 right-0 text-center text-[9px] text-[var(--color-muted)]
                              opacity-0 group-hover:opacity-100 transition-opacity whitespace-nowrap">
                    ${day.cost_usd.toFixed(2)}
                  </div>
                {/if}
              </div>
            </div>
            <span class="text-[9px] text-[var(--color-muted)]">
              {new Date(day.date + 'T12:00:00').toLocaleDateString('en', { weekday: 'short' })}
            </span>
          </div>
        {/each}
      </div>
    {/if}
  </div>
```

Note: The `day.date + 'T12:00:00'` fix prevents timezone-off-by-one where `new Date('2026-03-10')` is parsed as UTC midnight and `.toLocaleDateString` shows the previous day in negative-offset timezones.

- [ ] **Step 2: Verify build**

```bash
cd internal/dashboard/web && npm run test
```
Expected: 0 errors, build SUCCESS.

- [ ] **Step 3: Commit**

```bash
cd internal/dashboard/web && git add src/pages/ProjectDashboard.svelte
git commit -m "fix(ui): improve dashboard chart — hover cost labels, min bar height, timezone fix"
```

---

## Chunk 5: Final Cleanup

### Task 11: Remove dead `wsConnected` unused field warning — surface it in Sidebar

**Files:**
- Modify: `internal/dashboard/web/src/components/Sidebar.svelte`

`globalState.wsConnected` is tracked but never displayed. Already surfaced in GlobalOverview in Task 9. No further changes needed in Sidebar — the field is now used. Mark complete.

- [ ] **Step 1: Final verification — run full build**

```bash
cd internal/dashboard/web && npm run test
```
Expected: 0 svelte-check errors, vite build SUCCESS.

- [ ] **Step 2: Final commit**

```bash
cd internal/dashboard/web && git add -p
git commit -m "chore(ui): frontend production refactor complete"
```

---

## Verification Checklist

After all tasks complete, verify manually:

- [ ] `/` — Overview loads, WS indicator shows LIVE/POLLING, clicking a project row navigates to board
- [ ] `/projects/new` — Wizard works, Cancel button navigates home, Create navigates to board
- [ ] `/projects/:pid/board` — Kanban board loads, clicking a ticket opens TicketPanel
- [ ] TicketPanel — Tasks/Events/Chat tabs work, Expand button opens TicketFullView overlay
- [ ] TicketFullView — Back button returns to board, tasks + events render
- [ ] `/projects/:pid/settings` — Save works, Delete navigates to `/` via router
- [ ] `*` — Unknown URL shows NotFound page
- [ ] No `window.location.hash` references remain in src/

```bash
grep -r "window.location.hash" internal/dashboard/web/src/
```
Expected: no output.
