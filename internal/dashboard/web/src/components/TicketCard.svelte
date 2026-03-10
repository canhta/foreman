<script lang="ts">
  import type { TicketSummary } from '../types';

  interface Props {
    ticket: TicketSummary;
    onclick: () => void;
  }
  let { ticket, onclick }: Props = $props();

  let progress = $derived(
    ticket.tasks_done != null && ticket.tasks_total
      ? Math.round((ticket.tasks_done / ticket.tasks_total) * 100)
      : 0
  );

  let needsInput = $derived(
    ticket.Status === 'clarification_needed'
  );

  let hasPR = $derived(
    ['pr_created', 'pr_updated', 'awaiting_merge', 'merged', 'pr_closed'].includes(ticket.Status)
  );
</script>

<button
  {onclick}
  class="w-full text-left border border-[var(--color-border)] p-3
         hover:bg-[var(--color-surface-hover)] hover:border-[var(--color-border-strong)]
         hover:-translate-y-0.5 transition-all duration-150 cursor-pointer"
  class:border-l-[var(--color-warning)]={needsInput}
  class:border-l-2={needsInput}
>
  <div class="text-[10px] text-[var(--color-muted)] tracking-wider">{ticket.ID.slice(0, 8)}</div>
  <div class="text-xs mt-1 leading-tight line-clamp-2">{ticket.Title}</div>

  {#if ticket.tasks_total > 0}
    <div class="mt-2 flex items-center gap-2">
      <div class="flex-1 h-1 bg-[var(--color-surface)]">
        <div class="h-full bg-[var(--color-accent)]" style="width: {progress}%"></div>
      </div>
      <span class="text-[10px] text-[var(--color-muted)]">{ticket.tasks_done}/{ticket.tasks_total}</span>
    </div>
  {/if}

  <div class="mt-2 flex items-center gap-3 text-[10px] text-[var(--color-muted)]">
    {#if ticket.CostUSD > 0}
      <span>${ticket.CostUSD.toFixed(2)}</span>
    {/if}
    {#if needsInput}
      <span class="text-[var(--color-warning)]">needs input</span>
    {/if}
    {#if hasPR}
      <span class="text-[var(--color-accent)]">PR</span>
    {/if}
  </div>
</button>
