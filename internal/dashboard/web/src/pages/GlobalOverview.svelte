<script lang="ts">
  import { globalState } from '../state/global.svelte';
  import { link } from 'svelte-spa-router';

  function statusColor(status: string): string {
    switch (status) {
      case 'running': return 'text-[var(--color-success)]';
      case 'error': return 'text-[var(--color-danger)]';
      case 'paused': return 'text-[var(--color-warning)]';
      default: return 'text-[var(--color-muted)]';
    }
  }
</script>

<div class="p-6 max-w-6xl">
  <h1 class="text-sm font-bold tracking-[0.3em] text-[var(--color-accent)] mb-6">OVERVIEW</h1>

  <!-- Summary cards -->
  <div class="grid grid-cols-2 lg:grid-cols-4 gap-4 mb-8">
    <div class="border border-[var(--color-border)] p-4 relative animate-fade-in">
      <div class="absolute top-0 left-0 right-0 h-0.5 bg-[var(--color-accent)]"></div>
      <div class="text-[10px] tracking-widest text-[var(--color-muted)] uppercase">Cost Today</div>
      <div class="text-2xl font-bold mt-1 text-[var(--color-accent)]">${globalState.overview.cost_today.toFixed(2)}</div>
    </div>
    <div class="border border-[var(--color-border)] p-4 relative animate-fade-in" style="animation-delay: 50ms">
      <div class="absolute top-0 left-0 right-0 h-0.5 bg-[var(--color-border-strong)]"></div>
      <div class="text-[10px] tracking-widest text-[var(--color-muted)] uppercase">Active Tickets</div>
      <div class="text-2xl font-bold mt-1">{globalState.overview.active_tickets}</div>
    </div>
    <div class="border border-[var(--color-border)] p-4 relative animate-fade-in" style="animation-delay: 100ms">
      <div class="absolute top-0 left-0 right-0 h-0.5 bg-[var(--color-border-strong)]"></div>
      <div class="text-[10px] tracking-widests text-[var(--color-muted)] uppercase">Open PRs</div>
      <div class="text-2xl font-bold mt-1">{globalState.overview.open_prs}</div>
    </div>
    <div class="border p-4 relative animate-fade-in transition-colors" style="animation-delay: 150ms"
         class:border-[var(--color-warning)]={globalState.overview.need_input > 0}
         class:border-[var(--color-border)]={globalState.overview.need_input === 0}
         class:shadow-[0_0_20px_rgba(255,170,32,0.08)]={globalState.overview.need_input > 0}>
      {#if globalState.overview.need_input > 0}
        <div class="absolute top-0 left-0 right-0 h-0.5 bg-[var(--color-warning)] animate-pulse-slow"></div>
      {:else}
        <div class="absolute top-0 left-0 right-0 h-0.5 bg-[var(--color-border-strong)]"></div>
      {/if}
      <div class="text-[10px] tracking-widest uppercase" class:text-[var(--color-warning)]={globalState.overview.need_input > 0} class:text-[var(--color-muted)]={globalState.overview.need_input === 0}>Needs Input</div>
      <div class="text-2xl font-bold mt-1" class:text-[var(--color-warning)]={globalState.overview.need_input > 0}>{globalState.overview.need_input}</div>
    </div>
  </div>

  <!-- Project summary table -->
  <div class="border border-[var(--color-border)]">
    <div class="px-4 py-3 border-b border-[var(--color-border)]">
      <span class="text-[10px] tracking-widest text-[var(--color-muted)] uppercase">Projects</span>
    </div>
    <table class="w-full text-xs">
      <thead>
        <tr class="text-[var(--color-muted)] text-[10px] tracking-widest uppercase border-b border-[var(--color-border)]">
          <th class="text-left px-4 py-2">Project</th>
          <th class="text-right px-4 py-2">Active</th>
          <th class="text-right px-4 py-2">Input</th>
          <th class="text-right px-4 py-2">Status</th>
        </tr>
      </thead>
      <tbody>
        {#each globalState.projects as project}
          <tr class="border-b border-[var(--color-border)] hover:bg-[var(--color-surface-hover)] cursor-pointer"
              onclick={() => window.location.hash = `/projects/${project.id}/board`}>
            <td class="px-4 py-3">{project.name}</td>
            <td class="text-right px-4 py-3">{project.active ? 1 : 0}</td>
            <td class="text-right px-4 py-3">
              {#if (project.needsInput ?? 0) > 0}
                <span class="text-[var(--color-warning)]">{project.needsInput}</span>
              {:else}
                0
              {/if}
            </td>
            <td class="text-right px-4 py-3">
              <span class={statusColor(project.status ?? 'stopped')}>
                {project.status ?? 'stopped'}
              </span>
            </td>
          </tr>
        {/each}
        {#if globalState.projects.length === 0}
          <tr>
            <td colspan="4" class="px-4 py-8 text-center text-[var(--color-muted)]">
              No projects yet. <a href="/projects/new" use:link class="text-[var(--color-accent)] hover:underline">Create one</a>
            </td>
          </tr>
        {/if}
      </tbody>
    </table>
  </div>
</div>
