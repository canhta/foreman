<script lang="ts">
  import {
    appState, deselectTicket, retryTicket, deleteTicketAction, replyToTicket,
  } from '../state.svelte';
  import { FAIL_STATUSES, ACTIVE_STATUSES, DONE_STATUSES } from '../types';
  import { formatSender, formatRelative, formatCost } from '../format';
  import TaskCard from './TaskCard.svelte';
  import DagView from './DagView.svelte';
  import ActivityStream from './ActivityStream.svelte';
  import CostBreakdown from './CostBreakdown.svelte';
  import ConfirmDialog from './ConfirmDialog.svelte';

  let activeTab = $state<'tasks' | 'activity' | 'cost'>('tasks');
  let confirmDialog = $state<{ open: boolean; title: string; message: string; confirmLabel: string; confirmClass: string; action: () => void }>({
    open: false, title: '', message: '', confirmLabel: 'CONFIRM', confirmClass: 'bg-accent text-bg hover:bg-text', action: () => {},
  });
  let replyText = $state('');
  let replySending = $state(false);

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

  let statusBadge = $derived(
    isFailed ? 'bg-danger/15 text-danger border border-danger/40' :
    isActive ? 'bg-accent/15 text-accent border border-accent/40' :
    isDone ? 'bg-success/15 text-success border border-success/40' :
    'bg-surface-active text-muted-bright border border-border'
  );

  function handleRetry() {
    if (!appState.selectedTicketId) return;
    confirmDialog = {
      open: true,
      title: 'RETRY TICKET',
      message: 'Re-queue this ticket and run it through the pipeline again?',
      confirmLabel: '↺ RETRY',
      confirmClass: 'bg-warning text-bg hover:bg-text',
      action: () => retryTicket(appState.selectedTicketId!),
    };
  }

  async function handleReply() {
    if (!appState.selectedTicketId || !replyText.trim()) return;
    replySending = true;
    try {
      await replyToTicket(appState.selectedTicketId, replyText.trim());
      replyText = '';
    } finally {
      replySending = false;
    }
  }

  function handleDelete() {
    if (!appState.selectedTicketId) return;
    confirmDialog = {
      open: true,
      title: 'DELETE TICKET',
      message: 'Permanently delete this ticket and all its data? This cannot be undone.',
      confirmLabel: '✕ DELETE',
      confirmClass: 'bg-danger text-bg hover:bg-text',
      action: () => deleteTicketAction(appState.selectedTicketId!),
    };
  }

  function closeDialog() { confirmDialog = { ...confirmDialog, open: false }; }
  function runDialog() { confirmDialog.action(); closeDialog(); }

  function handleKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape') { deselectTicket(); e.preventDefault(); }
  }
</script>

<svelte:window onkeydown={handleKeydown} />

{#if appState.ticketDetail}
  <div class="flex flex-col h-full w-full">
    <!-- Action bar -->
    <div class="flex items-center justify-between px-3 py-1.5 bg-surface border-b border-border">
      <button
        class="text-[10px] opacity-60 hover:opacity-100 transition-opacity tracking-wider"
        onclick={deselectTicket}
        aria-label="Back to ticket list"
      >← BACK</button>
      <div class="flex items-center gap-2">
        {#if appState.ticketDetail.PRURL}
          <a
            href={appState.ticketDetail.PRURL}
            target="_blank"
            class="text-[10px] tracking-wider text-muted hover:text-text transition-colors px-2 py-1 border border-border hover:border-border-strong"
          >PR #{appState.ticketDetail.PRNumber || '—'} ↗</a>
        {/if}
        {#if isFailed || appState.ticketDetail.Status === 'partial'}
          <button
            class="text-[10px] font-bold tracking-wider px-3 py-1 bg-warning text-bg hover:bg-text transition-colors"
            onclick={handleRetry}
          >↺ RETRY</button>
        {/if}
        <button
          class="text-[10px] font-bold tracking-wider px-3 py-1 border border-danger text-danger hover:bg-danger hover:text-bg transition-colors"
          onclick={handleDelete}
        >✕ DEL</button>
      </div>
    </div>

    <!-- Ticket title + meta -->
    <div class="px-4 py-3 border-b-2 border-border">
      <div class="flex items-start gap-2 flex-wrap">
        <span class="text-[9px] font-bold tracking-[0.2em] px-1.5 py-0.5 rounded-sm shrink-0 {statusBadge}">
          {appState.ticketDetail.Status.toUpperCase()}
          {#if isActive}<span class="animate-pulse ml-0.5">▌</span>{/if}
        </span>
        <h2 class="text-sm font-bold text-text leading-snug">{appState.ticketDetail.Title}</h2>
      </div>
      <div class="flex flex-wrap items-center gap-x-2 gap-y-1 mt-2 text-[10px] text-muted">
        {#if formatSender(appState.ticketDetail.ChannelSenderID)}
          <span class="text-muted-bright">{formatSender(appState.ticketDetail.ChannelSenderID)}</span>
          <span class="opacity-40">·</span>
        {/if}
        {#if formatRelative(appState.ticketDetail.StartedAt || appState.ticketDetail.CreatedAt)}
          <span>{formatRelative(appState.ticketDetail.StartedAt || appState.ticketDetail.CreatedAt)}</span>
          <span class="opacity-40">·</span>
        {/if}
        <span class="text-muted-bright">{formatCost(appState.ticketDetail.CostUSD)}</span>
        {#if appState.ticketDetail.TotalLlmCalls > 0}
          <span class="opacity-40">·</span>
          <span>{appState.ticketDetail.TotalLlmCalls} LLM calls</span>
        {/if}
      </div>
    </div>

    <!-- Clarification warning + reply -->
    {#if appState.ticketDetail.ClarificationRequestedAt}
      <div class="mx-4 mt-3 border-l-4 border-l-warning p-3 bg-warning-bg text-xs">
        <div class="text-warning font-bold tracking-wider">⚠ CLARIFICATION NEEDED</div>
        <div class="text-text mt-1">{appState.ticketDetail.ErrorMessage || 'Clarification was requested'}</div>
        {#if appState.ticketDetail.Comments?.length}
          <div class="text-muted-bright mt-1 border-t border-border-strong pt-1">
            {appState.ticketDetail.Comments[appState.ticketDetail.Comments.length - 1].Body}
          </div>
        {/if}
        <div class="mt-2 border-t border-border-strong pt-2">
          <textarea
            class="w-full bg-bg border-2 border-border text-text text-xs p-2 font-mono resize-none focus:border-warning focus:outline-none"
            rows="3"
            placeholder="Type your clarification reply..."
            bind:value={replyText}
            disabled={replySending}
          ></textarea>
          <div class="flex justify-end mt-1">
            <button
              class="text-[10px] font-bold tracking-wider px-4 py-1.5 bg-warning text-bg hover:bg-text transition-colors disabled:opacity-40"
              onclick={handleReply}
              disabled={replySending || !replyText.trim()}
            >{replySending ? 'SENDING...' : '→ REPLY & RESUME'}</button>
          </div>
        </div>
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

<ConfirmDialog
  open={confirmDialog.open}
  title={confirmDialog.title}
  message={confirmDialog.message}
  confirmLabel={confirmDialog.confirmLabel}
  confirmClass={confirmDialog.confirmClass}
  onconfirm={runDialog}
  oncancel={closeDialog}
/>
