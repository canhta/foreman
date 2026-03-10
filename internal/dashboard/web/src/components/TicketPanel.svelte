<script lang="ts">
  import { projectState } from '../state/project.svelte';
  import TaskCard from './TaskCard.svelte';

  function statusLabel(status: string): string {
    return status.replace(/_/g, ' ').toUpperCase();
  }

  function statusColor(status: string): string {
    if (['done', 'merged'].includes(status)) return 'text-[var(--color-success)]';
    if (['failed', 'blocked'].includes(status)) return 'text-[var(--color-danger)]';
    if (status.includes('clarification')) return 'text-[var(--color-warning)]';
    return 'text-[var(--color-accent)]';
  }
</script>

{#if projectState.ticketDetail}
  {@const ticket = projectState.ticketDetail}
  <div class="p-4">
    <!-- Header -->
    <div class="flex items-center justify-between mb-4">
      <div>
        <span class="text-[10px] text-[var(--color-muted)] tracking-wider">{ticket.ExternalID || ticket.ID.slice(0, 8)}</span>
        <span class={`text-[10px] ml-2 ${statusColor(ticket.Status)}`}>{statusLabel(ticket.Status)}</span>
      </div>
      <div class="flex items-center gap-2">
        <button
          onclick={() => projectState.panelExpanded = true}
          class="text-[10px] text-[var(--color-muted)] hover:text-[var(--color-text)] px-2 py-1 border border-[var(--color-border)]"
        >
          Expand ▸
        </button>
        <button
          onclick={() => projectState.deselectTicket()}
          class="text-[var(--color-muted)] hover:text-[var(--color-text)] text-sm"
        >✕</button>
      </div>
    </div>

    <h2 class="text-sm font-bold mb-3">{ticket.Title}</h2>

    <!-- Progress -->
    {#if projectState.ticketTasks.length > 0}
      {@const done = projectState.ticketTasks.filter(t => t.Status === 'done').length}
      {@const total = projectState.ticketTasks.length}
      <div class="flex items-center gap-2 mb-4">
        <div class="flex-1 h-1.5 bg-[var(--color-surface)]">
          <div class="h-full bg-[var(--color-accent)]" style="width: {(done/total)*100}%"></div>
        </div>
        <span class="text-[10px] text-[var(--color-muted)]">{done}/{total} tasks</span>
      </div>
    {/if}

    <!-- PR link -->
    {#if ticket.PRURL}
      <div class="mb-4 text-xs">
        <a href={ticket.PRURL} target="_blank" rel="noopener" class="text-[var(--color-accent)] hover:underline">
          PR #{ticket.PRNumber} →
        </a>
      </div>
    {/if}

    <!-- Cost -->
    <div class="mb-4 text-xs text-[var(--color-muted)]">
      Cost: <span class="text-[var(--color-text)]">${ticket.CostUSD?.toFixed(2) ?? '0.00'}</span>
    </div>

    <!-- Description -->
    {#if ticket.Description}
      <div class="mb-4">
        <div class="text-[10px] tracking-widest text-[var(--color-muted)] uppercase mb-1">Description</div>
        <div class="text-xs text-[var(--color-muted-bright)] whitespace-pre-wrap">{ticket.Description}</div>
      </div>
    {/if}

    <!-- Tasks -->
    <div class="text-[10px] tracking-widest text-[var(--color-muted)] uppercase mb-2">Tasks</div>
    <div class="space-y-2">
      {#each projectState.ticketTasks as task (task.ID)}
        <TaskCard
          {task}
          events={projectState.ticketEvents}
          llmCalls={projectState.ticketLlmCalls}
        />
      {/each}
    </div>

    <!-- Actions -->
    <div class="mt-4 flex gap-2">
      {#if ['failed', 'blocked'].includes(ticket.Status)}
        <button
          onclick={() => projectState.retryTicket(ticket.ID)}
          class="text-[10px] px-3 py-1.5 bg-[var(--color-accent)] text-[var(--color-bg)] font-bold tracking-wider"
        >RETRY</button>
      {/if}
    </div>
  </div>
{/if}
