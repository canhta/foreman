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

  let costByRunner = $derived.by(() => {
    const runners: Record<string, number> = {};
    let total = 0;
    for (const c of llmCalls) {
      const key = c.AgentRunner || 'builtin';
      runners[key] = (runners[key] || 0) + (c.CostUSD || 0);
      total += c.CostUSD || 0;
    }
    return Object.entries(runners)
      .map(([runner, cost]) => ({ runner, cost, pct: total > 0 ? (cost / total) * 100 : 0 }))
      .sort((a, b) => b.cost - a.cost);
  });

  let summary = $derived.by(() => {
    let totalTokens = 0;
    const models  = new Set<string>();
    const runners = new Set<string>();
    let ok = 0, retried = 0;
    for (const c of llmCalls) {
      totalTokens += (c.TokensInput || 0) + (c.TokensOutput || 0);
      if (c.Model)       models.add(c.Model);
      if (c.AgentRunner) runners.add(c.AgentRunner);
      if (c.Status === 'success') ok++; else retried++;
    }
    return {
      totalCalls: llmCalls.length,
      ok, retried, totalTokens,
      model:  [...models].join(', ')  || '—',
      runner: [...runners].join(', ') || '—',
    };
  });

  function runnerBarColor(runner: string): string {
    if (runner === 'claudecode') return 'bg-[var(--color-accent)]';
    if (runner === 'copilot')    return 'bg-purple-400';
    return 'bg-[var(--color-muted-bright)]';
  }
</script>

<div class="space-y-4">

  <!-- Header -->
  <div class="flex items-center justify-between pb-3 border-b border-[var(--color-border)]">
    <span class="text-[10px] font-bold tracking-[0.2em] text-[var(--color-muted-bright)] uppercase">Cost Breakdown</span>
    <span class="text-base font-bold text-[var(--color-accent)] tabular-nums">{formatCost(ticket?.CostUSD || 0)}</span>
  </div>

  <!-- By runner -->
  {#if costByRunner.length > 1 || (costByRunner.length === 1 && costByRunner[0].runner !== 'builtin')}
    <div class="space-y-2">
      <div class="text-[10px] tracking-[0.15em] text-[var(--color-muted)] uppercase font-bold">By Runner</div>
      {#each costByRunner as item}
        <div class="space-y-1">
          <div class="flex justify-between items-center text-xs">
            <span class="text-[var(--color-muted-bright)]">{item.runner}</span>
            <span class="font-bold tabular-nums">{formatCost(item.cost)}</span>
          </div>
          <div class="h-[3px] bg-[var(--color-border-strong)] overflow-hidden">
            <div class="h-full transition-all {runnerBarColor(item.runner)}" style="width:{item.pct}%"></div>
          </div>
        </div>
      {/each}
    </div>
  {/if}

  <!-- By stage (pipeline role) -->
  <div class="space-y-2">
    <div class="text-[10px] tracking-[0.15em] text-[var(--color-muted)] uppercase font-bold">By Stage</div>
    {#if costByRole.length === 0}
      <div class="text-xs text-[var(--color-muted)] py-1">No LLM call data</div>
    {:else}
      {#each costByRole as item}
        <div class="space-y-1">
          <div class="flex justify-between items-center text-xs">
            <span class="text-[var(--color-muted-bright)] truncate mr-2">{item.role}</span>
            <span class="font-bold tabular-nums shrink-0">{formatCost(item.cost)}</span>
          </div>
          <div class="h-[3px] bg-[var(--color-border-strong)] overflow-hidden">
            <div class="h-full bg-[var(--color-accent)] transition-all" style="width:{item.pct}%"></div>
          </div>
        </div>
      {/each}
    {/if}
  </div>

  <!-- Summary grid -->
  <div class="pt-3 border-t border-[var(--color-border)] grid grid-cols-2 gap-3">
    <div>
      <div class="text-[10px] text-[var(--color-muted)] uppercase tracking-wider mb-1">Runner</div>
      <div class="text-xs text-[var(--color-text)] truncate">{summary.runner}</div>
    </div>
    <div>
      <div class="text-[10px] text-[var(--color-muted)] uppercase tracking-wider mb-1">Model</div>
      <div class="text-xs text-[var(--color-text)] truncate" title={summary.model}>{summary.model}</div>
    </div>
    <div>
      <div class="text-[10px] text-[var(--color-muted)] uppercase tracking-wider mb-1">Tokens</div>
      <div class="text-xs font-bold tabular-nums">{formatTokens(summary.totalTokens)}</div>
    </div>
    <div>
      <div class="text-[10px] text-[var(--color-muted)] uppercase tracking-wider mb-1">Calls</div>
      <div class="text-xs font-bold tabular-nums">{summary.totalCalls}</div>
    </div>
    {#if summary.retried > 0}
      <div>
        <div class="text-[10px] text-[var(--color-muted)] uppercase tracking-wider mb-1">Retried</div>
        <div class="text-xs font-bold tabular-nums text-[var(--color-warning)]">{summary.retried}</div>
      </div>
    {/if}
  </div>
</div>
