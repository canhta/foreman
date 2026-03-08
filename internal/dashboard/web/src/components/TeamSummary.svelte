<script lang="ts">
  import {
    appState, selectTicket,
  } from '../state.svelte';
  import { ACTIVE_STATUSES, DONE_STATUSES, FAIL_STATUSES } from '../types';
  import { formatSender, formatRelative, formatCost } from '../format';

  const STUCK_THRESHOLD_MS = 30 * 60 * 1000;

  let needsAttention = $derived(
    appState.tickets.filter(t => {
      if (FAIL_STATUSES.includes(t.Status)) return true;
      if (t.Status === 'clarification_needed') return true;
      if (ACTIVE_STATUSES.includes(t.Status) && t.UpdatedAt) {
        return Date.now() - new Date(t.UpdatedAt).getTime() > STUCK_THRESHOLD_MS;
      }
      return false;
    })
  );

  let todayStr = new Date().toISOString().slice(0, 10);
  let todayTickets = $derived(appState.tickets.filter(t => t.CreatedAt?.slice(0, 10) === todayStr));
  let todayActive = $derived(todayTickets.filter(t => ACTIVE_STATUSES.includes(t.Status)).length);
  let todayDone = $derived(todayTickets.filter(t => DONE_STATUSES.includes(t.Status)).length);
  let todayFailed = $derived(todayTickets.filter(t => FAIL_STATUSES.includes(t.Status)).length);

  let maxWeekCost = $derived(Math.max(0.01, ...appState.weekDays.map(d => d.cost_usd || 0)));

  function dayLabel(dateStr: string): string {
    return ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'][new Date(dateStr).getDay()];
  }

  function isToday(dateStr: string): boolean {
    return dateStr === todayStr;
  }
</script>

<div class="flex-1 overflow-y-auto">
  <!-- Header -->
  <div class="px-4 py-2 border-b-2 border-border bg-surface sticky top-0">
    <span class="text-xs font-bold tracking-[0.2em] text-muted-bright">OPERATIONS CENTER</span>
  </div>

  <div class="p-4 space-y-4">
    <!-- Needs Attention — shown prominently when there are issues -->
    {#if needsAttention.length > 0}
      <section class="border-2 border-danger/60">
        <div class="px-3 py-1.5 border-b border-danger/30 bg-danger-bg flex items-center justify-between">
          <span class="text-[10px] font-bold tracking-[0.2em] text-danger">⚠ NEEDS ATTENTION</span>
          <span class="text-danger text-xs font-bold">{needsAttention.length}</span>
        </div>
        <div>
          {#each needsAttention as t}
            <button
              class="w-full text-left px-3 py-2 border-b border-border last:border-b-0 hover:bg-danger-bg transition-colors cursor-pointer"
              onclick={() => selectTicket(t.ID)}
            >
              <div class="flex items-start gap-2">
                <span class="text-danger shrink-0 mt-0.5">
                  {#if FAIL_STATUSES.includes(t.Status)}✕{:else}?{/if}
                </span>
                <div class="flex-1 min-w-0">
                  <div class="text-xs text-text truncate">{t.Title}</div>
                  <div class="text-[10px] text-muted mt-0.5">
                    {t.Status.toUpperCase()} · {formatSender(t.ChannelSenderID)} · {formatRelative(t.UpdatedAt)}
                  </div>
                </div>
              </div>
            </button>
          {/each}
        </div>
      </section>
    {/if}

    <!-- Today's stats -->
    <section class="border-2 border-border">
      <div class="px-3 py-1.5 border-b border-border bg-surface-active flex items-center justify-between">
        <span class="text-[10px] font-bold tracking-[0.2em] text-muted-bright">TODAY</span>
        <span class="text-[10px] text-muted">{todayStr}</span>
      </div>
      <div class="grid grid-cols-4 gap-0">
        <div class="p-3 border-r border-border">
          <div class="text-[10px] text-muted tracking-wider mb-1">TOTAL</div>
          <div class="text-2xl font-bold text-text tabular-nums">{todayTickets.length}</div>
        </div>
        <div class="p-3 border-r border-border">
          <div class="text-[10px] text-muted tracking-wider mb-1">ACTIVE</div>
          <div class="text-2xl font-bold {todayActive > 0 ? 'text-accent' : 'text-muted'} tabular-nums">{todayActive}</div>
        </div>
        <div class="p-3 border-r border-border">
          <div class="text-[10px] text-muted tracking-wider mb-1">MERGED</div>
          <div class="text-2xl font-bold {todayDone > 0 ? 'text-success' : 'text-muted'} tabular-nums">{todayDone}</div>
        </div>
        <div class="p-3">
          <div class="text-[10px] text-muted tracking-wider mb-1">FAILED</div>
          <div class="text-2xl font-bold {todayFailed > 0 ? 'text-danger' : 'text-muted'} tabular-nums">{todayFailed}</div>
        </div>
      </div>
      <!-- Cost row -->
      <div class="px-3 pb-2.5 pt-1 border-t border-border grid grid-cols-2 gap-0 text-xs">
        <div class="text-muted">Daily: <span class="text-text">{formatCost(appState.dailyCost)}</span>
          {#if appState.dailyBudget > 0}<span class="text-muted"> / ${Math.round(appState.dailyBudget)}</span>{/if}
        </div>
        <div class="text-muted">Monthly: <span class="text-text">{formatCost(appState.monthlyCost)}</span>
          {#if appState.monthlyBudget > 0}<span class="text-muted"> / ${Math.round(appState.monthlyBudget)}</span>{/if}
        </div>
      </div>
    </section>

    <!-- This week cost chart -->
    <section class="border-2 border-border">
      <div class="px-3 py-1.5 border-b border-border bg-surface-active flex items-center justify-between">
        <span class="text-[10px] font-bold tracking-[0.2em] text-muted-bright">THIS WEEK</span>
        <span class="text-[10px] text-accent font-bold">{formatCost(appState.weeklyCost)}</span>
      </div>
      <div class="p-3 space-y-1.5">
        {#if appState.weekDays.length === 0}
          <div class="text-xs text-muted py-2">No cost data yet.</div>
        {:else}
          {#each appState.weekDays as day}
            {@const pct = (day.cost_usd || 0) / maxWeekCost * 100}
            {@const today = isToday(day.date)}
            <div class="flex items-center gap-2 text-[10px]">
              <span class="w-7 shrink-0 {today ? 'text-accent font-bold' : 'text-muted'}">{dayLabel(day.date)}</span>
              <div class="flex-1 h-3 bg-border overflow-hidden">
                <div
                  class="h-full {today ? 'bg-accent' : 'bg-border-bright'} transition-all"
                  style="width:{pct}%"
                ></div>
              </div>
              <span class="w-12 text-right {today ? 'text-accent' : 'text-muted'} tabular-nums">{formatCost(day.cost_usd || 0)}</span>
            </div>
          {/each}
        {/if}
      </div>
    </section>

    <!-- Team -->
    {#if appState.teamStats.length > 0}
      <section class="border-2 border-border">
        <div class="px-3 py-1.5 border-b border-border bg-surface-active flex items-center justify-between">
          <span class="text-[10px] font-bold tracking-[0.2em] text-muted-bright">TEAM</span>
          <span class="text-[10px] text-muted">{appState.teamStats.length} submitters</span>
        </div>
        <div>
          {#each appState.teamStats as stat}
            <div class="flex items-center gap-2 px-3 py-2 border-b border-border last:border-b-0 text-xs">
              <span class="flex-1 text-text truncate">{formatSender(stat.channel_sender_id)}</span>
              <span class="text-muted">{stat.ticket_count} tickets</span>
              <span class="text-muted-bright">{formatCost(stat.cost_usd)}</span>
              {#if stat.failed_count > 0}
                <span class="text-[10px] border border-danger/40 text-danger px-1">✕{stat.failed_count}</span>
              {/if}
            </div>
          {/each}
        </div>
      </section>
    {/if}

    <!-- Recent PRs -->
    {#if appState.recentPRs.length > 0}
      <section class="border-2 border-border">
        <div class="px-3 py-1.5 border-b border-border bg-surface-active">
          <span class="text-[10px] font-bold tracking-[0.2em] text-muted-bright">RECENT PRS</span>
        </div>
        <div>
          {#each appState.recentPRs as pr}
            <div class="px-3 py-2 border-b border-border last:border-b-0">
              <div class="flex items-start gap-2">
                <span class="text-success text-xs shrink-0">✓</span>
                <div class="flex-1 min-w-0">
                  <a
                    href={pr.PRURL}
                    target="_blank"
                    class="text-xs text-accent hover:underline truncate block"
                  >{pr.PRNumber ? `#${pr.PRNumber} ` : ''}{pr.Title}</a>
                  <div class="text-[10px] text-muted mt-0.5">{formatRelative(pr.UpdatedAt)}</div>
                </div>
              </div>
            </div>
          {/each}
        </div>
      </section>
    {/if}
  </div>
</div>
