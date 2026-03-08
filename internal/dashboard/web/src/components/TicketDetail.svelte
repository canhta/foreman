<script lang="ts">
  import {
    appState, deselectTicket, retryTicket, deleteTicketAction,
  } from '../state.svelte';
  import { FAIL_STATUSES, ACTIVE_STATUSES, DONE_STATUSES } from '../types';
  import { formatSender, formatRelative, formatCost } from '../format';
  import TaskCard from './TaskCard.svelte';
  import DagView from './DagView.svelte';
  import ActivityStream from './ActivityStream.svelte';
  import CostBreakdown from './CostBreakdown.svelte';

  let activeTab = $state<'tasks' | 'activity' | 'cost'>('tasks');

  let isFailed = $derived(
    appState.ticketDetail ? FAIL_STATUSES.includes(appState.ticketDetail.Status) : false
  );
  let isActive = $derived(
    appState.ticketDetail ? ACTIVE_STATUSES.includes(appState.ticketDetail.Status) : false
  );
  let isDone = $derived(
    appState.ticketDetail ? DONE_STATUSES.includes(appState.ticketDetail.Status) : false
  );

  let tasksDone = $derived(appState.ticketTasks.filter(t => t.Status === 'done').length);

  let statusBg = $derived(
    isFailed ? 'bg-danger text-bg' :
    isActive ? 'bg-accent text-bg' :
    isDone ? 'bg-success text-bg' :
    'bg-surface-active text-muted-bright'
  );

  function handleRetry() {
    if (appState.selectedTicketId && confirm('Retry this ticket?')) retryTicket(appState.selectedTicketId);
  }

  function handleDelete() {
    if (appState.selectedTicketId && confirm('Permanently delete this ticket and all its data?'))
      deleteTicketAction(appState.selectedTicketId);
  }

  function handleKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape') { deselectTicket(); e.preventDefault(); }
  }
</script>

<svelte:window onkeydown={handleKeydown} />

{#if appState.ticketDetail}
  <div class="flex flex-col h-full">
    <!-- Status banner -->
    <div class="flex items-center justify-between px-3 py-1.5 {statusBg}">
      <div class="flex items-center gap-2">
        <button
          class="text-[10px] opacity-60 hover:opacity-100 transition-opacity tracking-wider"
          onclick={deselectTicket}
          aria-label="Back to ticket list"
        >← BACK</button>
        <span class="text-[10px] font-bold tracking-[0.2em]">
          {appState.ticketDetail.Status.toUpperCase()}
          {#if isActive}<span class="animate-pulse ml-1">▌</span>{/if}
        </span>
      </div>
      <div class="flex items-center gap-2">
        {#if appState.ticketDetail.PRURL}
          <a
            href={appState.ticketDetail.PRURL}
            target="_blank"
            class="text-[10px] tracking-wider opacity-80 hover:opacity-100 transition-opacity"
          >PR #{appState.ticketDetail.PRNumber || '—'} ↗</a>
        {/if}
        {#if isFailed || appState.ticketDetail.Status === 'partial'}
          <button
            class="text-[10px] tracking-wider opacity-80 hover:opacity-100 transition-opacity"
            onclick={handleRetry}
          >↺ RETRY</button>
        {/if}
        <button
          class="text-[10px] tracking-wider opacity-60 hover:opacity-100 transition-opacity"
          onclick={handleDelete}
        >✕ DEL</button>
      </div>
    </div>

    <!-- Ticket title + meta -->
    <div class="px-4 py-3 border-b-2 border-border">
      <h2 class="text-sm font-bold text-text leading-snug">{appState.ticketDetail.Title}</h2>
      <div class="flex flex-wrap items-center gap-2 mt-2 text-[10px] text-muted">
        <span class="text-muted-bright">{formatSender(appState.ticketDetail.ChannelSenderID)}</span>
        <span>·</span>
        <span>{formatRelative(appState.ticketDetail.StartedAt || appState.ticketDetail.CreatedAt)}</span>
        <span>·</span>
        <span class="text-muted-bright">{formatCost(appState.ticketDetail.CostUSD)}</span>
        {#if appState.ticketDetail.TotalLlmCalls > 0}
          <span>·</span>
          <span>{appState.ticketDetail.TotalLlmCalls} LLM calls</span>
        {/if}
      </div>
    </div>

    <!-- Clarification warning -->
    {#if appState.ticketDetail.ClarificationRequestedAt}
      <div class="mx-4 mt-3 border-l-4 border-l-warning p-2 bg-warning-bg text-xs">
        <div class="text-warning font-bold">⚠ CLARIFICATION NEEDED</div>
        <div class="text-text mt-1">{appState.ticketDetail.ErrorMessage || 'Clarification was requested'}</div>
        {#if appState.ticketDetail.Comments?.length}
          <div class="text-muted-bright mt-1 border-t border-border-strong pt-1">
            {appState.ticketDetail.Comments[appState.ticketDetail.Comments.length - 1].Body}
          </div>
        {/if}
      </div>
    {/if}

    <!-- Error message -->
    {#if isFailed && appState.ticketDetail.ErrorMessage}
      <div class="mx-4 mt-3 border-l-4 border-l-danger p-2 bg-danger-bg text-xs">
        <div class="text-danger font-bold">✕ FAILURE</div>
        <div class="text-text/80 mt-1">{appState.ticketDetail.ErrorMessage}</div>
      </div>
    {/if}

    <!-- DAG View -->
    <div class="px-4 mt-3">
      <DagView tasks={appState.ticketTasks} />
    </div>

    <!-- Tab bar -->
    <div class="flex border-b-2 border-border mt-3">
      {#each [
        ['tasks', `TASKS`, tasksDone + '/' + appState.ticketTasks.length],
        ['activity', 'ACTIVITY', ''],
        ['cost', 'COST', formatCost(appState.ticketDetail.CostUSD)],
      ] as [key, lbl, sub]}
        <button
          class="flex-1 py-2 px-2 text-xs border-r border-border last:border-r-0 transition-colors
            {activeTab === key
              ? 'bg-accent text-bg font-bold border-b-2 border-b-accent -mb-px'
              : 'text-muted hover:text-text hover:bg-surface-hover'}"
          onclick={() => { activeTab = key as any; }}
        >
          <div class="tracking-wider">{lbl}</div>
          {#if sub}<div class="text-[10px] opacity-70">{sub}</div>{/if}
        </button>
      {/each}
    </div>

    <!-- Tab content -->
    <div class="flex-1 overflow-y-auto">
      {#if activeTab === 'tasks'}
        <div class="p-3 space-y-1">
          {#each appState.ticketTasks as task (task.ID)}
            <TaskCard {task} events={appState.ticketEvents} />
          {/each}
          {#if appState.ticketTasks.length === 0}
            <div class="text-center text-muted text-xs py-8 tracking-wider">NO TASKS YET</div>
          {/if}
        </div>
      {:else if activeTab === 'activity'}
        <ActivityStream events={appState.ticketEvents} tasks={appState.ticketTasks} />
      {:else if activeTab === 'cost'}
        <div class="p-4">
          <CostBreakdown ticket={appState.ticketDetail} llmCalls={appState.ticketLlmCalls} />
        </div>
      {/if}
    </div>
  </div>
{/if}
