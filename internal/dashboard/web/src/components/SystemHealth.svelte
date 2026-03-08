<script lang="ts">
  import {
    appState, deselectTicket,
  } from '../state.svelte';
  import { DONE_STATUSES, FAIL_STATUSES, ACTIVE_STATUSES } from '../types';
  import { formatCost } from '../format';

  let queuedCount = $derived(appState.tickets.filter(t => t.Status === 'queued').length);
  let doneToday = $derived(
    appState.tickets.filter(t =>
      DONE_STATUSES.includes(t.Status) &&
      t.CreatedAt?.slice(0, 10) === new Date().toISOString().slice(0, 10)
    ).length
  );
  let failedCount = $derived(appState.tickets.filter(t => FAIL_STATUSES.includes(t.Status)).length);
  let successRate = $derived(
    appState.tickets.length > 0
      ? Math.round((appState.tickets.filter(t => DONE_STATUSES.includes(t.Status)).length / appState.tickets.length) * 100)
      : 0
  );
  let totalTickets = $derived(appState.tickets.length);

  function budgetPct(used: number, budget: number): number {
    if (!budget) return 0;
    return Math.min(100, (used / budget) * 100);
  }

  function budgetBarCls(pct: number): string {
    if (pct >= 90) return 'bg-danger';
    if (pct >= 75) return 'bg-warning';
    return 'bg-accent';
  }
</script>

<div class="flex-1 overflow-y-auto">
  <!-- Section header -->
  <div class="flex items-center justify-between px-4 py-2 border-b-2 border-border bg-surface sticky top-0">
    <span class="text-xs font-bold tracking-[0.2em] text-muted-bright">SYSTEM HEALTH</span>
    <button
      class="text-xs text-muted hover:text-accent transition-colors tracking-wider"
      onclick={deselectTicket}
    >← BACK</button>
  </div>

  <div class="p-4 space-y-4">
    <!-- Daemon & Connectivity -->
    <section class="border-2 border-border">
      <div class="px-3 py-1.5 border-b border-border bg-surface-active">
        <span class="text-[10px] font-bold tracking-[0.2em] text-muted-bright">DAEMON</span>
      </div>
      <div class="p-3 space-y-2">
        <div class="flex items-center gap-2 text-xs">
          <span class="w-2 h-2 {appState.daemonState === 'running' ? 'bg-success animate-pulse' : 'bg-warning'}"></span>
          <span class="text-text font-bold">{appState.daemonState.toUpperCase()}</span>
        </div>
        <div class="flex items-center gap-2 text-xs">
          <span class="w-2 h-2 {appState.wsConnected ? 'bg-success' : 'bg-danger'}"></span>
          <span class="text-muted">WebSocket</span>
          <span class="{appState.wsConnected ? 'text-success' : 'text-danger'}">
            {appState.wsConnected ? 'CONNECTED' : 'DISCONNECTED'}
          </span>
        </div>
        {#if appState.whatsapp !== null}
          <div class="flex items-center gap-2 text-xs">
            <span class="w-2 h-2 {appState.whatsapp ? 'bg-success' : 'bg-danger'}"></span>
            <span class="text-muted">WhatsApp</span>
            <span class="{appState.whatsapp ? 'text-success' : 'text-danger'}">
              {appState.whatsapp ? 'OK' : 'DOWN'}
            </span>
          </div>
        {/if}
      </div>
    </section>

    <!-- MCP Servers -->
    {#if Object.keys(appState.mcpServers).length > 0}
      <section class="border-2 border-border">
        <div class="px-3 py-1.5 border-b border-border bg-surface-active">
          <span class="text-[10px] font-bold tracking-[0.2em] text-muted-bright">MCP SERVERS</span>
        </div>
        <div class="p-3 space-y-1.5">
          {#each Object.entries(appState.mcpServers) as [name, info]}
            <div class="flex items-center gap-2 text-xs">
              <span class="w-1.5 h-1.5 {info.status === 'ok' ? 'bg-success' : 'bg-danger'}"></span>
              <span class="text-text flex-1">{name}</span>
              {#if info.error}
                <span class="text-danger text-[10px] truncate">{info.error}</span>
              {:else}
                <span class="text-[10px] border px-1 {info.status === 'ok' ? 'text-success border-success/40' : 'text-danger border-danger/40'}">{info.status.toUpperCase()}</span>
              {/if}
            </div>
          {/each}
        </div>
      </section>
    {/if}

    <!-- Pipeline stats -->
    <section class="border-2 border-border">
      <div class="px-3 py-1.5 border-b border-border bg-surface-active">
        <span class="text-[10px] font-bold tracking-[0.2em] text-muted-bright">PIPELINE</span>
      </div>
      <div class="grid grid-cols-3 gap-0">
        <div class="p-3 border-r border-border">
          <div class="text-[10px] text-muted tracking-wider mb-1">QUEUED</div>
          <div class="text-2xl font-bold text-text tabular-nums">{queuedCount}</div>
        </div>
        <div class="p-3 border-r border-border">
          <div class="text-[10px] text-muted tracking-wider mb-1">ACTIVE</div>
          <div class="text-2xl font-bold {appState.activeCount > 0 ? 'text-accent' : 'text-text'} tabular-nums">{appState.activeCount}</div>
        </div>
        <div class="p-3">
          <div class="text-[10px] text-muted tracking-wider mb-1">FAILED</div>
          <div class="text-2xl font-bold {failedCount > 0 ? 'text-danger' : 'text-muted'} tabular-nums">{failedCount}</div>
        </div>
      </div>
    </section>

    <!-- Cost budgets -->
    <section class="border-2 border-border">
      <div class="px-3 py-1.5 border-b border-border bg-surface-active">
        <span class="text-[10px] font-bold tracking-[0.2em] text-muted-bright">COST BUDGETS</span>
      </div>
      <div class="p-3 space-y-3">
        {#each [
          { label: 'DAILY', used: appState.dailyCost, budget: appState.dailyBudget },
          { label: 'MONTHLY', used: appState.monthlyCost, budget: appState.monthlyBudget },
        ] as gauge}
          {@const pct = budgetPct(gauge.used, gauge.budget)}
          {@const barCls = budgetBarCls(pct)}
          <div>
            <div class="flex justify-between text-xs mb-1.5">
              <span class="text-muted tracking-wider">{gauge.label}</span>
              <span>
                <span class="{pct >= 80 ? (pct >= 90 ? 'text-danger' : 'text-warning') : 'text-text'}">{formatCost(gauge.used)}</span>
                {#if gauge.budget > 0}<span class="text-muted"> / ${Math.round(gauge.budget)}</span>{/if}
              </span>
            </div>
            <div class="h-2 bg-border overflow-hidden">
              <div class="h-full {barCls} transition-all duration-500" style="width:{pct}%"></div>
            </div>
            {#if pct >= 80}
              <div class="text-[10px] text-warning mt-1">{Math.round(pct)}% used{pct >= 90 ? ' — near limit' : ''}</div>
            {/if}
          </div>
        {/each}
      </div>
    </section>

    <!-- Throughput -->
    <section class="border-2 border-border">
      <div class="px-3 py-1.5 border-b border-border bg-surface-active">
        <span class="text-[10px] font-bold tracking-[0.2em] text-muted-bright">THROUGHPUT</span>
      </div>
      <div class="grid grid-cols-2 gap-0">
        <div class="p-3 border-r border-border">
          <div class="text-[10px] text-muted tracking-wider mb-1">DONE TODAY</div>
          <div class="text-2xl font-bold text-success tabular-nums">{doneToday}</div>
        </div>
        <div class="p-3">
          <div class="text-[10px] text-muted tracking-wider mb-1">SUCCESS RATE</div>
          <div class="text-2xl font-bold {successRate >= 80 ? 'text-success' : successRate >= 50 ? 'text-warning' : 'text-danger'} tabular-nums">
            {successRate}<span class="text-sm">%</span>
          </div>
        </div>
      </div>
      <div class="px-3 pb-3 grid grid-cols-2 gap-0 border-t border-border">
        <div class="pt-3 border-r border-border pr-3">
          <div class="text-[10px] text-muted tracking-wider mb-1">TOTAL TICKETS</div>
          <div class="text-xl font-bold text-text tabular-nums">{totalTickets}</div>
        </div>
        <div class="pt-3 pl-3">
          <div class="text-[10px] text-muted tracking-wider mb-1">WEEKLY COST</div>
          <div class="text-xl font-bold text-text tabular-nums">{formatCost(appState.weeklyCost)}</div>
        </div>
      </div>
    </section>
  </div>
</div>
