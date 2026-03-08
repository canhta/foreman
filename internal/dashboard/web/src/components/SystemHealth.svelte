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
