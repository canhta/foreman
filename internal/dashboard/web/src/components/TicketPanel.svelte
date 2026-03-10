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
    return status.replace(/_/g, ' ');
  }

  function statusChipCls(status: string): string {
    if (['done', 'merged'].includes(status))              return 'status-chip status-chip-done';
    if (['failed', 'blocked', 'partial'].includes(status)) return 'status-chip status-chip-failed';
    if (status.includes('clarification'))                  return 'status-chip status-chip-warn';
    return 'status-chip status-chip-active';
  }

  function severityColor(sev: string): string {
    if (sev === 'success') return 'text-[var(--color-success)]';
    if (sev === 'error')   return 'text-[var(--color-danger)]';
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

  const taskCount   = $derived(projectState.ticketTasks.length);
  const eventCount  = $derived(projectState.ticketEvents.length);
  const chatCount   = $derived(projectState.chatMessages.length);
  const doneTasks   = $derived(projectState.ticketTasks.filter(t => t.Status === 'done').length);
  const totalTasks  = $derived(projectState.ticketTasks.length);
  const progressPct = $derived(totalTasks > 0 ? (doneTasks / totalTasks) * 100 : 0);
</script>

{#if ticket}
  <div class="flex flex-col h-full animate-[slide-in-right_0.2s_ease-out]">

    <!-- Header -->
    <div class="px-4 pt-4 pb-3 border-b border-[var(--color-border)] shrink-0 bg-[var(--color-surface)]">
      <!-- Top row: ID / status + actions -->
      <div class="flex items-start justify-between gap-2 mb-2">
        <div class="flex items-center gap-2 flex-wrap min-w-0">
          <span class="text-[10px] text-[var(--color-muted)] tracking-wider font-mono shrink-0">
            {ticket.ExternalID || ticket.ID.slice(0, 8)}
          </span>
          <span class="{statusChipCls(ticket.Status)} capitalize">
            {statusLabel(ticket.Status)}
          </span>
        </div>
        <div class="flex items-center gap-1 shrink-0">
          <button
            onclick={() => projectState.expandPanel()}
            class="text-[10px] text-[var(--color-muted)] hover:text-[var(--color-text)]
                   px-2 py-1.5 border border-[var(--color-border)] hover:border-[var(--color-border-strong)]
                   transition-colors leading-none"
            title="Expand to full view"
          >⤢</button>
          <button
            onclick={() => projectState.deselectTicket()}
            class="text-[var(--color-muted)] hover:text-[var(--color-text)]
                   px-2 py-1.5 border border-[var(--color-border)] hover:border-[var(--color-border-strong)]
                   transition-colors text-sm leading-none"
          >✕</button>
        </div>
      </div>

      <!-- Title -->
      <h2 class="text-sm font-bold leading-snug text-[var(--color-text)] mb-2">{ticket.Title}</h2>

      <!-- Progress bar -->
      {#if totalTasks > 0}
        <div class="mb-2">
          <div class="h-[3px] bg-[var(--color-border-strong)] overflow-hidden mb-1">
            <div class="h-full bg-[var(--color-accent)] transition-all duration-500"
                 style="width: {progressPct}%"></div>
          </div>
          <div class="flex items-center justify-between text-[10px] text-[var(--color-muted)]">
            <span>{doneTasks}/{totalTasks} tasks complete</span>
            <span>{Math.round(progressPct)}%</span>
          </div>
        </div>
      {/if}

      <!-- Meta row -->
      <div class="flex items-center gap-4 text-[10px] text-[var(--color-muted)] flex-wrap">
        <span>
          <span class="text-[var(--color-muted)]">Cost</span>
          <span class="text-[var(--color-text)] ml-1 tabular-nums">{formatCost(ticket.CostUSD ?? 0)}</span>
        </span>
        {#if hasPR && ticket.PRURL}
          <a href={ticket.PRURL} target="_blank" rel="noopener"
             class="text-[var(--color-accent)] hover:opacity-80 transition-opacity uppercase tracking-wider">
            PR #{ticket.PRNumber} ↗
          </a>
        {/if}
        {#if ticket.ChannelSenderID}
          <span class="truncate max-w-[120px]" title={ticket.ChannelSenderID}>
            {ticket.ChannelSenderID.split('@')[0]}
          </span>
        {/if}
      </div>
    </div>

    <!-- Tabs -->
    <div class="flex border-b border-[var(--color-border)] shrink-0 bg-[var(--color-surface)]">
      {#each (['tasks', 'events', 'chat'] as Tab[]) as tab}
        {@const count = tab === 'tasks' ? taskCount : tab === 'events' ? eventCount : chatCount}
        <button
          onclick={() => activeTab = tab}
          class="flex-1 px-3 py-2.5 text-[10px] font-bold tracking-[0.15em] uppercase border-b-2 transition-colors"
          class:border-[var(--color-accent)]={activeTab === tab}
          class:text-[var(--color-accent)]={activeTab === tab}
          class:border-transparent={activeTab !== tab}
          class:text-[var(--color-muted)]={activeTab !== tab}
          class:hover:text-[var(--color-muted-bright)]={activeTab !== tab}
        >
          {tab}
          {#if count > 0}
            <span class="ml-1.5 text-[9px] opacity-70">{count}</span>
          {/if}
        </button>
      {/each}
    </div>

    <!-- Tab content -->
    <div class="flex-1 overflow-y-auto bg-[var(--color-bg)]">

      {#if activeTab === 'tasks'}
        <div class="p-3 space-y-2">
          {#if ticket.Description}
            <div class="p-3 bg-[var(--color-surface)] border border-[var(--color-border)]">
              <div class="text-[10px] tracking-[0.15em] text-[var(--color-muted)] uppercase mb-1.5">Description</div>
              <div class="text-xs text-[var(--color-muted-bright)] whitespace-pre-wrap leading-relaxed">
                {ticket.Description}
              </div>
            </div>
          {/if}

          {#if ticket.ErrorMessage}
            <div class="border-l-2 border-l-[var(--color-danger)] bg-[var(--color-danger-bg)] p-3">
              <div class="text-[10px] font-bold tracking-[0.15em] text-[var(--color-danger)] uppercase mb-1">Error</div>
              <div class="text-xs text-[var(--color-text)]/80 leading-relaxed">{ticket.ErrorMessage}</div>
            </div>
          {/if}

          {#each projectState.ticketTasks as task (task.ID)}
            <TaskCard {task} events={projectState.ticketEvents} llmCalls={projectState.ticketLlmCalls} />
          {/each}

          {#if projectState.ticketTasks.length === 0}
            <div class="text-center text-[var(--color-muted)] text-xs py-10">No tasks yet</div>
          {/if}

          <!-- Recent chat preview -->
          {#if projectState.chatMessages.length > 0}
            <div class="mt-4 pt-4 border-t border-[var(--color-border)]">
              <div class="text-[10px] tracking-[0.15em] text-[var(--color-muted)] uppercase mb-2">Recent Chat</div>
              {#each projectState.chatMessages.slice(-3) as msg (msg.id)}
                <div class="text-xs mb-2 flex gap-2">
                  <span class="shrink-0 text-[10px] font-bold tracking-wider"
                        class:text-[var(--color-accent)]={msg.sender === 'agent'}
                        class:text-[var(--color-success)]={msg.sender === 'user'}
                        class:text-[var(--color-muted)]={msg.sender === 'system'}>
                    {msg.sender === 'agent' ? 'AGENT' : msg.sender === 'user' ? 'YOU' : 'SYS'}
                  </span>
                  <span class="text-[var(--color-muted-bright)] truncate text-[10px]">
                    {msg.content.length > 80 ? msg.content.slice(0, 80) + '…' : msg.content}
                  </span>
                </div>
              {/each}
              <button onclick={() => activeTab = 'chat'}
                      class="text-[10px] text-[var(--color-accent)] hover:opacity-80 transition-opacity">
                {chatCount > 3 ? `View all ${chatCount} messages →` : 'Open chat →'}
              </button>
            </div>
          {/if}
        </div>

      {:else if activeTab === 'events'}
        <div class="divide-y divide-[var(--color-border)]">
          {#each projectState.ticketEvents as evt (evt.ID)}
            <div class="px-4 py-2.5 flex gap-3 items-start hover:bg-[var(--color-surface-hover)] transition-colors">
              <span class="shrink-0 text-xs {severityColor(evt.Severity)} mt-0.5 leading-none w-3 text-center">
                {severityIcon(evt.Severity)}
              </span>
              <div class="min-w-0 flex-1">
                <div class="text-xs text-[var(--color-text)] leading-snug">
                  {#each linkifyParts(evt.Message || evt.EventType) as part}
                    {#if part.type === 'url'}
                      <a href={part.content} target="_blank" rel="noopener"
                         class="text-[var(--color-accent)] hover:opacity-80 break-all">{part.content}</a>
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
            <div class="text-center text-[var(--color-muted)] text-xs py-10">No events yet</div>
          {/if}
        </div>

      {:else if activeTab === 'chat'}
        <div class="flex flex-col h-full">
          <div class="flex-1 overflow-y-auto divide-y divide-[var(--color-border)]">
            {#each projectState.chatMessages as msg (msg.id)}
              <div class="px-4 py-3 transition-colors"
                   class:bg-[var(--color-accent-bg)]={msg.sender === 'user'}>
                <div class="flex items-center gap-2 mb-1.5">
                  <span class="text-[10px] font-bold tracking-[0.15em] uppercase"
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
              <div class="text-center text-[var(--color-muted)] text-xs py-10">No messages</div>
            {/if}
          </div>

          {#if ticket.Status === 'clarification_needed'}
            <div class="border-t-2 border-t-[var(--color-warning)] border-[var(--color-border)] p-3 shrink-0 bg-[var(--color-surface)]">
              <div class="text-[10px] text-[var(--color-warning)] tracking-[0.15em] uppercase mb-2 font-bold">
                Agent waiting for input
              </div>
              <textarea
                bind:value={chatInput}
                rows="3"
                placeholder="Type your reply..."
                class="w-full bg-[var(--color-bg)] border border-[var(--color-border)] px-3 py-2 text-xs
                       text-[var(--color-text)] placeholder-[var(--color-muted)]
                       focus:border-[var(--color-warning)] focus:outline-none resize-none leading-relaxed"
                onkeydown={(e) => { if (e.key === 'Enter' && (e.ctrlKey || e.metaKey)) sendChat(); }}
              ></textarea>
              <div class="flex justify-between items-center mt-2">
                <span class="text-[10px] text-[var(--color-muted)]">Ctrl+Enter to send</span>
                <button
                  onclick={sendChat}
                  disabled={sendingChat || !chatInput.trim()}
                  class="px-4 py-1.5 bg-[var(--color-warning)] text-[var(--color-bg)] text-[10px] font-bold
                         tracking-widest uppercase disabled:opacity-40 hover:opacity-90 transition-opacity"
                >
                  {sendingChat ? 'Sending…' : 'Send'}
                </button>
              </div>
            </div>
          {/if}
        </div>
      {/if}
    </div>

    <!-- Actions footer -->
    <div class="border-t border-[var(--color-border)] px-4 py-3 flex items-center gap-2 shrink-0 bg-[var(--color-surface)]">
      {#if ['failed', 'blocked', 'partial'].includes(ticket.Status)}
        <button
          onclick={() => projectState.retryTicket(ticket.ID)}
          class="text-[10px] px-3 py-1.5 bg-[var(--color-accent)] text-[var(--color-bg)] font-bold tracking-widest uppercase hover:opacity-90 transition-opacity"
        >↺ Retry</button>
      {/if}
      <button
        onclick={() => showDeleteConfirm = true}
        class="text-[10px] px-3 py-1.5 border border-[var(--color-border-strong)] text-[var(--color-muted)]
               hover:border-[var(--color-danger)] hover:text-[var(--color-danger)]
               hover:bg-[var(--color-danger-bg)] tracking-widest uppercase transition-colors ml-auto"
      >Delete</button>
    </div>
  </div>

  <ConfirmDialog
    open={showDeleteConfirm}
    title="Delete Ticket"
    message="Permanently delete this ticket and all its data?"
    confirmLabel="Delete"
    onconfirm={handleDelete}
    oncancel={() => showDeleteConfirm = false}
  />
{/if}
