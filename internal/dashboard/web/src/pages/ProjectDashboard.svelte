<script lang="ts">
  import { projectState } from '../state/project.svelte';
  import { globalState } from '../state/global.svelte';
  import ProjectTabs from '../components/ProjectTabs.svelte';

  let { params } = $props<{ params: { pid: string } }>();

  const project = $derived(globalState.projects.find(p => p.id === params.pid));

  $effect(() => {
    if (params.pid) {
      projectState.switchProject(params.pid);
    }
  });

  let activeTickets  = $derived(projectState.tickets.filter(t => !['done', 'merged', 'failed'].includes(t.Status)).length);
  let doneTickets    = $derived(projectState.tickets.filter(t => ['done', 'merged'].includes(t.Status)).length);
  let failedTickets  = $derived(projectState.tickets.filter(t => t.Status === 'failed').length);
  let successRate    = $derived(
    doneTickets + failedTickets > 0
      ? Math.round((doneTickets / (doneTickets + failedTickets)) * 100)
      : 0
  );
  let maxDayCost = $derived(Math.max(...projectState.weekDays.map(d => d.cost_usd), 0.01));

  // Grid line values for the bar chart
  let gridLines = $derived(() => {
    if (maxDayCost <= 0.01) return [];
    const top = maxDayCost * 1.1;
    return [top, top * 0.75, top * 0.5, top * 0.25];
  });
</script>

{#if project}
  <ProjectTabs projectId={params.pid} projectName={project.name} />
{/if}

<div class="p-6 w-full max-w-5xl">

  <!-- Summary cards -->
  <div class="grid grid-cols-2 lg:grid-cols-4 gap-3 mb-8">
    <div class="bg-[var(--color-surface)] border border-[var(--color-border)] p-5 relative animate-[fade-in_0.18s_ease-out]"
         style="animation-fill-mode:both">
      <div class="absolute top-0 left-0 right-0 h-0.5 bg-[var(--color-accent)]"></div>
      <div class="text-[10px] tracking-[0.15em] text-[var(--color-muted)] uppercase mb-3">Cost Today</div>
      <div class="text-3xl font-bold text-[var(--color-accent)] leading-none tabular-nums">
        ${projectState.dailyCost.toFixed(2)}
      </div>
    </div>

    <div class="bg-[var(--color-surface)] border border-[var(--color-border)] p-5 relative animate-[fade-in_0.18s_ease-out]"
         style="animation-delay:50ms; animation-fill-mode:both">
      <div class="absolute top-0 left-0 right-0 h-0.5 bg-[var(--color-border-strong)]"></div>
      <div class="text-[10px] tracking-[0.15em] text-[var(--color-muted)] uppercase mb-3">This Month</div>
      <div class="text-3xl font-bold leading-none tabular-nums">
        ${projectState.monthlyCost.toFixed(2)}
      </div>
    </div>

    <div class="bg-[var(--color-surface)] border border-[var(--color-border)] p-5 relative animate-[fade-in_0.18s_ease-out]"
         style="animation-delay:100ms; animation-fill-mode:both">
      <div class="absolute top-0 left-0 right-0 h-0.5 bg-[var(--color-border-strong)]"></div>
      <div class="text-[10px] tracking-[0.15em] text-[var(--color-muted)] uppercase mb-3">Active Tickets</div>
      <div class="text-3xl font-bold leading-none tabular-nums">{activeTickets}</div>
    </div>

    <div class="bg-[var(--color-surface)] border border-[var(--color-border)] p-5 relative animate-[fade-in_0.18s_ease-out]"
         style="animation-delay:150ms; animation-fill-mode:both">
      <div class="absolute top-0 left-0 right-0 h-0.5 bg-[var(--color-success)]"></div>
      <div class="text-[10px] tracking-[0.15em] text-[var(--color-muted)] uppercase mb-3">Success Rate</div>
      <div class="text-3xl font-bold leading-none tabular-nums text-[var(--color-success)]">
        {successRate}%
      </div>
    </div>
  </div>

  <!-- 7-day cost trend -->
  <div class="bg-[var(--color-surface)] border border-[var(--color-border)] p-5 mb-8">
    <div class="text-[10px] tracking-[0.15em] text-[var(--color-muted)] uppercase mb-5 font-bold">7-Day Cost Trend</div>

    {#if projectState.weekDays.length === 0}
      <div class="flex items-center justify-center h-32 text-xs text-[var(--color-muted)]">No cost data yet</div>
    {:else}
      <div class="flex gap-0 h-40 items-end relative">
        <!-- Grid lines -->
        <div class="absolute inset-0 bottom-6 pointer-events-none flex flex-col justify-between">
          {#each [0, 1, 2, 3] as line}
            <div class="flex items-center gap-2 w-full">
              <div class="w-full h-px bg-[var(--color-border)]"></div>
            </div>
          {/each}
        </div>

        <!-- Bars -->
        {#each projectState.weekDays as day, i}
          {@const pct = Math.max((day.cost_usd / maxDayCost) * 100, day.cost_usd > 0 ? 3 : 0.5)}
          <div class="flex-1 flex flex-col items-center gap-1.5 group relative">
            <!-- Tooltip -->
            {#if day.cost_usd > 0}
              <div class="absolute bottom-full mb-1 opacity-0 group-hover:opacity-100 transition-opacity
                          bg-[var(--color-surface-active)] border border-[var(--color-border-strong)]
                          text-[10px] text-[var(--color-text)] px-2 py-1 whitespace-nowrap z-10 pointer-events-none
                          -translate-x-1/2 left-1/2">
                ${day.cost_usd.toFixed(4)}
              </div>
            {/if}
            <!-- Bar container -->
            <div class="w-full flex flex-col justify-end" style="height: 128px">
              <div
                class="w-full animate-[fade-in_0.18s_ease-out] relative transition-opacity duration-200"
                class:bg-[var(--color-accent)]={day.cost_usd > 0}
                class:opacity-80={day.cost_usd > 0}
                class:group-hover:opacity-100={day.cost_usd > 0}
                class:bg-[var(--color-border)]={day.cost_usd === 0}
                style="height: {pct}%; animation-delay: {i * 50}ms; min-height: 2px"
              ></div>
            </div>
            <!-- Day label -->
            <span class="text-[9px] text-[var(--color-muted)] tracking-wider">
              {new Date(day.date + 'T12:00:00').toLocaleDateString('en', { weekday: 'short' }).toUpperCase()}
            </span>
          </div>
        {/each}
      </div>
    {/if}
  </div>

  <!-- Ticket throughput -->
  <div class="bg-[var(--color-surface)] border border-[var(--color-border)]">
    <div class="px-5 py-3 border-b border-[var(--color-border)]">
      <span class="text-[10px] tracking-[0.15em] text-[var(--color-muted)] uppercase font-bold">Ticket Throughput</span>
    </div>
    <div class="divide-y divide-[var(--color-border)]">
      <div class="flex items-center justify-between px-5 py-3">
        <span class="text-xs text-[var(--color-muted-bright)]">Completed</span>
        <span class="text-sm font-bold text-[var(--color-success)] tabular-nums">{doneTickets}</span>
      </div>
      <div class="flex items-center justify-between px-5 py-3">
        <span class="text-xs text-[var(--color-muted-bright)]">Failed</span>
        <span class="text-sm font-bold text-[var(--color-danger)] tabular-nums">{failedTickets}</span>
      </div>
      <div class="flex items-center justify-between px-5 py-3">
        <span class="text-xs text-[var(--color-muted-bright)]">Active</span>
        <span class="text-sm font-bold tabular-nums">{activeTickets}</span>
      </div>
      <div class="flex items-center justify-between px-5 py-3 bg-[var(--color-surface-hover)]">
        <span class="text-xs text-[var(--color-muted-bright)] font-bold">Total</span>
        <span class="text-sm font-bold tabular-nums">{projectState.tickets.length}</span>
      </div>
    </div>
  </div>
</div>
