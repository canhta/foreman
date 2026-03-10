<script lang="ts">
  import { projectState } from '../state/project.svelte';
  import { formatRelative, formatCost, severityIcon, linkifyParts } from '../format';
  import { PR_STATUSES } from '../types';
  import TaskCard from './TaskCard.svelte';

  const ticket = $derived(projectState.ticketDetail);
  const hasPR = $derived(ticket ? PR_STATUSES.includes(ticket.Status) : false);

  function statusLabel(status: string): string {
    return status.replace(/_/g, ' ').toUpperCase();
  }

  function statusColor(status: string): string {
    if (['done', 'merged'].includes(status)) return 'text-[var(--color-success)]';
    if (['failed', 'blocked', 'partial'].includes(status)) return 'text-[var(--color-danger)]';
    if (status.includes('clarification')) return 'text-[var(--color-warning)]';
    return 'text-[var(--color-accent)]';
  }

  function severityColor(sev: string): string {
    if (sev === 'success') return 'text-[var(--color-success)]';
    if (sev === 'error') return 'text-[var(--color-danger)]';
    if (sev === 'warning') return 'text-[var(--color-warning)]';
    return 'text-[var(--color-muted-bright)]';
  }
</script>

{#if ticket}
  <div class="absolute inset-0 bg-[var(--color-bg)] z-20 flex flex-col animate-[zoom-in_0.15s_ease-out] overflow-hidden">
    <!-- Top bar -->
    <div class="h-12 border-b border-[var(--color-border)] px-6 flex items-center gap-4 shrink-0">
      <button
        onclick={() => projectState.collapsePanel()}
        class="text-[10px] text-[var(--color-muted)] hover:text-[var(--color-text)] tracking-wider flex items-center gap-1"
      >
        ← BACK TO BOARD
      </button>
      <div class="h-4 w-px bg-[var(--color-border)]"></div>
      <span class="text-[10px] font-mono text-[var(--color-muted)]">
        {ticket.ExternalID || ticket.ID.slice(0, 8)}
      </span>
      <span class="text-[10px] font-bold tracking-wider {statusColor(ticket.Status)}">
        {statusLabel(ticket.Status)}
      </span>
      <div class="ml-auto flex items-center gap-4 text-[10px] text-[var(--color-muted)]">
        <span>Cost: <span class="text-[var(--color-text)]">{formatCost(ticket.CostUSD ?? 0)}</span></span>
        {#if hasPR && ticket.PRURL}
          <a href={ticket.PRURL} target="_blank" rel="noopener"
             class="text-[var(--color-accent)] hover:underline">PR #{ticket.PRNumber} →</a>
        {/if}
      </div>
    </div>

    <!-- Two-column content -->
    <div class="flex flex-1 overflow-hidden">
      <!-- Left: ticket info + tasks -->
      <div class="flex-1 overflow-y-auto p-6 border-r border-[var(--color-border)]">
        <h1 class="text-sm font-bold mb-4 leading-snug">{ticket.Title}</h1>

        {#if projectState.ticketTasks.length > 0}
          {@const done = projectState.ticketTasks.filter(t => t.Status === 'done').length}
          {@const total = projectState.ticketTasks.length}
          <div class="flex items-center gap-2 mb-6">
            <div class="flex-1 h-1.5 bg-[var(--color-surface)]">
              <div class="h-full bg-[var(--color-accent)] transition-all duration-300"
                   style="width: {total > 0 ? (done / total) * 100 : 0}%"></div>
            </div>
            <span class="text-[10px] text-[var(--color-muted)] shrink-0">{done}/{total} tasks</span>
          </div>
        {/if}

        {#if ticket.Description}
          <div class="mb-6 p-4 bg-[var(--color-surface)] border border-[var(--color-border)]">
            <div class="text-[10px] tracking-widest text-[var(--color-muted)] uppercase mb-2">Description</div>
            <div class="text-xs text-[var(--color-muted-bright)] whitespace-pre-wrap leading-relaxed">
              {ticket.Description}
            </div>
          </div>
        {/if}

        {#if ticket.ErrorMessage}
          <div class="mb-6 border-l-4 border-l-[var(--color-danger)] bg-[var(--color-danger-bg)] p-4">
            <div class="text-[var(--color-danger)] font-bold text-[10px] tracking-wider mb-1">ERROR</div>
            <div class="text-xs text-[var(--color-text)]/80">{ticket.ErrorMessage}</div>
          </div>
        {/if}

        <div class="text-[10px] tracking-widest text-[var(--color-muted)] uppercase mb-3">Tasks</div>
        <div class="space-y-2">
          {#each projectState.ticketTasks as task (task.ID)}
            <TaskCard {task} events={projectState.ticketEvents} llmCalls={projectState.ticketLlmCalls} />
          {/each}
          {#if projectState.ticketTasks.length === 0}
            <div class="text-center text-[var(--color-muted)] text-xs py-8">No tasks yet</div>
          {/if}
        </div>
      </div>

      <!-- Right: event log -->
      <div class="w-80 shrink-0 overflow-y-auto">
        <div class="px-4 py-3 border-b border-[var(--color-border)]">
          <span class="text-[10px] tracking-widest text-[var(--color-muted)] uppercase">Events</span>
        </div>
        <div class="divide-y divide-[var(--color-border)]">
          {#each projectState.ticketEvents as evt (evt.ID)}
            <div class="px-4 py-2.5 flex gap-2 items-start hover:bg-[var(--color-surface-hover)]">
              <span class="shrink-0 text-xs {severityColor(evt.Severity)} mt-0.5">
                {severityIcon(evt.Severity)}
              </span>
              <div class="min-w-0 flex-1">
                <div class="text-xs text-[var(--color-text)] leading-snug">
                  {#each linkifyParts(evt.Message || evt.EventType) as part}
                    {#if part.type === 'url'}
                      <a href={part.content} target="_blank" rel="noopener"
                         class="text-[var(--color-accent)] hover:underline break-all">{part.content}</a>
                    {:else}
                      {part.content}
                    {/if}
                  {/each}
                </div>
                <div class="text-[10px] text-[var(--color-muted)] mt-0.5">
                  {formatRelative(evt.CreatedAt)}
                </div>
              </div>
            </div>
          {/each}
          {#if projectState.ticketEvents.length === 0}
            <div class="text-center text-[var(--color-muted)] text-xs py-8">No events</div>
          {/if}
        </div>
      </div>
    </div>
  </div>
{/if}
