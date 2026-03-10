<script lang="ts">
  import type { TicketSummary } from '../types';
  import { PR_STATUSES } from '../types';
  import { IconCircleFilled, IconExternalLink } from '@tabler/icons-svelte';

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

  let needsInput = $derived(ticket.Status === 'clarification_needed');
  let hasPR      = $derived(PR_STATUSES.includes(ticket.Status));
  let isDone     = $derived(['done', 'merged'].includes(ticket.Status));
  let isFailed   = $derived(['failed', 'blocked', 'partial'].includes(ticket.Status));
  let isActive   = $derived(['implementing', 'reviewing', 'planning', 'plan_validating', 'decomposing', 'tdd_verifying'].includes(ticket.Status));

  function borderAccent(): string {
    if (needsInput) return 'border-l-[var(--color-warning)]';
    if (isFailed)   return 'border-l-[var(--color-danger)]';
    if (isDone)     return 'border-l-[var(--color-success)]';
    if (isActive)   return 'border-l-[var(--color-accent)]';
    return 'border-l-[var(--color-border-strong)]';
  }

  function progressColor(): string {
    if (isDone)   return 'bg-[var(--color-success)]';
    if (isFailed) return 'bg-[var(--color-danger)]';
    return 'bg-[var(--color-accent)]';
  }
</script>

<button
  {onclick}
  class="w-full text-left bg-[var(--color-surface)] border border-[var(--color-border)] border-l-2 {borderAccent()}
         hover:bg-[var(--color-surface-hover)] hover:border-[var(--color-border-strong)]
         transition-all duration-150 cursor-pointer p-3.5 space-y-2.5"
  class:shadow-[0_0_12px_rgba(255,170,32,0.06)]={needsInput}
>
  <!-- Top row: ID + status chip -->
  <div class="flex items-center justify-between gap-2">
    <span class="text-[10px] text-[var(--color-muted)] tracking-wider font-mono leading-none">
      {ticket.ID.slice(0, 8)}
    </span>
    {#if needsInput}
      <span class="status-chip status-chip-warn leading-none">Input</span>
    {:else if isFailed}
      <span class="status-chip status-chip-failed leading-none">Failed</span>
    {:else if isDone}
      <span class="status-chip status-chip-done leading-none">Done</span>
    {:else if hasPR}
      <span class="status-chip status-chip-active leading-none">PR</span>
    {:else if isActive}
      <span class="status-chip status-chip-active leading-none">
        <span class="animate-pulse-slow flex items-center"><IconCircleFilled size={10} stroke={1.5} /></span>
        Active
      </span>
    {/if}
  </div>

  <!-- Title -->
  <div class="text-xs leading-snug line-clamp-2 text-[var(--color-text)]">{ticket.Title}</div>

  <!-- Progress bar -->
  {#if ticket.tasks_total > 0}
    <div class="space-y-1">
      <div class="h-[3px] bg-[var(--color-border-strong)] overflow-hidden">
        <div class="h-full {progressColor()} transition-all duration-500"
             style="width: {progress}%"></div>
      </div>
      <div class="flex items-center justify-between text-[10px] text-[var(--color-muted)]">
        <span>{ticket.tasks_done}/{ticket.tasks_total} tasks</span>
        <span>{progress}%</span>
      </div>
    </div>
  {/if}

  <!-- Footer: cost + PR indicator -->
  {#if ticket.CostUSD > 0 || hasPR}
    <div class="flex items-center gap-3 text-[10px] text-[var(--color-muted)]">
      {#if ticket.CostUSD > 0}
        <span class="tabular-nums">${ticket.CostUSD.toFixed(2)}</span>
      {/if}
      {#if hasPR}
        <span class="text-[var(--color-accent)] flex items-center gap-1"><IconExternalLink size={12} stroke={1.5} /> PR open</span>
      {/if}
    </div>
  {/if}
</button>
