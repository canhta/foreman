<script lang="ts">
  import {
    ticketDetail, ticketTasks, ticketEvents, ticketLlmCalls,
    selectedTicketId, deselectTicket, retryTicket, deleteTicketAction,
  } from '../state.svelte';
  import { FAIL_STATUSES } from '../types';
  import { formatSender, formatRelative, formatCost } from '../format';
  import TaskCard from './TaskCard.svelte';
  import DagView from './DagView.svelte';
  import ActivityStream from './ActivityStream.svelte';
  import CostBreakdown from './CostBreakdown.svelte';

  let activeTab = $state<'tasks' | 'activity' | 'cost'>('tasks');

  let isFailed = $derived(
    ticketDetail ? FAIL_STATUSES.includes(ticketDetail.Status) : false
  );

  let tasksDone = $derived(ticketTasks.filter(t => t.Status === 'done').length);

  function handleRetry() {
    if (selectedTicketId && confirm('Retry this ticket?')) retryTicket(selectedTicketId);
  }

  function handleDelete() {
    if (selectedTicketId && confirm('Permanently delete this ticket and all its data?'))
      deleteTicketAction(selectedTicketId);
  }

  function handleKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape') { deselectTicket(); e.preventDefault(); }
  }
</script>

<svelte:window onkeydown={handleKeydown} />

{#if ticketDetail}
  <div class="flex flex-col h-full">
    <!-- Header -->
    <div class="px-4 py-3 border-b border-border">
      <div class="flex items-center gap-2 mb-2">
        <button
          class="text-xs text-muted hover:text-accent"
          onclick={deselectTicket}
          aria-label="Back to ticket list"
        >&larr; BACK</button>
        <div class="ml-auto flex gap-2">
          {#if isFailed || ticketDetail.Status === 'partial'}
            <button class="text-xs text-accent hover:text-text" onclick={handleRetry}>&#8635; RETRY</button>
          {/if}
          <button class="text-xs text-danger hover:text-text" onclick={handleDelete}>&#10007; DELETE</button>
        </div>
      </div>
      <h2 class="text-base font-bold text-text">{ticketDetail.Title}</h2>
      <div class="text-xs text-muted mt-1 flex gap-2 flex-wrap">
        <span class="{isFailed ? 'text-danger' : 'text-accent'}">{ticketDetail.Status.toUpperCase()}</span>
        <span>&middot; {formatSender(ticketDetail.ChannelSenderID)}</span>
        <span>&middot; {formatRelative(ticketDetail.StartedAt || ticketDetail.CreatedAt)}</span>
        {#if ticketDetail.PRURL}
          <a href={ticketDetail.PRURL} target="_blank" class="text-accent hover:underline">
            PR #{ticketDetail.PRNumber || 'link'}
          </a>
        {/if}
      </div>
    </div>

    <!-- Clarification -->
    {#if ticketDetail.ClarificationRequestedAt}
      <div class="mx-4 mt-3 p-2 border border-warning/30 bg-warning/5 text-xs">
        <div class="text-warning">&#10067; {ticketDetail.ErrorMessage || 'Clarification requested'}</div>
        {#if ticketDetail.Comments?.length}
          <div class="text-text mt-1">{ticketDetail.Comments[ticketDetail.Comments.length - 1].Body}</div>
        {/if}
      </div>
    {/if}

    <!-- DAG View -->
    <div class="px-4 mt-3">
      <DagView tasks={ticketTasks} />
    </div>

    <!-- Tab bar -->
    <div class="flex px-4 mt-3 gap-1 border-b border-border">
      {#each [['tasks', `TASKS ${tasksDone}/${ticketTasks.length}`], ['activity', 'ACTIVITY'], ['cost', 'COST']] as [key, label]}
        <button
          class="text-xs py-2 px-3 border-b-2 {activeTab === key ? 'border-accent text-accent' : 'border-transparent text-muted hover:text-text'}"
          onclick={() => { activeTab = key as any; }}
        >{label}</button>
      {/each}
    </div>

    <!-- Tab content -->
    <div class="flex-1 overflow-y-auto">
      {#if activeTab === 'tasks'}
        <div class="p-4 space-y-1">
          {#each ticketTasks as task (task.ID)}
            <TaskCard {task} events={ticketEvents} />
          {/each}
          {#if ticketTasks.length === 0}
            <div class="text-center text-muted text-sm py-8">No tasks yet.</div>
          {/if}
        </div>
      {:else if activeTab === 'activity'}
        <ActivityStream events={ticketEvents} tasks={ticketTasks} />
      {:else if activeTab === 'cost'}
        <div class="p-4">
          <CostBreakdown ticket={ticketDetail} llmCalls={ticketLlmCalls} />
        </div>
      {/if}
    </div>
  </div>
{/if}
