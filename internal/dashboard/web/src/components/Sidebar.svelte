<script lang="ts">
  import { link } from 'svelte-spa-router';
  import { location } from 'svelte-spa-router';
  import { globalState } from '../state/global.svelte';
  import {
    IconCircleFilled, IconPlayerPause, IconAlertTriangle, IconCircle,
    IconLayoutDashboard, IconChevronLeft, IconChevronRight, IconX
  } from '@tabler/icons-svelte';

  let collapsed = $state((() => { try { return localStorage.getItem('sidebar_collapsed') === 'true'; } catch { return false; } })());

  function toggle() {
    collapsed = !collapsed;
    try { localStorage.setItem('sidebar_collapsed', String(collapsed)); } catch {}
  }

  function statusColor(status: string): string {
    switch (status) {
      case 'running': return 'text-[var(--color-success)]';
      case 'error':   return 'text-[var(--color-danger)]';
      case 'paused':  return 'text-[var(--color-warning)]';
      default:        return 'text-[var(--color-muted)]';
    }
  }

  function isActive(path: string): boolean {
    return $location === path || $location.startsWith(path + '/');
  }

  const overviewActive = $derived(
    $location === '/' || $location === ''
  );
</script>

<aside
  class="h-screen border-r border-[var(--color-border)] bg-[var(--color-bg)] flex flex-col transition-all duration-200 shrink-0"
  class:w-52={!collapsed}
  class:w-14={collapsed}
>
  <!-- Logo -->
  <div class="h-12 flex items-center px-4 border-b border-[var(--color-border)] shrink-0">
    {#if !collapsed}
      <div class="flex items-baseline gap-2 flex-1 min-w-0">
        <span class="text-base font-bold tracking-[0.25em] text-[var(--color-accent)] leading-none">FOREMAN</span>
        <span class="text-[10px] text-[var(--color-muted)] tracking-widest leading-none">v2</span>
      </div>
    {:else}
      <span class="text-base font-bold text-[var(--color-accent)] leading-none mx-auto">F</span>
    {/if}
  </div>

  <!-- Nav -->
  <nav class="flex-1 overflow-y-auto py-3 space-y-0.5">

    <!-- Overview -->
    <a
      href="/"
      use:link
      class="flex items-center gap-2.5 px-4 py-2 text-xs tracking-[0.15em] uppercase transition-colors relative group"
      class:bg-[var(--color-accent-bg)]={overviewActive}
      class:text-[var(--color-accent)]={overviewActive}
      class:font-bold={overviewActive}
      class:text-[var(--color-muted-bright)]={!overviewActive}
      class:hover:text-[var(--color-text)]={!overviewActive}
      class:hover:bg-[var(--color-surface-hover)]={!overviewActive}
    >
      {#if overviewActive}
        <div class="absolute left-0 top-0 bottom-0 w-0.5 bg-[var(--color-accent)]"></div>
      {/if}
      {#if !collapsed}
        <span class="shrink-0 flex items-center {overviewActive ? 'text-[var(--color-accent)]' : 'text-[var(--color-muted)]'}"><IconLayoutDashboard size={14} stroke={1.5} /></span>
        <span>Overview</span>
      {:else}
        <span class="mx-auto flex items-center {overviewActive ? 'text-[var(--color-accent)]' : 'text-[var(--color-muted-bright)]'}"><IconLayoutDashboard size={14} stroke={1.5} /></span>
      {/if}
    </a>

    <!-- Projects heading -->
    {#if !collapsed}
      <div class="px-4 pt-4 pb-1.5 flex items-center justify-between">
        <span class="text-[10px] tracking-[0.2em] text-[var(--color-muted)] uppercase">Projects</span>
        <a href="/projects/new" use:link class="text-[10px] text-[var(--color-muted)] hover:text-[var(--color-accent)] transition-colors leading-none">+</a>
      </div>
    {:else}
      <div class="border-b border-[var(--color-border)] mx-3 my-2"></div>
    {/if}

    <!-- Project entries -->
    {#each globalState.projects as project}
      {@const active = isActive(`/projects/${project.id}`)}
      {@const hasInput = (project.needsInput ?? 0) > 0}
      <a
        href="/projects/{project.id}/board"
        use:link
        class="flex items-center gap-2.5 px-4 py-2 text-xs transition-colors relative group"
        class:bg-[var(--color-surface-hover)]={active}
        class:text-[var(--color-text)]={active}
        class:font-medium={active}
        class:text-[var(--color-muted-bright)]={!active}
        class:hover:text-[var(--color-text)]={!active}
        class:hover:bg-[var(--color-surface-hover)]={!active}
      >
        {#if active}
          <div class="absolute left-0 top-0 bottom-0 w-0.5 bg-[var(--color-accent)]"></div>
        {/if}
        <!-- Status dot -->
        <span class="shrink-0 flex items-center {statusColor(project.status ?? 'stopped')}"
              class:animate-pulse={project.status === 'running'}>
          {#if project.status === 'running'}<IconCircleFilled size={10} stroke={1.5} />
          {:else if project.status === 'paused'}<IconPlayerPause size={10} stroke={1.5} />
          {:else if project.status === 'error'}<IconAlertTriangle size={10} stroke={1.5} />
          {:else}<IconCircle size={10} stroke={1.5} />{/if}
        </span>
        {#if !collapsed}
          <span class="truncate flex-1 leading-snug">{project.name}</span>
          {#if hasInput}
            <span class="shrink-0 text-[10px] font-bold text-[var(--color-warning)] bg-[var(--color-warning-bg)]
                         border border-[var(--color-warning)]/30 px-1.5 py-0.5 leading-none min-w-[1.2rem] text-center">
              {project.needsInput}
            </span>
          {/if}
        {/if}
      </a>
    {/each}

    <!-- Add project (expanded) -->
    {#if !collapsed}
      <a
        href="/projects/new"
        use:link
        class="flex items-center gap-2.5 px-4 py-2 text-xs text-[var(--color-muted)] hover:text-[var(--color-accent)] hover:bg-[var(--color-surface-hover)] transition-colors"
      >
        <span class="text-[10px] leading-none">+</span>
        <span>Add Project</span>
      </a>
    {/if}
  </nav>

  <!-- Bottom section -->
  <div class="border-t border-[var(--color-border)] py-1.5 shrink-0">
    <button
      onclick={toggle}
      class="flex items-center gap-2.5 px-4 py-2 text-[11px] text-[var(--color-muted)] hover:text-[var(--color-text)] w-full text-left transition-colors"
      title={collapsed ? 'Expand sidebar' : 'Collapse sidebar'}
    >
      {#if !collapsed}
        <span class="flex items-center"><IconChevronLeft size={14} stroke={1.5} /></span>
        <span class="tracking-wider text-[10px] uppercase">Collapse</span>
      {:else}
        <span class="mx-auto flex items-center"><IconChevronRight size={14} stroke={1.5} /></span>
      {/if}
    </button>
    <button
      onclick={() => globalState.logout()}
      class="flex items-center gap-2.5 px-4 py-2 text-[10px] text-[var(--color-muted)] hover:text-[var(--color-danger)] w-full text-left transition-colors uppercase tracking-[0.15em]"
    >
      {#if !collapsed}
        <span class="flex items-center"><IconX size={14} stroke={1.5} /></span>
        <span>Logout</span>
      {:else}
        <span class="mx-auto flex items-center"><IconX size={14} stroke={1.5} /></span>
      {/if}
    </button>
  </div>
</aside>
