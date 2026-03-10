<script lang="ts">
  import { projectState } from '../state/project.svelte';
  import { formatRelative, formatCost, severityIcon, linkifyParts } from '../format';
  import { PR_STATUSES } from '../types';
  import TaskCard from './TaskCard.svelte';
  import ConfirmDialog from './ConfirmDialog.svelte';

  type Tab = 'tasks' | 'events' | 'chat';
  let activeTab = $state<Tab>('tasks');
  let showDeleteConfirm = $state(false);
  let chatInput = $state('');
  let sendingChat = $state(false);

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

  async function sendChat() {
    if (!ticket || !chatInput.trim()) return;
    sendingChat = true;
    try {
      await projectState.sendChatMessage(ticket.ID, chatInput.trim());
      chatInput = '';
    } finally {
      sendingChat = false;
    }
  }

  async function handleDelete() {
    if (!ticket) return;
    await projectState.deleteTicket(ticket.ID);
    showDeleteConfirm = false;
  }
</script>

{#if ticket}
  <div class="flex flex-col h-full animate-[slide-in-right_0.2s_ease-out]">
    <!-- Header -->
    <div class="px-4 pt-4 pb-3 border-b border-[var(--color-border)] shrink-0">
      <div class="flex items-start justify-between gap-2 mb-2">
        <div class="min-w-0">
          <div class="flex items-center gap-2 flex-wrap">
            <span class="text-[10px] text-[var(--color-muted)] tracking-wider font-mono">
              {ticket.ExternalID || ticket.ID.slice(0, 8)}
            </span>
            <span class="text-[10px] font-bold tracking-wider {statusColor(ticket.Status)}">
              {statusLabel(ticket.Status)}
            </span>
          </div>
          <h2 class="text-xs font-bold mt-1 leading-snug">{ticket.Title}</h2>
        </div>
        <div class="flex items-center gap-1 shrink-0">
          <button
            onclick={() => projectState.expandPanel()}
            class="text-[10px] text-[var(--color-muted)] hover:text-[var(--color-text)] px-2 py-1 border border-[var(--color-border)] hover:border-[var(--color-border-strong)] transition-colors"
            title="Expand full view"
          >⤢</button>
          <button
            onclick={() => projectState.deselectTicket()}
            class="text-[var(--color-muted)] hover:text-[var(--color-text)] px-2 py-1 border border-[var(--color-border)] hover:border-[var(--color-border-strong)] transition-colors text-sm leading-none"
          >✕</button>
        </div>
      </div>

      <!-- Progress bar -->
      {#if projectState.ticketTasks.length > 0}
        {@const done = projectState.ticketTasks.filter(t => t.Status === 'done').length}
        {@const total = projectState.ticketTasks.length}
        <div class="flex items-center gap-2 mt-2">
          <div class="flex-1 h-1 bg-[var(--color-surface)]">
            <div class="h-full bg-[var(--color-accent)] transition-all duration-300"
                 style="width: {total > 0 ? (done / total) * 100 : 0}%"></div>
          </div>
          <span class="text-[10px] text-[var(--color-muted)] shrink-0">{done}/{total}</span>
        </div>
      {/if}

      <!-- Meta row -->
      <div class="flex items-center gap-3 mt-2 text-[10px] text-[var(--color-muted)] flex-wrap">
        <span>Cost: <span class="text-[var(--color-text)]">{formatCost(ticket.CostUSD ?? 0)}</span></span>
        {#if hasPR && ticket.PRURL}
          <a href={ticket.PRURL} target="_blank" rel="noopener"
             class="text-[var(--color-accent)] hover:underline">
            PR #{ticket.PRNumber} →
          </a>
        {/if}
        {#if ticket.ChannelSenderID}
          <span class="truncate max-w-[120px]" title={ticket.ChannelSenderID}>
            by {ticket.ChannelSenderID.split('@')[0]}
          </span>
        {/if}
      </div>
    </div>

    <!-- Tabs -->
    <div class="flex border-b border-[var(--color-border)] shrink-0">
      {#each (['tasks', 'events', 'chat'] as Tab[]) as tab}
        {@const count = tab === 'tasks' ? projectState.ticketTasks.length
                      : tab === 'events' ? projectState.ticketEvents.length
                      : projectState.chatMessages.length}
        <button
          onclick={() => activeTab = tab}
          class="px-4 py-2 text-[10px] tracking-widest uppercase border-b-2 transition-colors"
          class:border-[var(--color-accent)]={activeTab === tab}
          class:text-[var(--color-accent)]={activeTab === tab}
          class:border-transparent={activeTab !== tab}
          class:text-[var(--color-muted)]={activeTab !== tab}
          class:hover:text-[var(--color-muted-bright)]={activeTab !== tab}
        >
          {tab}{count > 0 ? ` · ${count}` : ''}
        </button>
      {/each}
    </div>

    <!-- Tab content -->
    <div class="flex-1 overflow-y-auto">

      {#if activeTab === 'tasks'}
        <div class="p-3 space-y-2">
          {#if ticket.Description}
            <div class="mb-3 p-3 bg-[var(--color-surface)] border border-[var(--color-border)]">
              <div class="text-[10px] tracking-widest text-[var(--color-muted)] uppercase mb-1">Description</div>
              <div class="text-xs text-[var(--color-muted-bright)] whitespace-pre-wrap leading-relaxed">
                {ticket.Description}
              </div>
            </div>
          {/if}
          {#if ticket.ErrorMessage}
            <div class="mb-3 border-l-4 border-l-[var(--color-danger)] bg-[var(--color-danger-bg)] p-3">
              <div class="text-[var(--color-danger)] font-bold text-[10px] tracking-wider mb-1">ERROR</div>
              <div class="text-xs text-[var(--color-text)]/80">{ticket.ErrorMessage}</div>
            </div>
          {/if}
          {#each projectState.ticketTasks as task (task.ID)}
            <TaskCard
              {task}
              events={projectState.ticketEvents}
              llmCalls={projectState.ticketLlmCalls}
            />
          {/each}
          {#if projectState.ticketTasks.length === 0}
            <div class="text-center text-[var(--color-muted)] text-xs py-8">No tasks yet</div>
          {/if}
        </div>

      {:else if activeTab === 'events'}
        <div class="divide-y divide-[var(--color-border)]">
          {#each projectState.ticketEvents as evt (evt.ID)}
            <div class="px-4 py-2.5 flex gap-3 items-start hover:bg-[var(--color-surface-hover)]">
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
                {#if evt.Details}
                  <div class="text-[10px] text-[var(--color-muted)] mt-0.5 truncate" title={evt.Details}>
                    {evt.Details}
                  </div>
                {/if}
              </div>
              <span class="shrink-0 text-[10px] text-[var(--color-muted)] whitespace-nowrap">
                {formatRelative(evt.CreatedAt)}
              </span>
            </div>
          {/each}
          {#if projectState.ticketEvents.length === 0}
            <div class="text-center text-[var(--color-muted)] text-xs py-8">No events yet</div>
          {/if}
        </div>

      {:else if activeTab === 'chat'}
        <div class="flex flex-col h-full">
          <div class="flex-1 overflow-y-auto divide-y divide-[var(--color-border)]">
            {#each projectState.chatMessages as msg (msg.id)}
              <div class="px-4 py-3"
                   class:bg-[var(--color-accent-bg)]={msg.sender === 'user'}>
                <div class="flex items-center gap-2 mb-1">
                  <span class="text-[10px] font-bold tracking-wider uppercase"
                        class:text-[var(--color-accent)]={msg.sender === 'user'}
                        class:text-[var(--color-muted-bright)]={msg.sender !== 'user'}>
                    {msg.sender}
                  </span>
                  <span class="text-[10px] text-[var(--color-muted)]">
                    {formatRelative(msg.created_at)}
                  </span>
                </div>
                <div class="text-xs text-[var(--color-text)] whitespace-pre-wrap leading-relaxed">
                  {msg.content}
                </div>
              </div>
            {/each}
            {#if projectState.chatMessages.length === 0}
              <div class="text-center text-[var(--color-muted)] text-xs py-8">No messages</div>
            {/if}
          </div>

          {#if ticket.Status === 'clarification_needed'}
            <div class="border-t border-[var(--color-border)] p-3 shrink-0">
              <div class="text-[10px] text-[var(--color-warning)] tracking-wider uppercase mb-2">
                Agent is waiting for your input
              </div>
              <textarea
                bind:value={chatInput}
                rows="3"
                placeholder="Type your reply..."
                class="w-full bg-[var(--color-surface)] border border-[var(--color-border)] px-3 py-2 text-xs
                       text-[var(--color-text)] focus:border-[var(--color-accent)] focus:outline-none resize-none"
                onkeydown={(e) => { if (e.key === 'Enter' && (e.ctrlKey || e.metaKey)) sendChat(); }}
              ></textarea>
              <div class="flex justify-between items-center mt-2">
                <span class="text-[10px] text-[var(--color-muted)]">Ctrl+Enter to send</span>
                <button
                  onclick={sendChat}
                  disabled={sendingChat || !chatInput.trim()}
                  class="px-4 py-1.5 bg-[var(--color-accent)] text-[var(--color-bg)] text-[10px] font-bold
                         tracking-widest disabled:opacity-40 hover:opacity-90 transition-opacity"
                >
                  {sendingChat ? 'SENDING...' : 'SEND'}
                </button>
              </div>
            </div>
          {/if}
        </div>
      {/if}
    </div>

    <!-- Actions footer -->
    <div class="border-t border-[var(--color-border)] px-4 py-3 flex gap-2 shrink-0">
      {#if ['failed', 'blocked', 'partial'].includes(ticket.Status)}
        <button
          onclick={() => projectState.retryTicket(ticket.ID)}
          class="text-[10px] px-3 py-1.5 bg-[var(--color-accent)] text-[var(--color-bg)] font-bold tracking-wider hover:opacity-90"
        >↺ RETRY</button>
      {/if}
      <button
        onclick={() => showDeleteConfirm = true}
        class="text-[10px] px-3 py-1.5 border border-[var(--color-danger)] text-[var(--color-danger)] hover:bg-[var(--color-danger-bg)] tracking-wider ml-auto"
      >DELETE</button>
    </div>
  </div>

  <ConfirmDialog
    open={showDeleteConfirm}
    title="Delete Ticket"
    message="Permanently delete this ticket and all its data?"
    confirmLabel="DELETE"
    onconfirm={handleDelete}
    oncancel={() => showDeleteConfirm = false}
  />
{/if}
