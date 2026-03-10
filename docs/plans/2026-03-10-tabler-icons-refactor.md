# Tabler Icons Refactor Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Replace all Unicode character icons across the Svelte dashboard with proper SVG icons from `@tabler/icons-svelte`, improving visual consistency, accessibility, and semantic clarity.

**Architecture:** Direct component imports — each `.svelte` file imports only the Tabler icon components it needs. No shared barrel file or wrapper component. The two helper functions `severityIcon()` and `taskIcon()` in `format.ts` are removed; call sites are refactored to render Tabler components conditionally.

**Tech Stack:** Svelte 5, TypeScript, `@tabler/icons-svelte`, npm, Vite 6

---

## Icon Mapping Reference

| Unicode char | Context | Tabler component |
|---|---|---|
| `✓` `\u2713` | success / done / check | `IconCheck` |
| `✗` `\u2717` | error / failed / close-small | `IconX` |
| `⚠` `\u26A0` | warning | `IconAlertTriangle` |
| `●` `\u25CF` | running / live / info dot | `IconCircleFilled` |
| `○` `\u25CB` | pending / default | `IconCircle` |
| `⚙` `\u2699` | in-progress (implementing etc.) | `IconSettings2` |
| `⊘` `\u2298` | skipped | `IconCircleOff` |
| `◼` | paused | `IconPlayerPause` |
| `▲` (status) | error status indicator | `IconAlertTriangle` |
| `◈` | nav overview diamond | `IconLayoutDashboard` |
| `◁` | sidebar collapse left | `IconChevronLeft` |
| `▷` | sidebar expand right | `IconChevronRight` |
| `✕` | logout / close panel | `IconX` |
| `↺` | retry | `IconRefresh` |
| `►` | live activity indicator | `IconPlayerPlay` |
| `▼` / `▲` (chevron) | expand / collapse accordion | `IconChevronDown` / `IconChevronUp` |
| `↗` | PR external link | `IconExternalLink` |
| `✓` (wizard) | completed step | `IconCircleCheck` |
| `▶` (resume) | resume project | `IconPlayerPlay` |
| `↻` (sync) | sync tracker | `IconRefresh` |

**Default props for all icons:** `size={16} stroke={1.5}`

For animated pulse cases (running status, live indicator), keep the parent `<span>` with `animate-pulse` class and place the icon inside it.

---

## Task 1: Install @tabler/icons-svelte

**Files:**
- Modify: `internal/dashboard/web/package.json`

**Step 1: Install the package**

```bash
cd internal/dashboard/web && npm install @tabler/icons-svelte
```

**Step 2: Verify installation**

```bash
ls internal/dashboard/web/node_modules/@tabler/icons-svelte/dist/
```
Expected: directory exists with icon files.

**Step 3: Commit**

```bash
git add internal/dashboard/web/package.json internal/dashboard/web/package-lock.json
git commit -m "chore: install @tabler/icons-svelte"
```

---

## Task 2: Remove severityIcon() and taskIcon() helpers from format.ts

**Files:**
- Modify: `internal/dashboard/web/src/format.ts` (lines 56–63 and 81–89)

**Step 1: Delete severityIcon() function (lines 56–63)**

Remove the entire block:
```typescript
export function severityIcon(severity: string): string {
  switch (severity) {
    case 'success': return '\u2713';
    case 'error': return '\u2717';
    case 'warning': return '\u26A0';
    default: return '\u25CF';
  }
}
```

**Step 2: Delete taskIcon() function (lines 81–89)**

Remove the entire block:
```typescript
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

**Step 3: Verify the file still compiles**

```bash
cd internal/dashboard/web && npx tsc --noEmit
```
Expected: TypeScript errors only for files that still import the removed functions (Toasts, ActivityStream, TaskCard, TicketFullView — these will be fixed in subsequent tasks).

**Step 4: Commit**

```bash
git add internal/dashboard/web/src/format.ts
git commit -m "refactor: remove severityIcon and taskIcon string helpers from format.ts"
```

---

## Task 3: Refactor Toasts.svelte

**Files:**
- Modify: `internal/dashboard/web/src/components/Toasts.svelte`

**Step 1: Replace the script block**

Old `<script>`:
```svelte
<script lang="ts">
  import { toasts } from '../state/toasts.svelte';
  import { projectState } from '../state/project.svelte';
  import { severityIcon } from '../format';
</script>
```

New `<script>`:
```svelte
<script lang="ts">
  import { toasts } from '../state/toasts.svelte';
  import { projectState } from '../state/project.svelte';
  import { IconCheck, IconX, IconAlertTriangle, IconCircleFilled } from '@tabler/icons-svelte';
</script>
```

**Step 2: Replace the icon span inside the icon block (line 14–16)**

Old:
```svelte
          <span class="text-sm {toast.severity === 'error' ? 'text-[var(--color-danger)]' : toast.severity === 'warning' ? 'text-[var(--color-warning)]' : toast.severity === 'info' ? 'text-[var(--color-accent)]' : 'text-[var(--color-success)]'}">
            {severityIcon(toast.severity)}
          </span>
```

New:
```svelte
          <span class="{toast.severity === 'error' ? 'text-[var(--color-danger)]' : toast.severity === 'warning' ? 'text-[var(--color-warning)]' : toast.severity === 'info' ? 'text-[var(--color-accent)]' : 'text-[var(--color-success)]'}">
            {#if toast.severity === 'success'}<IconCheck size={16} stroke={1.5} />
            {:else if toast.severity === 'error'}<IconX size={16} stroke={1.5} />
            {:else if toast.severity === 'warning'}<IconAlertTriangle size={16} stroke={1.5} />
            {:else}<IconCircleFilled size={16} stroke={1.5} />{/if}
          </span>
```

**Step 3: Verify**

```bash
cd internal/dashboard/web && npx tsc --noEmit
```
Expected: no errors for Toasts.svelte.

**Step 4: Commit**

```bash
git add internal/dashboard/web/src/components/Toasts.svelte
git commit -m "refactor: replace Unicode icons with Tabler in Toasts.svelte"
```

---

## Task 4: Refactor ActivityStream.svelte

**Files:**
- Modify: `internal/dashboard/web/src/components/ActivityStream.svelte`

**Step 1: Replace import in script block**

Old import line:
```typescript
  import { severityIcon, formatRelative, taskIcon, linkifyParts } from '../format';
```

New import lines:
```typescript
  import { formatRelative, linkifyParts } from '../format';
  import { IconCheck, IconX, IconAlertTriangle, IconCircleFilled } from '@tabler/icons-svelte';
```

**Step 2: Replace the icon span (lines 73–75)**

Old:
```svelte
        <span class="text-xs shrink-0 mt-0.5 w-3 text-center {severityTextCls(evt.Severity)}">
          {severityIcon(evt.Severity)}
        </span>
```

New:
```svelte
        <span class="shrink-0 mt-0.5 {severityTextCls(evt.Severity)}">
          {#if evt.Severity === 'success'}<IconCheck size={14} stroke={1.5} />
          {:else if evt.Severity === 'error'}<IconX size={14} stroke={1.5} />
          {:else if evt.Severity === 'warning'}<IconAlertTriangle size={14} stroke={1.5} />
          {:else}<IconCircleFilled size={14} stroke={1.5} />{/if}
        </span>
```

**Step 3: Verify**

```bash
cd internal/dashboard/web && npx tsc --noEmit
```

**Step 4: Commit**

```bash
git add internal/dashboard/web/src/components/ActivityStream.svelte
git commit -m "refactor: replace Unicode icons with Tabler in ActivityStream.svelte"
```

---

## Task 5: Refactor TaskCard.svelte

**Files:**
- Modify: `internal/dashboard/web/src/components/TaskCard.svelte`

**Step 1: Replace import in script block**

Old:
```typescript
  import { taskIcon, formatCost, formatRelative, runnerBadgeCls, shortModel } from '../format';
```

New:
```typescript
  import { formatCost, formatRelative, runnerBadgeCls, shortModel } from '../format';
  import {
    IconCheck, IconX, IconSettings2, IconCircleOff, IconCircle,
    IconRefresh, IconPlayerPlay, IconChevronUp, IconChevronDown
  } from '@tabler/icons-svelte';
```

**Step 2: Replace the task status icon span (lines 77–84)**

Old:
```svelte
    <!-- Icon -->
    <span class="text-xs shrink-0 w-4 text-center leading-none"
          class:text-[var(--color-accent)]={isActive}
          class:animate-pulse={isActive}
          class:text-[var(--color-success)]={isDone}
          class:text-[var(--color-danger)]={isFailed}
          class:text-[var(--color-muted-bright)]={!isActive && !isDone && !isFailed}>
      {taskIcon(task.Status)}
    </span>
```

New:
```svelte
    <!-- Icon -->
    <span class="shrink-0 w-4 flex items-center justify-center"
          class:text-[var(--color-accent)]={isActive}
          class:animate-pulse={isActive}
          class:text-[var(--color-success)]={isDone}
          class:text-[var(--color-danger)]={isFailed}
          class:text-[var(--color-muted-bright)]={!isActive && !isDone && !isFailed}>
      {#if isDone}<IconCheck size={14} stroke={1.5} />
      {:else if isFailed}<IconX size={14} stroke={1.5} />
      {:else if isActive}<IconSettings2 size={14} stroke={1.5} />
      {:else if task.Status === 'skipped'}<IconCircleOff size={14} stroke={1.5} />
      {:else}<IconCircle size={14} stroke={1.5} />{/if}
    </span>
```

**Step 3: Replace the retry button icon (line 106)**

Old:
```svelte
      >↺</button>
```

New:
```svelte
      ><IconRefresh size={14} stroke={1.5} /></button>
```

**Step 4: Replace the live activity indicator icon (line 116)**

Old:
```svelte
      <span class="text-[var(--color-accent)] text-xs animate-pulse leading-none">►</span>
```

New:
```svelte
      <span class="text-[var(--color-accent)] animate-pulse flex items-center"><IconPlayerPlay size={14} stroke={1.5} /></span>
```

**Step 5: Replace the expand chevron (line 110)**

Old:
```svelte
    <span class="text-[var(--color-muted)] text-[10px] shrink-0 leading-none">{expanded ? '▲' : '▼'}</span>
```

New:
```svelte
    <span class="text-[var(--color-muted)] shrink-0 flex items-center">
      {#if expanded}<IconChevronUp size={14} stroke={1.5} />{:else}<IconChevronDown size={14} stroke={1.5} />{/if}
    </span>
```

**Step 6: Replace the acceptance criteria check/circle (line 191)**

Old:
```svelte
                  {isDone ? '✓' : '○'}
```

New:
```svelte
                  {#if isDone}<IconCheck size={12} stroke={1.5} />{:else}<IconCircle size={12} stroke={1.5} />{/if}
```

Also update the containing `<span>` to use `flex items-center justify-center` instead of relying on text sizing:

Old (line 188–191):
```svelte
                <span class="shrink-0 text-[10px] leading-none mt-0.5"
                      class:text-[var(--color-success)]={isDone}
                      class:text-[var(--color-muted)]={!isDone}>
                  {isDone ? '✓' : '○'}
                </span>
```

New:
```svelte
                <span class="shrink-0 flex items-center mt-0.5"
                      class:text-[var(--color-success)]={isDone}
                      class:text-[var(--color-muted)]={!isDone}>
                  {#if isDone}<IconCheck size={12} stroke={1.5} />{:else}<IconCircle size={12} stroke={1.5} />{/if}
                </span>
```

**Step 7: Update ConfirmDialog confirmLabel prop (line 222)**

Old:
```svelte
  confirmLabel="↺ Retry"
```

New:
```svelte
  confirmLabel="Retry"
```

**Step 8: Verify**

```bash
cd internal/dashboard/web && npx tsc --noEmit
```

**Step 9: Commit**

```bash
git add internal/dashboard/web/src/components/TaskCard.svelte
git commit -m "refactor: replace Unicode icons with Tabler in TaskCard.svelte"
```

---

## Task 6: Refactor TicketCard.svelte

**Files:**
- Modify: `internal/dashboard/web/src/components/TicketCard.svelte`

**Step 1: Add imports to script block**

Add after the existing imports:
```typescript
  import { IconCircleFilled, IconExternalLink } from '@tabler/icons-svelte';
```

**Step 2: Replace the active status pulse dot (line 60)**

Old:
```svelte
        <span class="animate-pulse-slow">●</span>
```

New:
```svelte
        <span class="animate-pulse-slow flex items-center"><IconCircleFilled size={10} stroke={1.5} /></span>
```

**Step 3: Replace the PR arrow (line 90)**

Old:
```svelte
        <span class="text-[var(--color-accent)]">↗ PR open</span>
```

New:
```svelte
        <span class="text-[var(--color-accent)] flex items-center gap-1"><IconExternalLink size={12} stroke={1.5} /> PR open</span>
```

**Step 4: Verify**

```bash
cd internal/dashboard/web && npx tsc --noEmit
```

**Step 5: Commit**

```bash
git add internal/dashboard/web/src/components/TicketCard.svelte
git commit -m "refactor: replace Unicode icons with Tabler in TicketCard.svelte"
```

---

## Task 7: Refactor TicketFullView.svelte

**Files:**
- Modify: `internal/dashboard/web/src/components/TicketFullView.svelte`

**Step 1: Replace imports in script block**

Old:
```typescript
  import { formatRelative, formatCost, severityIcon, linkifyParts } from '../format';
```

New:
```typescript
  import { formatRelative, formatCost, linkifyParts } from '../format';
  import {
    IconCheck, IconX, IconAlertTriangle, IconCircleFilled,
    IconChevronUp, IconChevronDown, IconExternalLink
  } from '@tabler/icons-svelte';
```

**Step 2: Replace PR arrow in top bar (line 59)**

Old:
```svelte
          <a href={ticket.PRURL} target="_blank" rel="noopener"
             class="status-chip status-chip-active hover:underline">↗ PR #{ticket.PRNumber}</a>
```

New:
```svelte
          <a href={ticket.PRURL} target="_blank" rel="noopener"
             class="status-chip status-chip-active hover:underline flex items-center gap-1">
            <IconExternalLink size={12} stroke={1.5} /> PR #{ticket.PRNumber}
          </a>
```

**Step 3: Replace events accordion chevron (line 123)**

Old:
```svelte
            <span class="text-[10px] text-[var(--color-muted)]">{eventsExpanded ? '▲' : '▼'}</span>
```

New:
```svelte
            <span class="text-[var(--color-muted)] flex items-center">
              {#if eventsExpanded}<IconChevronUp size={14} stroke={1.5} />{:else}<IconChevronDown size={14} stroke={1.5} />{/if}
            </span>
```

**Step 4: Replace severity icon in events list (line 130)**

Old:
```svelte
                  <span class="shrink-0 text-xs {severityColor(evt.Severity)} mt-0.5">
                    {severityIcon(evt.Severity)}
                  </span>
```

New:
```svelte
                  <span class="shrink-0 flex items-center mt-0.5 {severityColor(evt.Severity)}">
                    {#if evt.Severity === 'success'}<IconCheck size={14} stroke={1.5} />
                    {:else if evt.Severity === 'error'}<IconX size={14} stroke={1.5} />
                    {:else if evt.Severity === 'warning'}<IconAlertTriangle size={14} stroke={1.5} />
                    {:else}<IconCircleFilled size={14} stroke={1.5} />{/if}
                  </span>
```

**Step 5: Verify**

```bash
cd internal/dashboard/web && npx tsc --noEmit
```

**Step 6: Commit**

```bash
git add internal/dashboard/web/src/components/TicketFullView.svelte
git commit -m "refactor: replace Unicode icons with Tabler in TicketFullView.svelte"
```

---

## Task 8: Refactor TicketPanel.svelte

**Files:**
- Modify: `internal/dashboard/web/src/components/TicketPanel.svelte`

**Step 1: Add imports to script block**

Add after the existing imports:
```typescript
  import { IconX, IconRefresh, IconExternalLink } from '@tabler/icons-svelte';
```

**Step 2: Replace the expand-to-full-view button icon (line 92)**

Old:
```svelte
          >⤢</button>
```

New:
```svelte
          ><IconExternalLink size={14} stroke={1.5} /></button>
```

**Step 3: Replace the close panel button icon (line 98)**

Old:
```svelte
          >✕</button>
```

New:
```svelte
          ><IconX size={16} stroke={1.5} /></button>
```

**Step 4: Replace the retry button text (line 286)**

Old:
```svelte
        >{retrying ? '…' : '↺ Retry'}</button>
```

New:
```svelte
        >{#if retrying}…{:else}<span class="flex items-center gap-1"><IconRefresh size={14} stroke={1.5} /> Retry</span>{/if}</button>
```

**Step 5: Replace PR arrow (line 128)**

Old:
```svelte
          <a href={ticket.PRURL} target="_blank" rel="noopener"
             class="text-[var(--color-accent)] hover:opacity-80 transition-opacity uppercase tracking-wider">
            PR #{ticket.PRNumber} ↗
          </a>
```

New:
```svelte
          <a href={ticket.PRURL} target="_blank" rel="noopener"
             class="text-[var(--color-accent)] hover:opacity-80 transition-opacity uppercase tracking-wider flex items-center gap-1">
            PR #{ticket.PRNumber} <IconExternalLink size={12} stroke={1.5} />
          </a>
```

**Step 6: Verify**

```bash
cd internal/dashboard/web && npx tsc --noEmit
```

**Step 7: Commit**

```bash
git add internal/dashboard/web/src/components/TicketPanel.svelte
git commit -m "refactor: replace Unicode icons with Tabler in TicketPanel.svelte"
```

---

## Task 9: Refactor Sidebar.svelte

**Files:**
- Modify: `internal/dashboard/web/src/components/Sidebar.svelte`

**Step 1: Add imports to script block**

Add after the existing imports:
```typescript
  import {
    IconCircleFilled, IconPlayerPause, IconAlertTriangle, IconCircle,
    IconLayoutDashboard, IconChevronLeft, IconChevronRight, IconX
  } from '@tabler/icons-svelte';
```

**Step 2: Remove the statusIndicator() function (lines 13–20) entirely**

The function `statusIndicator()` returns Unicode strings. It will be replaced inline.

**Step 3: Replace status dot span in project entries (lines 112–115)**

Old:
```svelte
        <!-- Status dot -->
        <span class="text-[10px] shrink-0 leading-none {statusColor(project.status ?? 'stopped')}"
              class:animate-pulse={project.status === 'running'}>
          {statusIndicator(project.status ?? 'stopped')}
        </span>
```

New:
```svelte
        <!-- Status dot -->
        <span class="shrink-0 flex items-center {statusColor(project.status ?? 'stopped')}"
              class:animate-pulse={project.status === 'running'}>
          {#if project.status === 'running'}<IconCircleFilled size={10} stroke={1.5} />
          {:else if project.status === 'paused'}<IconPlayerPause size={10} stroke={1.5} />
          {:else if project.status === 'error'}<IconAlertTriangle size={10} stroke={1.5} />
          {:else}<IconCircle size={10} stroke={1.5} />{/if}
        </span>
```

**Step 4: Replace the Overview nav icon (lines 76 and 79)**

Old (expanded):
```svelte
        <span class="text-[10px] shrink-0 {overviewActive ? 'text-[var(--color-accent)]' : 'text-[var(--color-muted)]'}">◈</span>
```

New:
```svelte
        <span class="shrink-0 flex items-center {overviewActive ? 'text-[var(--color-accent)]' : 'text-[var(--color-muted)]'}"><IconLayoutDashboard size={14} stroke={1.5} /></span>
```

Old (collapsed):
```svelte
        <span class="text-[11px] mx-auto {overviewActive ? 'text-[var(--color-accent)]' : 'text-[var(--color-muted-bright)]'}">◈</span>
```

New:
```svelte
        <span class="mx-auto flex items-center {overviewActive ? 'text-[var(--color-accent)]' : 'text-[var(--color-muted-bright)]'}"><IconLayoutDashboard size={14} stroke={1.5} /></span>
```

**Step 5: Replace collapse/expand button icons (lines 149 and 152)**

Old (expanded, line 149):
```svelte
        <span class="text-[10px]">◁</span>
```

New:
```svelte
        <span class="flex items-center"><IconChevronLeft size={14} stroke={1.5} /></span>
```

Old (collapsed, line 152):
```svelte
        <span class="text-[10px] mx-auto">▷</span>
```

New:
```svelte
        <span class="mx-auto flex items-center"><IconChevronRight size={14} stroke={1.5} /></span>
```

**Step 6: Replace logout icon (lines 160 and 163)**

Old (expanded, line 160):
```svelte
        <span class="text-[10px]">✕</span>
```

New:
```svelte
        <span class="flex items-center"><IconX size={14} stroke={1.5} /></span>
```

Old (collapsed, line 163):
```svelte
        <span class="text-[10px] mx-auto">✕</span>
```

New:
```svelte
        <span class="mx-auto flex items-center"><IconX size={14} stroke={1.5} /></span>
```

**Step 7: Verify**

```bash
cd internal/dashboard/web && npx tsc --noEmit
```

**Step 8: Commit**

```bash
git add internal/dashboard/web/src/components/Sidebar.svelte
git commit -m "refactor: replace Unicode icons with Tabler in Sidebar.svelte"
```

---

## Task 10: Refactor ProjectTabs.svelte

**Files:**
- Modify: `internal/dashboard/web/src/components/ProjectTabs.svelte`

**Step 1: Add imports to script block**

Add after the existing imports:
```typescript
  import { IconRefresh, IconPlayerPlay, IconPlayerPause } from '@tabler/icons-svelte';
```

**Step 2: Replace sync button icon (line 87)**

Old:
```svelte
      {syncing ? '↻' : '↻'} {syncing ? 'Syncing…' : 'Sync'}
```

New:
```svelte
      <span class="flex items-center gap-1"><IconRefresh size={14} stroke={1.5} /> {syncing ? 'Syncing…' : 'Sync'}</span>
```

**Step 3: Replace Resume button icon (line 99)**

Old:
```svelte
        ▶ Resume
```

New:
```svelte
        <span class="flex items-center gap-1"><IconPlayerPlay size={14} stroke={1.5} /> Resume</span>
```

**Step 4: Replace Pause button icon (line 109)**

Old:
```svelte
        ◼ Pause
```

New:
```svelte
        <span class="flex items-center gap-1"><IconPlayerPause size={14} stroke={1.5} /> Pause</span>
```

**Step 5: Verify**

```bash
cd internal/dashboard/web && npx tsc --noEmit
```

**Step 6: Commit**

```bash
git add internal/dashboard/web/src/components/ProjectTabs.svelte
git commit -m "refactor: replace Unicode icons with Tabler in ProjectTabs.svelte"
```

---

## Task 11: Refactor ProjectSettings.svelte

**Files:**
- Modify: `internal/dashboard/web/src/pages/ProjectSettings.svelte`

**Step 1: Add imports to script block**

Add after the existing imports:
```typescript
  import { IconLayoutDashboard } from '@tabler/icons-svelte';
```

**Step 2: Replace the nav overview icon (line 122)**

Old:
```svelte
      <span class="text-xs tracking-widest text-[var(--color-muted)] uppercase">◈ PROJECT</span>
```

New:
```svelte
      <span class="text-xs tracking-widest text-[var(--color-muted)] uppercase flex items-center gap-1.5"><IconLayoutDashboard size={14} stroke={1.5} /> PROJECT</span>
```

**Step 3: Verify**

```bash
cd internal/dashboard/web && npx tsc --noEmit
```

**Step 4: Commit**

```bash
git add internal/dashboard/web/src/pages/ProjectSettings.svelte
git commit -m "refactor: replace Unicode icons with Tabler in ProjectSettings.svelte"
```

---

## Task 12: Refactor ProjectWizard.svelte (step complete checkmark)

**Files:**
- Modify: `internal/dashboard/web/src/pages/ProjectWizard.svelte`

**Step 1: Add imports to script block**

Find the `<script lang="ts">` block and add:
```typescript
  import { IconCircleCheck } from '@tabler/icons-svelte';
```

**Step 2: Replace wizard step complete checkmark (line 141)**

Old:
```svelte
            {step > n ? '✓' : n}
```

New:
```svelte
            {#if step > n}<IconCircleCheck size={14} stroke={1.5} />{:else}{n}{/if}
```

**Step 3: Verify**

```bash
cd internal/dashboard/web && npx tsc --noEmit
```

**Step 4: Commit**

```bash
git add internal/dashboard/web/src/pages/ProjectWizard.svelte
git commit -m "refactor: replace Unicode icons with Tabler in ProjectWizard.svelte"
```

---

## Task 13: Refactor GlobalOverview.svelte

**Files:**
- Modify: `internal/dashboard/web/src/pages/GlobalOverview.svelte`

**Step 1: Add imports to script block**

Add after the existing imports:
```typescript
  import { IconCircleFilled, IconPlayerPause, IconAlertTriangle, IconCircle } from '@tabler/icons-svelte';
```

**Step 2: Remove the statusDot() function (lines 14–21)**

The `statusDot()` function returns Unicode strings. Delete it entirely.

**Step 3: Replace WebSocket status dot (line 39)**

Old:
```svelte
      <span class="text-[8px]" class:animate-pulse={globalState.wsConnected}>●</span>
```

New:
```svelte
      <span class="flex items-center" class:animate-pulse={globalState.wsConnected}><IconCircleFilled size={10} stroke={1.5} /></span>
```

**Step 4: Find and replace project status dot usage**

Search the rest of GlobalOverview.svelte for `{statusDot(` and replace with inline conditional icon renders. Read the full file first to find all call sites:

```bash
grep -n "statusDot" internal/dashboard/web/src/pages/GlobalOverview.svelte
```

For each call site of `{statusDot(project.status)}`, replace with:
```svelte
{#if project.status === 'running'}<IconCircleFilled size={12} stroke={1.5} />
{:else if project.status === 'paused'}<IconPlayerPause size={12} stroke={1.5} />
{:else if project.status === 'error'}<IconAlertTriangle size={12} stroke={1.5} />
{:else}<IconCircle size={12} stroke={1.5} />{/if}
```

The surrounding `<span>` should add `flex items-center` for alignment, with the existing `statusColor(project.status)` class kept.

**Step 5: Verify**

```bash
cd internal/dashboard/web && npx tsc --noEmit
```

**Step 6: Commit**

```bash
git add internal/dashboard/web/src/pages/GlobalOverview.svelte
git commit -m "refactor: replace Unicode icons with Tabler in GlobalOverview.svelte"
```

---

## Task 14: Full build verification

**Step 1: Run full TypeScript check**

```bash
cd internal/dashboard/web && npx tsc --noEmit
```
Expected: zero errors.

**Step 2: Run Vite production build**

```bash
cd internal/dashboard/web && npm run build
```
Expected: build completes without errors, `dist/` updated.

**Step 3: Grep for leftover Unicode icon literals**

```bash
grep -rn "↺\|↗\|►\|◈\|◁\|▷\|✕\|✓\|✗\|⚠\|●\|○\|⚙\|⊘\|◼\|▲\|▼\|▶\|↻" internal/dashboard/web/src/
```
Expected: no matches in icon usage contexts (there may be ← in TicketFullView BACK TO BOARD text — that is not an icon and should remain).

**Step 4: Final commit (if any stray fixes needed)**

```bash
git add internal/dashboard/web/
git commit -m "chore: final cleanup after tabler icons refactor"
```
