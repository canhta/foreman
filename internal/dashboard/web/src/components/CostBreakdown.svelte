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

<div class="space-y-3">
  <!-- Header row -->
  <div class="flex items-center justify-between border-b border-border pb-2">
    <span class="text-[10px] font-bold tracking-[0.2em] text-text">COST BREAKDOWN</span>
    <span class="text-sm font-bold text-text">{formatCost(ticket?.CostUSD || 0)}</span>
  </div>

  <!-- By role -->
  {#each costByRole as item}
    <div class="space-y-1">
      <div class="flex justify-between text-xs">
        <span class="text-muted truncate">{item.role}</span>
        <span class="text-text font-bold tabular-nums">{formatCost(item.cost)}</span>
      </div>
      <div class="h-1.5 bg-border overflow-hidden">
        <div class="h-full bg-accent transition-all" style="width:{item.pct}%"></div>
      </div>
    </div>
  {/each}

  {#if costByRole.length === 0}
    <div class="text-xs text-muted py-2">No LLM call data.</div>
  {/if}

  <!-- Summary stats -->
  <div class="border-t border-border pt-2 grid grid-cols-2 gap-2 text-[10px]">
    <div>
      <div class="text-muted-bright tracking-wider">MODEL</div>
      <div class="text-text mt-0.5 truncate">{summary.model}</div>
    </div>
    <div>
      <div class="text-muted-bright tracking-wider">TOKENS</div>
      <div class="text-text mt-0.5">{formatTokens(summary.totalTokens)}</div>
    </div>
    <div>
      <div class="text-muted-bright tracking-wider">CALLS</div>
      <div class="text-text mt-0.5">{summary.totalCalls}</div>
    </div>
    <div>
      <div class="text-muted-bright tracking-wider">RETRIED</div>
      <div class="{summary.retried > 0 ? 'text-warning' : 'text-text'} mt-0.5">{summary.retried}</div>
    </div>
  </div>
</div>
