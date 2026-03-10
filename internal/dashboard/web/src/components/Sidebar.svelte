<script lang="ts">
  import { link } from 'svelte-spa-router';
  import { location } from 'svelte-spa-router';
  import { globalState } from '../state/global.svelte';

  let collapsed = $state(localStorage.getItem('sidebar_collapsed') === 'true');

  function toggle() {
    collapsed = !collapsed;
    localStorage.setItem('sidebar_collapsed', String(collapsed));
  }

  function statusIndicator(status: string): string {
    switch (status) {
      case 'running': return '●';
      case 'paused': return '⏸';
      case 'error': return '⚠';
      default: return '○';
    }
  }

  function statusColor(status: string): string {
    switch (status) {
      case 'running': return 'text-[var(--color-success)]';
      case 'error': return 'text-[var(--color-danger)]';
      case 'paused': return 'text-[var(--color-warning)]';
      default: return 'text-[var(--color-muted)]';
    }
  }

  function isActive(path: string): boolean {
    return $location === path || $location.startsWith(path + '/');
  }
</script>

<aside
  class="h-screen border-r border-[var(--color-border)] bg-[var(--color-bg)] flex flex-col transition-all duration-200"
  class:w-52={!collapsed}
  class:w-14={collapsed}
>
  <!-- Logo -->
  <div class="h-12 flex items-center px-4 border-b border-[var(--color-border)]">
    {#if !collapsed}
      <span class="text-sm font-bold tracking-[0.3em] text-[var(--color-accent)]">FOREMAN</span>
      <span class="text-[10px] text-[var(--color-muted)] ml-auto tracking-wider">v2</span>
    {:else}
      <span class="text-sm font-bold text-[var(--color-accent)]">F</span>
    {/if}
  </div>

  <!-- Overview -->
  <nav class="flex-1 overflow-y-auto py-2">
    <a
      href="/"
      use:link
      class="flex items-center gap-2 px-4 py-2 text-xs tracking-widest hover:bg-[var(--color-surface-hover)] transition-colors"
      class:bg-[var(--color-accent-bg)]={isActive('/')}
      class:text-[var(--color-accent)]={isActive('/')}
      class:text-[var(--color-muted-bright)]={!isActive('/')}
    >
      {#if !collapsed}OVERVIEW{:else}◈{/if}
    </a>

    <!-- Projects section -->
    {#if !collapsed}
      <div class="px-4 pt-4 pb-1 text-[10px] tracking-[0.2em] text-[var(--color-muted)] uppercase">
        Projects
      </div>
    {:else}
      <div class="border-b border-[var(--color-border)] mx-2 my-2"></div>
    {/if}

    {#each globalState.projects as project}
      <a
        href="/projects/{project.id}/board"
        use:link
        class="flex items-center gap-2 px-4 py-2 text-xs hover:bg-[var(--color-surface-hover)] transition-colors group relative"
        class:bg-[var(--color-accent-bg)]={isActive(`/projects/${project.id}`)}
        class:text-[var(--color-text)]={isActive(`/projects/${project.id}`)}
        class:text-[var(--color-muted-bright)]={!isActive(`/projects/${project.id}`)}
      >
        {#if isActive(`/projects/${project.id}`)}
          <div class="absolute left-0 top-1 bottom-1 w-0.5 bg-[var(--color-accent)]"></div>
        {/if}
        <span class={statusColor(project.status)}>{statusIndicator(project.status)}</span>
        {#if !collapsed}
          <span class="truncate">{project.name}</span>
          {#if project.needsInput > 0}
            <span class="ml-auto text-[10px] bg-[var(--color-warning-bg)] text-[var(--color-warning)] px-1.5">
              {project.needsInput}
            </span>
          {/if}
        {/if}
      </a>
    {/each}

    <a
      href="/projects/new"
      use:link
      class="flex items-center gap-2 px-4 py-2 text-xs text-[var(--color-muted)] hover:text-[var(--color-accent)] hover:bg-[var(--color-surface-hover)] transition-colors"
    >
      {#if !collapsed}+ Add Project{:else}+{/if}
    </a>
  </nav>

  <!-- Bottom section -->
  <div class="border-t border-[var(--color-border)] py-2">
    <button
      onclick={toggle}
      class="flex items-center gap-2 px-4 py-2 text-xs text-[var(--color-muted)] hover:text-[var(--color-text)] w-full text-left"
    >
      {#if !collapsed}◁ Collapse{:else}▷{/if}
    </button>
    <button
      onclick={() => globalState.logout()}
      class="flex items-center gap-2 px-4 py-2 text-xs text-[var(--color-muted)] hover:text-[var(--color-danger)] w-full text-left"
    >
      {#if !collapsed}Logout{:else}✕{/if}
    </button>
  </div>
</aside>
