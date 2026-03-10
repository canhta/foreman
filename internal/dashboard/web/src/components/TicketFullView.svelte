<script lang="ts">
  import { projectState } from '../state/project.svelte';
  import { formatRelative, formatCost, linkifyParts } from '../format';
  import {
    IconCheck, IconX, IconAlertTriangle, IconCircleFilled,
    IconChevronUp, IconChevronDown, IconExternalLink
  } from '@tabler/icons-svelte';
  import { PR_STATUSES } from '../types';
  import TaskCard from './TaskCard.svelte';
  import ChatInterface from './ChatInterface.svelte';
  import DagView from './DagView.svelte';

  const ticket = $derived(projectState.ticketDetail);
  const hasPR = $derived(ticket ? PR_STATUSES.includes(ticket.Status) : false);

  let eventsExpanded = $state(false);

  function statusLabel(status: string): string {
    return status.replace(/_/g, ' ').toUpperCase();
  }

  function statusChipClass(status: string): string {
    if (['done', 'merged'].includes(status)) return 'status-chip status-chip-done';
    if (['failed', 'blocked', 'partial'].includes(status)) return 'status-chip status-chip-failed';
    if (status.includes('clarification')) return 'status-chip status-chip-warn';
    return 'status-chip status-chip-active';
  }

  function severityColor(sev: string): string {
    if (sev === 'success') return 'text-[var(--color-success)]';
    if (sev === 'error') return 'text-[var(--color-danger)]';
    if (sev === 'warning') return 'text-[var(--color-warning)]';
    return 'text-[var(--color-muted-bright)]';
  }

  function severityRowBg(sev: string): string {
    if (sev === 'error') return 'bg-[var(--color-danger-bg)]';
    return '';
  }
</script>

{#if ticket}
  <div class="absolute inset-0 bg-[var(--color-bg)] z-20 flex flex-col animate-[zoom-in_0.15s_ease-out] overflow-hidden">
    <!-- Top bar -->
    <div class="h-14 border-b border-[var(--color-border)] px-6 flex items-center gap-4 shrink-0">
      <button
        onclick={() => projectState.collapsePanel()}
        class="text-xs text-[var(--color-muted)] hover:text-[var(--color-text)] tracking-wider flex items-center gap-1"
      >
        ← BACK TO BOARD
      </button>
      <div class="h-4 w-px bg-[var(--color-border)]"></div>
      <span class="text-[10px] font-mono text-[var(--color-muted)]">
        {ticket.ExternalID || ticket.ID.slice(0, 8)}
      </span>
      <span class="{statusChipClass(ticket.Status)}">
        {statusLabel(ticket.Status)}
      </span>
      <div class="ml-auto flex items-center gap-4 text-[10px] text-[var(--color-muted)]">
        <span>Cost: <span class="text-[var(--color-text)]">{formatCost(ticket.CostUSD ?? 0)}</span></span>
        {#if hasPR && ticket.PRURL && ticket.PRNumber > 0}
          <a href={ticket.PRURL} target="_blank" rel="noopener"
             class="status-chip status-chip-active hover:underline flex items-center gap-1">
            <IconExternalLink size={12} stroke={1.5} /> PR #{ticket.PRNumber}
          </a>
        {/if}
      </div>
    </div>

    <!-- Two-column content -->
    <div class="flex flex-1 overflow-hidden">
      <!-- Left: ticket info + tasks + events accordion -->
      <div class="flex-1 overflow-y-auto p-6 border-r border-[var(--color-border)]">
        <h1 class="text-sm font-bold mb-4 leading-snug">{ticket.Title}</h1>

        {#if projectState.ticketTasks.length > 0}
          {@const done = projectState.ticketTasks.filter(t => t.Status === 'done').length}
          {@const total = projectState.ticketTasks.length}
          <div class="flex items-center gap-2 mb-6">
            <div class="progress-track flex-1">
              <div class="progress-fill {done === total ? 'progress-fill-success' : ''} transition-all duration-300"
                   style="width: {total > 0 ? (done / total) * 100 : 0}%"></div>
            </div>
            <span class="text-[10px] text-[var(--color-muted)] shrink-0">{done}/{total} tasks</span>
          </div>
        {/if}

        {#if ticket.Description}
          <div class="card mb-6 p-4">
            <div class="text-xs tracking-widest text-[var(--color-muted)] uppercase mb-2">Description</div>
            <div class="text-xs text-[var(--color-muted-bright)] whitespace-pre-wrap leading-relaxed">
              {ticket.Description}
            </div>
          </div>
        {/if}

        {#if ticket.ErrorMessage}
          <div class="mb-6 border-l-4 border-l-[var(--color-danger)] border-t border-t-[var(--color-danger)] bg-[var(--color-danger-bg)] p-4">
            <div class="text-[var(--color-danger)] font-bold text-xs tracking-wider mb-1">ERROR</div>
            <div class="text-xs text-[var(--color-text)]/80">{ticket.ErrorMessage}</div>
          </div>
        {/if}

        <div class="text-xs tracking-[0.15em] uppercase font-bold text-[var(--color-muted-bright)] mb-3">Tasks</div>
        <div class="mb-4">
          <DagView tasks={projectState.ticketTasks} />
        </div>
        <div class="space-y-2 mb-6">
          {#each projectState.ticketTasks as task (task.ID)}
            <TaskCard {task} events={projectState.ticketEvents} llmCalls={projectState.ticketLlmCalls} />
          {/each}
          {#if projectState.ticketTasks.length === 0}
            <div class="text-center text-[var(--color-muted)] text-xs py-8">No tasks yet</div>
          {/if}
        </div>

        <!-- Events accordion -->
        <div class="border border-[var(--color-border)]">
          <button
            class="w-full px-4 py-3 flex items-center justify-between hover:bg-[var(--color-surface-hover)] transition-colors"
            onclick={() => eventsExpanded = !eventsExpanded}
          >
            <div class="flex items-center gap-2">
              <span class="text-xs tracking-[0.15em] uppercase font-bold text-[var(--color-muted-bright)]">Events</span>
              {#if projectState.ticketEvents.length > 0}
                <span class="status-chip status-chip-neutral">{projectState.ticketEvents.length}</span>
              {/if}
            </div>
            <span class="text-[var(--color-muted)] flex items-center">
              {#if eventsExpanded}<IconChevronUp size={14} stroke={1.5} />{:else}<IconChevronDown size={14} stroke={1.5} />{/if}
            </span>
          </button>
          {#if eventsExpanded}
            <div class="divide-y divide-[var(--color-border)] border-t border-[var(--color-border)]">
              {#each projectState.ticketEvents as evt (evt.ID)}
                <div class="px-4 py-2.5 flex gap-2 items-start hover:bg-[var(--color-surface-hover)] {severityRowBg(evt.Severity)}">
                  <span class="shrink-0 flex items-center mt-0.5 {severityColor(evt.Severity)}">
                    {#if evt.Severity === 'success'}<IconCheck size={14} stroke={1.5} />
                    {:else if evt.Severity === 'error'}<IconX size={14} stroke={1.5} />
                    {:else if evt.Severity === 'warning'}<IconAlertTriangle size={14} stroke={1.5} />
                    {:else}<IconCircleFilled size={14} stroke={1.5} />{/if}
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
          {/if}
        </div>
      </div>

      <!-- Right: chat interface -->
      <div class="w-80 shrink-0 flex flex-col overflow-hidden">
        <div class="px-4 py-3 border-b border-[var(--color-border)] shrink-0">
          <span class="text-xs font-bold tracking-widest text-[var(--color-muted-bright)] uppercase">Chat</span>
          <div class="mt-2 border-t border-[var(--color-border)]"></div>
        </div>
        <div class="flex-1 overflow-hidden">
          <ChatInterface
            messages={projectState.chatMessages}
            onSend={(content) => projectState.sendChatMessage(ticket.ID, content)}
          />
        </div>
      </div>
    </div>
  </div>
{/if}
