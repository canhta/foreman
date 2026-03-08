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
