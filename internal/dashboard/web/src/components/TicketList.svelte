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

  function statusBadge(status: string): { text: string; cls: string } {
    if (FAIL_STATUSES.includes(status as any)) return { text: status.toUpperCase(), cls: 'text-danger border-danger/40' };
    if (ACTIVE_STATUSES.includes(status as any)) return { text: status.toUpperCase(), cls: 'text-accent border-accent/40' };
    if (DONE_STATUSES.includes(status as any)) return { text: status.toUpperCase(), cls: 'text-success border-success/40' };
    return { text: status.toUpperCase(), cls: 'text-muted border-border-strong' };
  }

  function isActive(t: TicketSummary): boolean {
    return ACTIVE_STATUSES.includes(t.Status);
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

<section class="flex flex-col h-full bg-surface w-full">
  <!-- Header -->
  <div class="px-3 py-2 border-b-2 border-border flex items-center justify-between">
    <span class="text-xs font-bold tracking-[0.2em] text-muted-bright">TICKETS</span>
    <span class="text-xs text-muted bg-surface-active px-2 py-0.5 border border-border">{filteredTickets.length}</span>
  </div>

  <!-- Search -->
  <div class="px-3 pt-2 pb-1 border-b border-border">
    <input
      type="text"
      value={appState.search}
      oninput={(e) => setSearch((e.target as HTMLInputElement).value)}
      placeholder="search..."
      class="w-full bg-bg border border-border px-2 py-1.5 text-xs text-text placeholder:text-muted
             focus:border-accent focus:outline-none transition-colors"
    />
  </div>

  <!-- Filter tabs -->
  <div class="flex border-b-2 border-border">
    {#each [['all', 'ALL'], ['active', 'ACT'], ['done', 'DONE'], ['fail', 'FAIL']] as [key, lbl]}
      {@const n = countByFilter(key)}
      <button
        class="flex-1 text-xs py-1.5 border-r border-border last:border-r-0 transition-colors
          {appState.filter === key
            ? 'bg-accent text-bg font-bold'
            : 'text-muted hover:text-text hover:bg-surface-hover'}"
        onclick={() => setFilter(key as 'all' | 'active' | 'done' | 'fail')}
      >
        <div class="tracking-wider">{lbl}</div>
        {#if n > 0}
          <div class="text-[10px] opacity-70">{n}</div>
        {/if}
      </button>
    {/each}
  </div>

  <!-- List -->
  <div class="flex-1 overflow-y-auto">
    {#each filteredTickets as t, i (t.ID)}
      {@const badge = statusBadge(t.Status)}
      {@const selected = appState.selectedTicketId === t.ID}
      <button
        class="w-full text-left px-3 py-2.5 border-b border-border transition-colors cursor-pointer
          {selected ? 'bg-accent-bg border-l-4 border-l-accent pl-2' : 'border-l-4 border-l-transparent hover:bg-surface-hover'}
          {focusIndex === i ? 'ring-1 ring-inset ring-accent/40' : ''}"
        onclick={() => selectTicket(t.ID)}
      >
        <!-- Title -->
        <div class="text-xs text-text truncate leading-tight {selected ? 'font-bold' : ''}"
          title={t.Title || t.ID}>
          {t.Title || t.ID}
        </div>

        <!-- Status + sender -->
        <div class="flex items-center gap-1.5 mt-1.5">
          <span class="text-[10px] border px-1 py-0.5 leading-none tracking-wider {badge.cls}">
            {badge.text}
          </span>
          {#if isActive(t)}
            <span class="w-1 h-1 bg-accent animate-pulse"></span>
          {/if}
          <span class="text-[10px] text-muted ml-auto">{formatRelative(t.UpdatedAt)}</span>
        </div>

        <!-- Progress bar -->
        {#if t.tasks_total > 0}
          <div class="mt-1.5 space-y-0.5">
            <div class="flex justify-between text-[10px] text-muted">
              <span>{formatSender(t.ChannelSenderID)}</span>
              <span>{t.tasks_done}/{t.tasks_total} · {formatCost(t.CostUSD)}</span>
            </div>
            <div class="h-0.5 bg-border overflow-hidden">
              <div
                class="h-full {t.tasks_done === t.tasks_total ? 'bg-success' : 'bg-accent'} transition-all"
                style="width:{(t.tasks_done / t.tasks_total) * 100}%"
              ></div>
            </div>
          </div>
        {:else}
          <div class="text-[10px] text-muted mt-1">{formatSender(t.ChannelSenderID)}</div>
        {/if}
      </button>
    {/each}

    {#if filteredTickets.length === 0}
      <div class="px-3 py-8 text-center">
        <div class="text-muted text-xs tracking-wider mb-1">NO TICKETS</div>
        <div class="text-muted/50 text-[10px]">{appState.filter !== 'all' ? 'Try a different filter' : 'Waiting for work...'}</div>
      </div>
    {/if}
  </div>
</section>
