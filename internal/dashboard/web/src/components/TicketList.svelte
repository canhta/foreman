<script lang="ts">
  import {
    appState, selectTicket, setFilter, setSearch,
  } from '../state.svelte';
  import { ACTIVE_STATUSES, DONE_STATUSES, FAIL_STATUSES } from '../types';
  import type { TicketSummary } from '../types';
  import { formatSender, formatRelative, formatCost } from '../format';

  let focusIndex = $state(-1);

  let filteredTickets = $derived.by(() => {
    let list = appState.tickets;
    if (appState.filter === 'active') list = list.filter(t => ACTIVE_STATUSES.includes(t.Status));
    else if (appState.filter === 'done') list = list.filter(t => DONE_STATUSES.includes(t.Status));
    else if (appState.filter === 'fail') list = list.filter(t => FAIL_STATUSES.includes(t.Status));

    if (appState.search) {
      const q = appState.search.toLowerCase();
      list = list.filter(t =>
        t.Title?.toLowerCase().includes(q) ||
        t.ChannelSenderID?.toLowerCase().includes(q)
      );
    }

    return list.toSorted((a, b) => {
      const aFail = FAIL_STATUSES.includes(a.Status) ? 0 : 1;
      const bFail = FAIL_STATUSES.includes(b.Status) ? 0 : 1;
      if (aFail !== bFail) return aFail - bFail;
      return new Date(b.UpdatedAt).getTime() - new Date(a.UpdatedAt).getTime();
    });
  });

  function countByFilter(f: string): number {
    if (f === 'active') return appState.tickets.filter(t => ACTIVE_STATUSES.includes(t.Status)).length;
    if (f === 'done') return appState.tickets.filter(t => DONE_STATUSES.includes(t.Status)).length;
    if (f === 'fail') return appState.tickets.filter(t => FAIL_STATUSES.includes(t.Status)).length;
    return appState.tickets.length;
  }

  function statusClass(status: string): string {
    if (FAIL_STATUSES.includes(status as any)) return 'text-danger';
    if (ACTIVE_STATUSES.includes(status as any)) return 'text-accent';
    if (DONE_STATUSES.includes(status as any)) return 'text-success';
    return 'text-muted';
  }

  function handleKeydown(e: KeyboardEvent) {
    if (e.key === 'j') { focusIndex = Math.min(focusIndex + 1, filteredTickets.length - 1); e.preventDefault(); }
    if (e.key === 'k') { focusIndex = Math.max(focusIndex - 1, 0); e.preventDefault(); }
    if (e.key === 'Enter' && focusIndex >= 0) { selectTicket(filteredTickets[focusIndex].ID); e.preventDefault(); }
    if (e.key === '1') { setFilter('all'); e.preventDefault(); }
    if (e.key === '2') { setFilter('active'); e.preventDefault(); }
    if (e.key === '3') { setFilter('done'); e.preventDefault(); }
    if (e.key === '4') { setFilter('fail'); e.preventDefault(); }
  }
</script>

<svelte:window onkeydown={handleKeydown} />

<section class="flex flex-col h-full border-r border-border bg-surface">
  <div class="px-3 py-2 border-b border-border text-xs text-muted font-bold tracking-wider">
    TICKETS ({filteredTickets.length})
  </div>

  <div class="px-3 py-2 border-b border-border space-y-2">
    <input
      type="text"
      value={appState.search}
      oninput={(e) => setSearch((e.target as HTMLInputElement).value)}
      placeholder="Search tickets..."
      class="w-full bg-bg border border-border px-2 py-1 text-xs text-text placeholder:text-muted focus:border-accent outline-none"
    />
    <div class="flex gap-1">
      {#each [['all', 'ALL'], ['active', 'ACT'], ['done', 'DONE'], ['fail', 'FAIL']] as [key, label]}
        <button
          class="flex-1 text-xs py-1 border {appState.filter === key ? 'border-accent text-accent' : 'border-border text-muted hover:text-text'}"
          onclick={() => setFilter(key as 'all' | 'active' | 'done' | 'fail')}
        >{label} {countByFilter(key)}</button>
      {/each}
    </div>
  </div>

  <div class="flex-1 overflow-y-auto">
    {#each filteredTickets as t, i}
      <button
        class="w-full text-left px-3 py-2 border-b border-border hover:bg-surface-hover cursor-pointer
          {appState.selectedTicketId === t.ID ? 'bg-surface-hover border-l-2 border-l-accent' : ''}
          {focusIndex === i ? 'ring-1 ring-accent ring-inset' : ''}"
        onclick={() => selectTicket(t.ID)}
      >
        <div class="text-sm text-text truncate">{t.Title || t.ID}</div>
        <div class="flex items-center gap-2 mt-1 text-xs">
          <span class={statusClass(t.Status)}>{t.Status.toUpperCase()}</span>
          <span class="text-muted">{formatSender(t.ChannelSenderID)}</span>
        </div>
        {#if t.tasks_total > 0}
          <div class="flex items-center gap-2 mt-1">
            <div class="flex-1 h-1 bg-border rounded overflow-hidden">
              <div class="h-full bg-accent" style="width:{(t.tasks_done / t.tasks_total) * 100}%"></div>
            </div>
            <span class="text-xs text-muted">{formatCost(t.CostUSD)} {t.tasks_done}/{t.tasks_total}</span>
          </div>
        {/if}
      </button>
    {/each}
  </div>
</section>
