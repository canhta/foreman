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

  let activeTickets = $derived(
    projectState.tickets.filter(t => !['done', 'merged', 'failed'].includes(t.Status)).length
  );
  let doneTickets = $derived(
    projectState.tickets.filter(t => ['done', 'merged'].includes(t.Status)).length
  );
  let failedTickets = $derived(
    projectState.tickets.filter(t => t.Status === 'failed').length
  );
  let successRate = $derived(
    doneTickets + failedTickets > 0
      ? Math.round((doneTickets / (doneTickets + failedTickets)) * 100)
      : 0
  );
  let maxDayCost = $derived(
    Math.max(...projectState.weekDays.map(d => d.cost_usd), 0.01)
  );
</script>

{#if project}
  <ProjectTabs projectId={params.pid} projectName={project.name} />
{/if}

<div class="p-6 max-w-5xl">
  <!-- Summary cards -->
  <div class="grid grid-cols-2 lg:grid-cols-4 gap-4 mb-8">
    <div class="border border-[var(--color-border)] p-4 relative animate-fade-in">
      <div class="absolute top-0 left-0 right-0 h-0.5 bg-[var(--color-accent)]"></div>
      <div class="text-[10px] tracking-widest text-[var(--color-muted)] uppercase">Cost Today</div>
      <div class="text-2xl font-bold mt-1 text-[var(--color-accent)]">${projectState.dailyCost.toFixed(2)}</div>
    </div>
    <div class="border border-[var(--color-border)] p-4 relative animate-fade-in" style="animation-delay: 50ms">
      <div class="absolute top-0 left-0 right-0 h-0.5 bg-[var(--color-border-strong)]"></div>
      <div class="text-[10px] tracking-widest text-[var(--color-muted)] uppercase">Cost This Month</div>
      <div class="text-2xl font-bold mt-1">${projectState.monthlyCost.toFixed(2)}</div>
    </div>
    <div class="border border-[var(--color-border)] p-4 relative animate-fade-in" style="animation-delay: 100ms">
      <div class="absolute top-0 left-0 right-0 h-0.5 bg-[var(--color-border-strong)]"></div>
      <div class="text-[10px] tracking-widest text-[var(--color-muted)] uppercase">Active Tickets</div>
      <div class="text-2xl font-bold mt-1">{activeTickets}</div>
    </div>
    <div class="border border-[var(--color-border)] p-4 relative animate-fade-in" style="animation-delay: 150ms">
      <div class="absolute top-0 left-0 right-0 h-0.5 bg-[var(--color-success)]"></div>
      <div class="text-[10px] tracking-widest text-[var(--color-muted)] uppercase">Success Rate</div>
      <div class="text-2xl font-bold mt-1">{successRate}%</div>
    </div>
  </div>

  <!-- 7-day cost trend -->
  <div class="border border-[var(--color-border)] p-4 mb-8">
    <div class="text-[10px] tracking-widest text-[var(--color-muted)] uppercase mb-4">7-Day Cost Trend</div>
    {#if projectState.weekDays.length === 0}
      <div class="flex items-center justify-center h-32 text-xs text-[var(--color-muted)]">No cost data yet</div>
    {:else}
      <div class="flex items-end gap-1 h-36">
        {#each projectState.weekDays as day, i}
          {@const pct = Math.max((day.cost_usd / maxDayCost) * 100, day.cost_usd > 0 ? 4 : 1)}
          <div class="flex-1 flex flex-col items-center gap-1 group" title="${day.cost_usd.toFixed(4)}">
            <div class="w-full flex flex-col justify-end" style="height: 112px">
              <div class="w-full bg-[var(--color-accent)] opacity-80 group-hover:opacity-100 transition-opacity
                          animate-fade-in relative"
                   style="height: {pct}%; animation-delay: {i * 60}ms; min-height: 2px">
                {#if day.cost_usd > 0}
                  <div class="absolute -top-4 left-0 right-0 text-center text-[9px] text-[var(--color-muted)]
                              opacity-0 group-hover:opacity-100 transition-opacity whitespace-nowrap">
                    ${day.cost_usd.toFixed(2)}
                  </div>
                {/if}
              </div>
            </div>
            <span class="text-[9px] text-[var(--color-muted)]">
              {new Date(day.date + 'T12:00:00').toLocaleDateString('en', { weekday: 'short' })}
            </span>
          </div>
        {/each}
      </div>
    {/if}
  </div>

  <!-- Ticket throughput -->
  <div class="border border-[var(--color-border)] mb-8">
    <div class="px-4 py-3 border-b border-[var(--color-border)]">
      <span class="text-[10px] tracking-widest text-[var(--color-muted)] uppercase">Ticket Throughput</span>
    </div>
    <table class="w-full text-xs">
      <tbody>
        <tr class="border-b border-[var(--color-border)]">
          <td class="px-4 py-2 text-[var(--color-muted)]">Completed</td>
          <td class="px-4 py-2 text-right text-[var(--color-success)]">{doneTickets}</td>
        </tr>
        <tr class="border-b border-[var(--color-border)]">
          <td class="px-4 py-2 text-[var(--color-muted)]">Failed</td>
          <td class="px-4 py-2 text-right text-[var(--color-danger)]">{failedTickets}</td>
        </tr>
        <tr class="border-b border-[var(--color-border)]">
          <td class="px-4 py-2 text-[var(--color-muted)]">Active</td>
          <td class="px-4 py-2 text-right">{activeTickets}</td>
        </tr>
        <tr>
          <td class="px-4 py-2 text-[var(--color-muted)]">Total</td>
          <td class="px-4 py-2 text-right font-bold">{projectState.tickets.length}</td>
        </tr>
      </tbody>
    </table>
  </div>
</div>
