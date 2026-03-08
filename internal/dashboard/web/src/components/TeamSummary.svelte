<script lang="ts">
  import {
    tickets, teamStats, recentPRs, weekDays,
    dailyCost, dailyBudget, monthlyCost, monthlyBudget, weeklyCost,
    selectTicket,
  } from '../state.svelte';
  import { ACTIVE_STATUSES, DONE_STATUSES, FAIL_STATUSES } from '../types';
  import { formatSender, formatRelative, formatCost } from '../format';

  const STUCK_THRESHOLD_MS = 30 * 60 * 1000;

  let needsAttention = $derived(
    tickets.filter(t => {
      if (FAIL_STATUSES.includes(t.Status)) return true;
      if (t.Status === 'clarification_needed') return true;
      if (ACTIVE_STATUSES.includes(t.Status) && t.UpdatedAt) {
        return Date.now() - new Date(t.UpdatedAt).getTime() > STUCK_THRESHOLD_MS;
      }
      return false;
    })
  );

  let todayStr = new Date().toISOString().slice(0, 10);
  let todayTickets = $derived(tickets.filter(t => t.CreatedAt?.slice(0, 10) === todayStr));
  let todayActive = $derived(todayTickets.filter(t => ACTIVE_STATUSES.includes(t.Status)).length);
  let todayDone = $derived(todayTickets.filter(t => DONE_STATUSES.includes(t.Status)).length);
  let todayFailed = $derived(todayTickets.filter(t => FAIL_STATUSES.includes(t.Status)).length);

  let maxWeekCost = $derived(Math.max(1, ...weekDays.map(d => d.cost_usd || 0)));

  function barWidth(cost: number): string {
    const chars = Math.round((cost / maxWeekCost) * 16);
    return '\u2588'.repeat(chars) || '\u00B7';
  }

  function dayLabel(dateStr: string): string {
    return ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'][new Date(dateStr).getDay()];
  }
</script>

<div class="p-4 space-y-4 overflow-y-auto h-full">
  <!-- Needs Attention -->
  {#if needsAttention.length > 0}
    <section>
      <div class="flex justify-between text-xs text-muted font-bold tracking-wider mb-2">
        <span>&#9888; NEEDS ATTENTION</span>
        <span>{needsAttention.length}</span>
      </div>
      {#each needsAttention as t}
        <button
          class="w-full text-left px-2 py-1 text-xs hover:bg-surface-hover flex justify-between cursor-pointer"
          onclick={() => selectTicket(t.ID)}
        >
          <span>
            {#if FAIL_STATUSES.includes(t.Status)}<span class="text-danger">&#10007; </span>{/if}
            {#if t.Status === 'clarification_needed'}<span class="text-warning">&#10067; </span>{/if}
            {t.Title}
          </span>
          <span class="text-muted">{t.Status} &middot; {formatSender(t.ChannelSenderID)}</span>
        </button>
      {/each}
    </section>
  {/if}

  <!-- Today -->
  <section>
    <div class="flex justify-between text-xs text-muted font-bold tracking-wider mb-2">
      <span>TODAY</span>
      <span>{todayStr}</span>
    </div>
    <div class="grid grid-cols-4 gap-2 text-xs">
      <div><span class="text-muted">Tickets</span><br>{todayTickets.length}</div>
      <div><span class="text-muted">Active</span><br>{todayActive}</div>
      <div><span class="text-muted">Merged</span><br><span class="text-success">&#10003; {todayDone}</span></div>
      <div><span class="text-muted">Failed</span><br>
        <span class="{todayFailed > 0 ? 'text-danger' : 'text-muted'}">{todayFailed > 0 ? `\u2717 ${todayFailed}` : '--'}</span>
      </div>
    </div>
    <div class="text-xs text-muted mt-2 space-y-0.5">
      <div>Daily:   {formatCost(dailyCost)} / ${Math.round(dailyBudget)}</div>
      <div>Monthly: {formatCost(monthlyCost)} / ${Math.round(monthlyBudget)}</div>
    </div>
  </section>

  <!-- This Week -->
  <section>
    <div class="flex justify-between text-xs text-muted font-bold tracking-wider mb-2">
      <span>THIS WEEK</span>
      <span>{formatCost(weeklyCost)}</span>
    </div>
    {#if weekDays.length === 0}
      <div class="text-xs text-muted">No cost data yet.</div>
    {:else}
      {#each weekDays as day}
        <div class="flex gap-2 text-xs items-center">
          <span class="w-8 text-muted">{dayLabel(day.date)}</span>
          <span class="text-accent font-mono flex-1">{barWidth(day.cost_usd || 0)}</span>
          <span class="text-muted">{formatCost(day.cost_usd || 0)}</span>
        </div>
      {/each}
    {/if}
  </section>

  <!-- Team -->
  <section>
    <div class="flex justify-between text-xs text-muted font-bold tracking-wider mb-2">
      <span>TEAM</span>
      <span>{teamStats.length} submitters</span>
    </div>
    {#if teamStats.length === 0}
      <div class="text-xs text-muted">No team activity this week.</div>
    {:else}
      {#each teamStats as stat}
        <div class="flex gap-2 text-xs items-center py-0.5">
          <span class="flex-1 truncate">{formatSender(stat.channel_sender_id)}</span>
          <span class="text-muted">{stat.ticket_count} tickets</span>
          <span>{formatCost(stat.cost_usd)}</span>
          {#if stat.failed_count > 0}
            <span class="text-danger">&#10007;{stat.failed_count}</span>
          {/if}
        </div>
      {/each}
    {/if}
  </section>

  <!-- Recent PRs -->
  <section>
    <div class="flex justify-between text-xs text-muted font-bold tracking-wider mb-2">
      <span>RECENT PRS</span>
      <span>{recentPRs.length}</span>
    </div>
    {#if recentPRs.length === 0}
      <div class="text-xs text-muted">No merged PRs yet.</div>
    {:else}
      {#each recentPRs as pr}
        <div class="text-xs py-0.5">
          <a href={pr.PRURL} target="_blank" class="text-accent hover:underline">
            {pr.PRNumber ? `#${pr.PRNumber} ` : ''}{pr.Title}
          </a>
          <span class="text-muted">{pr.Status} {formatRelative(pr.UpdatedAt)}</span>
        </div>
      {/each}
    {/if}
  </section>
</div>
