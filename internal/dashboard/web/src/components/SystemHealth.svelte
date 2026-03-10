<script lang="ts">
  import { globalState } from '../state/global.svelte';
  import { projectState } from '../state/project.svelte';
  import { DONE_STATUSES, FAIL_STATUSES } from '../types';
  import { formatCost, localDateStr } from '../format';

  let queuedCount = $derived(projectState.tickets.filter(t => t.Status === 'queued').length);
  let doneToday = $derived(
    projectState.tickets.filter(t =>
      DONE_STATUSES.includes(t.Status) &&
      t.CreatedAt?.slice(0, 10) === localDateStr()
    ).length
  );
  let failedCount = $derived(projectState.tickets.filter(t => FAIL_STATUSES.includes(t.Status)).length);
  let successRate = $derived(
    projectState.tickets.length > 0
      ? Math.round((projectState.tickets.filter(t => DONE_STATUSES.includes(t.Status)).length / projectState.tickets.length) * 100)
      : 0
  );
  let totalTickets = $derived(projectState.tickets.length);
  let activeCount = $derived(
    projectState.tickets.filter(t => !DONE_STATUSES.includes(t.Status) && !FAIL_STATUSES.includes(t.Status) && t.Status !== 'queued').length
  );
</script>

<div class="flex-1 overflow-y-auto">
  <!-- Section header -->
  <div class="flex items-center justify-between px-4 py-2 border-b-2 border-border bg-surface sticky top-0">
    <span class="text-xs font-bold tracking-[0.2em] text-muted-bright">SYSTEM HEALTH</span>
    <button
      class="text-xs text-muted hover:text-accent transition-colors tracking-wider"
      onclick={() => projectState.deselectTicket()}
    >← BACK</button>
  </div>

  <div class="p-4 space-y-4">
    <!-- Daemon & Connectivity -->
    <section class="border-2 border-border">
      <div class="px-3 py-1.5 border-b border-border bg-surface-active border-l-4 border-l-accent">
        <span class="text-[10px] font-bold tracking-[0.2em] text-text">DAEMON</span>
      </div>
      <div class="p-3 space-y-2">
        <div class="flex items-center gap-2 text-xs">
          <span class="w-2 h-2 {globalState.daemonState === 'running' ? 'bg-success animate-pulse' : 'bg-warning'}"></span>
          <span class="text-text font-bold">{globalState.daemonState.toUpperCase()}</span>
        </div>
        <div class="flex items-center gap-2 text-xs">
          <span class="w-2 h-2 {globalState.wsConnected ? 'bg-success' : 'bg-danger'}"></span>
          <span class="text-muted">WebSocket</span>
          <span class="{globalState.wsConnected ? 'text-success' : 'text-danger'}">
            {globalState.wsConnected ? 'CONNECTED' : 'DISCONNECTED'}
          </span>
        </div>
      </div>
    </section>

    <!-- Pipeline stats -->
    <section class="border-2 border-border">
      <div class="px-3 py-1.5 border-b border-border bg-surface-active border-l-4 border-l-accent">
        <span class="text-[10px] font-bold tracking-[0.2em] text-text">PIPELINE</span>
      </div>
      <div class="grid grid-cols-3 gap-0">
        <div class="p-3 border-r border-border">
          <div class="text-[10px] text-muted-bright tracking-wider mb-1">QUEUED</div>
          <div class="text-2xl font-bold text-text tabular-nums">{queuedCount}</div>
        </div>
        <div class="p-3 border-r border-border">
          <div class="text-[10px] text-muted-bright tracking-wider mb-1">ACTIVE</div>
          <div class="text-2xl font-bold {activeCount > 0 ? 'text-accent' : 'text-text'} tabular-nums">{activeCount}</div>
        </div>
        <div class="p-3">
          <div class="text-[10px] text-muted-bright tracking-wider mb-1">FAILED</div>
          <div class="text-2xl font-bold {failedCount > 0 ? 'text-danger' : 'text-muted'} tabular-nums">{failedCount}</div>
        </div>
      </div>
    </section>

    <!-- Cost -->
    <section class="border-2 border-border">
      <div class="px-3 py-1.5 border-b border-border bg-surface-active border-l-4 border-l-accent">
        <span class="text-[10px] font-bold tracking-[0.2em] text-text">COST</span>
      </div>
      <div class="p-3 grid grid-cols-2 gap-3 text-xs">
        <div>
          <div class="text-muted tracking-wider mb-1">DAILY</div>
          <div class="text-text font-bold">{formatCost(projectState.dailyCost)}</div>
        </div>
        <div>
          <div class="text-muted tracking-wider mb-1">MONTHLY</div>
          <div class="text-text font-bold">{formatCost(projectState.monthlyCost)}</div>
        </div>
      </div>
    </section>

    <!-- Throughput -->
    <section class="border-2 border-border">
      <div class="px-3 py-1.5 border-b border-border bg-surface-active border-l-4 border-l-accent">
        <span class="text-[10px] font-bold tracking-[0.2em] text-text">THROUGHPUT</span>
      </div>
      <div class="grid grid-cols-2 gap-0">
        <div class="p-3 border-r border-border">
          <div class="text-[10px] text-muted-bright tracking-wider mb-1">DONE TODAY</div>
          <div class="text-2xl font-bold text-success tabular-nums">{doneToday}</div>
        </div>
        <div class="p-3">
          <div class="text-[10px] text-muted-bright tracking-wider mb-1">SUCCESS RATE</div>
          <div class="text-2xl font-bold {successRate >= 80 ? 'text-success' : successRate >= 50 ? 'text-warning' : 'text-danger'} tabular-nums">
            {successRate}<span class="text-sm">%</span>
          </div>
        </div>
      </div>
      <div class="px-3 pb-3 grid grid-cols-1 gap-0 border-t border-border">
        <div class="pt-3">
          <div class="text-[10px] text-muted-bright tracking-wider mb-1">TOTAL TICKETS</div>
          <div class="text-xl font-bold text-text tabular-nums">{totalTickets}</div>
        </div>
      </div>
    </section>
  </div>
</div>
