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
  let failedCount  = $derived(projectState.tickets.filter(t => FAIL_STATUSES.includes(t.Status)).length);
  let successRate  = $derived(
    projectState.tickets.length > 0
      ? Math.round((projectState.tickets.filter(t => DONE_STATUSES.includes(t.Status)).length / projectState.tickets.length) * 100)
      : 0
  );
  let totalTickets = $derived(projectState.tickets.length);
  let activeCount  = $derived(
    projectState.tickets.filter(t =>
      !DONE_STATUSES.includes(t.Status) && !FAIL_STATUSES.includes(t.Status) && t.Status !== 'queued'
    ).length
  );
</script>

<div class="flex-1 overflow-y-auto">

  <!-- Header -->
  <div class="flex items-center justify-between px-4 py-3 border-b border-[var(--color-border)] bg-[var(--color-surface)] sticky top-0">
    <span class="text-[10px] font-bold tracking-[0.2em] text-[var(--color-muted-bright)] uppercase">System Health</span>
    <button
      class="text-[10px] text-[var(--color-muted)] hover:text-[var(--color-accent)] transition-colors tracking-wider uppercase"
      onclick={() => projectState.deselectTicket()}
    >← Back</button>
  </div>

  <div class="p-4 space-y-4">

    <!-- Daemon & Connectivity -->
    <section>
      <div class="text-[10px] tracking-[0.15em] text-[var(--color-muted)] uppercase mb-2 px-1 font-bold">Daemon</div>
      <div class="bg-[var(--color-surface)] border border-[var(--color-border)] divide-y divide-[var(--color-border)]">
        <!-- Daemon state -->
        <div class="flex items-center justify-between px-3 py-2.5">
          <span class="text-xs text-[var(--color-muted-bright)]">Status</span>
          <div class="flex items-center gap-2">
            <span class="w-2 h-2 shrink-0"
                  class:bg-[var(--color-success)]={globalState.daemonState === 'running'}
                  class:animate-pulse={globalState.daemonState === 'running'}
                  class:bg-[var(--color-warning)]={globalState.daemonState !== 'running'}></span>
            <span class="text-xs font-bold uppercase tracking-wider">{globalState.daemonState}</span>
          </div>
        </div>
        <!-- WebSocket -->
        <div class="flex items-center justify-between px-3 py-2.5">
          <span class="text-xs text-[var(--color-muted-bright)]">WebSocket</span>
          <div class="flex items-center gap-2">
            <span class="w-2 h-2 shrink-0"
                  class:bg-[var(--color-success)]={globalState.wsConnected}
                  class:bg-[var(--color-danger)]={!globalState.wsConnected}></span>
            <span class="text-xs font-bold tracking-wider"
                  class:text-[var(--color-success)]={globalState.wsConnected}
                  class:text-[var(--color-danger)]={!globalState.wsConnected}>
              {globalState.wsConnected ? 'Connected' : 'Disconnected'}
            </span>
          </div>
        </div>
      </div>
    </section>

    <!-- Pipeline -->
    <section>
      <div class="text-[10px] tracking-[0.15em] text-[var(--color-muted)] uppercase mb-2 px-1 font-bold">Pipeline</div>
      <div class="bg-[var(--color-surface)] border border-[var(--color-border)] grid grid-cols-3 divide-x divide-[var(--color-border)]">
        <div class="p-3 text-center">
          <div class="text-[10px] text-[var(--color-muted)] tracking-wider uppercase mb-1.5">Queued</div>
          <div class="text-2xl font-bold tabular-nums">{queuedCount}</div>
        </div>
        <div class="p-3 text-center">
          <div class="text-[10px] text-[var(--color-muted)] tracking-wider uppercase mb-1.5">Active</div>
          <div class="text-2xl font-bold tabular-nums"
               class:text-[var(--color-accent)]={activeCount > 0}>{activeCount}</div>
        </div>
        <div class="p-3 text-center">
          <div class="text-[10px] text-[var(--color-muted)] tracking-wider uppercase mb-1.5">Failed</div>
          <div class="text-2xl font-bold tabular-nums"
               class:text-[var(--color-danger)]={failedCount > 0}
               class:text-[var(--color-muted)]={failedCount === 0}>{failedCount}</div>
        </div>
      </div>
    </section>

    <!-- Cost -->
    <section>
      <div class="text-[10px] tracking-[0.15em] text-[var(--color-muted)] uppercase mb-2 px-1 font-bold">Cost</div>
      <div class="bg-[var(--color-surface)] border border-[var(--color-border)] grid grid-cols-2 divide-x divide-[var(--color-border)]">
        <div class="p-3">
          <div class="text-[10px] text-[var(--color-muted)] uppercase mb-1.5 tracking-wider">Daily</div>
          <div class="text-sm font-bold text-[var(--color-accent)] tabular-nums">{formatCost(projectState.dailyCost)}</div>
        </div>
        <div class="p-3">
          <div class="text-[10px] text-[var(--color-muted)] uppercase mb-1.5 tracking-wider">Monthly</div>
          <div class="text-sm font-bold tabular-nums">{formatCost(projectState.monthlyCost)}</div>
        </div>
      </div>
    </section>

    <!-- Throughput -->
    <section>
      <div class="text-[10px] tracking-[0.15em] text-[var(--color-muted)] uppercase mb-2 px-1 font-bold">Throughput</div>
      <div class="bg-[var(--color-surface)] border border-[var(--color-border)] divide-y divide-[var(--color-border)]">
        <div class="grid grid-cols-2 divide-x divide-[var(--color-border)]">
          <div class="p-3 text-center">
            <div class="text-[10px] text-[var(--color-muted)] uppercase mb-1.5 tracking-wider">Done Today</div>
            <div class="text-2xl font-bold text-[var(--color-success)] tabular-nums">{doneToday}</div>
          </div>
          <div class="p-3 text-center">
            <div class="text-[10px] text-[var(--color-muted)] uppercase mb-1.5 tracking-wider">Success Rate</div>
            <div class="text-2xl font-bold tabular-nums"
                 class:text-[var(--color-success)]={successRate >= 80}
                 class:text-[var(--color-warning)]={successRate >= 50 && successRate < 80}
                 class:text-[var(--color-danger)]={successRate < 50}>
              {successRate}<span class="text-sm">%</span>
            </div>
          </div>
        </div>
        <div class="flex items-center justify-between px-3 py-2.5">
          <span class="text-xs text-[var(--color-muted-bright)]">Total Tickets</span>
          <span class="text-sm font-bold tabular-nums">{totalTickets}</span>
        </div>
      </div>
    </section>
  </div>
</div>
