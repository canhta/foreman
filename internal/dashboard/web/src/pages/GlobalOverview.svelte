<script lang="ts">
  import { globalState } from '../state/global.svelte';
  import { link, push } from 'svelte-spa-router';

  function statusColor(status: string): string {
    switch (status) {
      case 'running': return 'text-[var(--color-success)]';
      case 'error':   return 'text-[var(--color-danger)]';
      case 'paused':  return 'text-[var(--color-warning)]';
      default:        return 'text-[var(--color-muted)]';
    }
  }

  function statusDot(status: string): string {
    switch (status) {
      case 'running': return '●';
      case 'paused':  return '◼';
      case 'error':   return '▲';
      default:        return '○';
    }
  }

  function statusLabel(status: string): string {
    return status.toUpperCase();
  }
</script>

<div class="p-6 max-w-5xl">

  <!-- Page header -->
  <div class="flex items-center justify-between mb-8">
    <div>
      <h1 class="text-xs font-bold tracking-[0.25em] text-[var(--color-accent)] uppercase mb-0.5">Overview</h1>
      <p class="text-[10px] text-[var(--color-muted)] tracking-wider">All projects at a glance</p>
    </div>
    <div class="flex items-center gap-1.5 text-[10px]"
         class:text-[var(--color-success)]={globalState.wsConnected}
         class:text-[var(--color-muted)]={!globalState.wsConnected}>
      <span class="text-[8px]" class:animate-pulse={globalState.wsConnected}>●</span>
      <span class="tracking-[0.15em] uppercase">{globalState.wsConnected ? 'Live' : 'Polling'}</span>
    </div>
  </div>

  {#if globalState.loading}
    <!-- Skeleton -->
    <div class="grid grid-cols-2 lg:grid-cols-4 gap-3 mb-8">
      {#each [0, 1, 2, 3] as i}
        <div class="bg-[var(--color-surface)] border border-[var(--color-border)] p-5 relative animate-pulse-slow"
             style="animation-delay: {i * 60}ms">
          <div class="absolute top-0 left-0 right-0 h-0.5 bg-[var(--color-border-strong)]"></div>
          <div class="h-2 w-20 bg-[var(--color-surface-hover)] mb-4 rounded-none"></div>
          <div class="h-9 w-16 bg-[var(--color-surface-hover)] rounded-none"></div>
        </div>
      {/each}
    </div>
  {:else}
    <!-- Summary cards -->
    <div class="grid grid-cols-2 lg:grid-cols-4 gap-3 mb-8">

      <!-- Cost Today -->
      <div class="bg-[var(--color-surface)] border border-[var(--color-border)] p-5 relative animate-[fade-in_0.18s_ease-out]"
           style="animation-fill-mode:both">
        <div class="absolute top-0 left-0 right-0 h-0.5 bg-[var(--color-accent)]"></div>
        <div class="text-[10px] tracking-[0.15em] text-[var(--color-muted)] uppercase mb-3">Cost Today</div>
        <div class="text-3xl font-bold text-[var(--color-accent)] leading-none tabular-nums">
          ${globalState.overview.cost_today.toFixed(2)}
        </div>
      </div>

      <!-- Active Tickets -->
      <div class="bg-[var(--color-surface)] border border-[var(--color-border)] p-5 relative animate-[fade-in_0.18s_ease-out]"
           style="animation-delay:50ms; animation-fill-mode:both">
        <div class="absolute top-0 left-0 right-0 h-0.5 bg-[var(--color-border-strong)]"></div>
        <div class="text-[10px] tracking-[0.15em] text-[var(--color-muted)] uppercase mb-3">Active Tickets</div>
        <div class="text-3xl font-bold leading-none tabular-nums">{globalState.overview.active_tickets}</div>
      </div>

      <!-- Open PRs -->
      <div class="bg-[var(--color-surface)] border border-[var(--color-border)] p-5 relative animate-[fade-in_0.18s_ease-out]"
           style="animation-delay:100ms; animation-fill-mode:both">
        <div class="absolute top-0 left-0 right-0 h-0.5 bg-[var(--color-border-strong)]"></div>
        <div class="text-[10px] tracking-[0.15em] text-[var(--color-muted)] uppercase mb-3">Open PRs</div>
        <div class="text-3xl font-bold leading-none tabular-nums">{globalState.overview.open_prs}</div>
      </div>

      <!-- Needs Input -->
      <div class="bg-[var(--color-surface)] border p-5 relative animate-[fade-in_0.18s_ease-out] transition-colors"
           style="animation-delay:150ms; animation-fill-mode:both"
           class:border-[var(--color-warning)]={globalState.overview.need_input > 0}
           class:border-[var(--color-border)]={globalState.overview.need_input === 0}
           class:shadow-[0_0_24px_rgba(255,170,32,0.06)]={globalState.overview.need_input > 0}>
        {#if globalState.overview.need_input > 0}
          <div class="absolute top-0 left-0 right-0 h-0.5 bg-[var(--color-warning)] animate-pulse-slow"></div>
        {:else}
          <div class="absolute top-0 left-0 right-0 h-0.5 bg-[var(--color-border-strong)]"></div>
        {/if}
        <div class="text-[10px] tracking-[0.15em] uppercase mb-3"
             class:text-[var(--color-warning)]={globalState.overview.need_input > 0}
             class:text-[var(--color-muted)]={globalState.overview.need_input === 0}>
          Needs Input
        </div>
        <div class="text-3xl font-bold leading-none tabular-nums"
             class:text-[var(--color-warning)]={globalState.overview.need_input > 0}>
          {globalState.overview.need_input}
        </div>
        {#if globalState.overview.need_input > 0}
          <div class="mt-3 text-[10px] text-[var(--color-warning)] tracking-wider">Awaiting reply</div>
        {/if}
      </div>
    </div>

    <!-- Projects table -->
    <div class="bg-[var(--color-surface)] border border-[var(--color-border)]">
      <!-- Table header -->
      <div class="px-5 py-3 border-b border-[var(--color-border)] flex items-center justify-between">
        <span class="text-[10px] tracking-[0.2em] text-[var(--color-muted)] uppercase font-bold">Projects</span>
        <a href="/projects/new" use:link
           class="text-[10px] tracking-wider text-[var(--color-accent)] hover:opacity-80 transition-opacity uppercase">
          + New Project
        </a>
      </div>

      {#if globalState.projects.length > 0}
        <table class="w-full">
          <thead>
            <tr class="border-b border-[var(--color-border)]">
              <th class="text-left px-5 py-2.5 text-[10px] tracking-[0.15em] text-[var(--color-muted)] uppercase font-normal">Name</th>
              <th class="text-right px-5 py-2.5 text-[10px] tracking-[0.15em] text-[var(--color-muted)] uppercase font-normal hidden sm:table-cell">Needs Input</th>
              <th class="text-right px-5 py-2.5 text-[10px] tracking-[0.15em] text-[var(--color-muted)] uppercase font-normal">Status</th>
            </tr>
          </thead>
          <tbody>
            {#each globalState.projects as project, i}
              <tr
                class="border-b border-[var(--color-border)] hover:bg-[var(--color-surface-hover)] cursor-pointer transition-colors group"
                style="animation-delay: {i * 40}ms"
                onclick={() => push(`/projects/${project.id}/board`)}
              >
                <td class="px-5 py-3.5">
                  <div class="flex items-center gap-2">
                    <span class="text-sm font-bold text-[var(--color-text)] group-hover:text-[var(--color-accent)] transition-colors leading-none">
                      {project.name}
                    </span>
                  </div>
                </td>
                <td class="text-right px-5 py-3.5 hidden sm:table-cell">
                  {#if (project.needsInput ?? 0) > 0}
                    <span class="text-xs font-bold text-[var(--color-warning)] bg-[var(--color-warning-bg)]
                                 border border-[var(--color-warning)]/30 px-2 py-0.5 inline-block">
                      {project.needsInput}
                    </span>
                  {:else}
                    <span class="text-[var(--color-muted)] text-xs">—</span>
                  {/if}
                </td>
                <td class="text-right px-5 py-3.5">
                  <span class="inline-flex items-center justify-end gap-1.5 text-xs {statusColor(project.status ?? 'stopped')}">
                    <span class="text-[10px]" class:animate-pulse={project.status === 'running'}>
                      {statusDot(project.status ?? 'stopped')}
                    </span>
                    <span class="tracking-wider">{statusLabel(project.status ?? 'stopped')}</span>
                  </span>
                </td>
              </tr>
            {/each}
          </tbody>
        </table>
      {:else}
        <div class="px-5 py-12 text-center">
          <div class="text-[var(--color-muted)] text-xs mb-3">No projects yet</div>
          <a href="/projects/new" use:link
             class="text-[10px] tracking-wider text-[var(--color-accent)] hover:opacity-80 transition-opacity uppercase">
            Create your first project →
          </a>
        </div>
      {/if}
    </div>
  {/if}
</div>
