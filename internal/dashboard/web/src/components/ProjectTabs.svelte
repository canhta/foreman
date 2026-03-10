<script lang="ts">
  import { link, location } from 'svelte-spa-router';
  import { projectState } from '../state/project.svelte';

  interface Props {
    projectId: string;
    projectName: string;
    projectStatus?: string;
  }
  let { projectId, projectName, projectStatus = 'stopped' }: Props = $props();

  const tabs = [
    { label: 'Board', path: 'board' },
    { label: 'Dashboard', path: 'dashboard' },
    { label: 'Settings', path: 'settings' },
  ] as const;

  function isActive(path: string): boolean {
    return $location.endsWith(`/${path}`);
  }

  let syncing = $state(false);
  let pausing = $state(false);

  async function handleSync() {
    syncing = true;
    try {
      await projectState.syncTracker();
    } finally {
      syncing = false;
    }
  }

  async function handlePause() {
    pausing = true;
    try {
      await projectState.pauseProject();
    } finally {
      pausing = false;
    }
  }

  async function handleResume() {
    pausing = true;
    try {
      await projectState.resumeProject();
    } finally {
      pausing = false;
    }
  }

  const isPaused = $derived(projectStatus === 'paused');
  const isRunning = $derived(projectStatus === 'running');
</script>

<div class="border-b border-[var(--color-border)] px-6 flex items-center gap-0">
  <span class="text-xs font-bold tracking-[0.2em] text-[var(--color-text)] py-3.5 pr-6 border-r border-[var(--color-border)] mr-4">{projectName.toUpperCase()}</span>
  <div class="flex gap-1 flex-1">
    {#each tabs as tab}
      <a
        href="/projects/{projectId}/{tab.path}"
        use:link
        class="px-4 py-3.5 text-xs tracking-widest uppercase border-b-2 transition-colors"
        class:border-[var(--color-accent)]={isActive(tab.path)}
        class:text-[var(--color-accent)]={isActive(tab.path)}
        class:font-bold={isActive(tab.path)}
        class:border-transparent={!isActive(tab.path)}
        class:text-[var(--color-muted)]={!isActive(tab.path)}
        class:hover:text-[var(--color-text)]={!isActive(tab.path)}
        class:hover:bg-[var(--color-surface-hover)]={!isActive(tab.path)}
      >
        {tab.label}
      </a>
    {/each}
  </div>

  <!-- Project action buttons -->
  <div class="flex items-center gap-1 ml-4">
    <!-- Sync -->
    <button
      onclick={handleSync}
      disabled={syncing}
      class="px-3 py-1.5 text-[10px] tracking-widest uppercase border border-[var(--color-border)] text-[var(--color-muted)]
             hover:border-[var(--color-accent)] hover:text-[var(--color-accent)] transition-colors disabled:opacity-40 disabled:cursor-not-allowed"
      title="Sync from tracker"
    >
      {syncing ? '↻' : '↻'} {syncing ? 'Syncing…' : 'Sync'}
    </button>

    <!-- Pause / Resume -->
    {#if isPaused}
      <button
        onclick={handleResume}
        disabled={pausing}
        class="px-3 py-1.5 text-[10px] tracking-widest uppercase border border-[var(--color-warning)] text-[var(--color-warning)]
               hover:bg-[var(--color-warning)]/10 transition-colors disabled:opacity-40 disabled:cursor-not-allowed"
        title="Resume project"
      >
        ▶ Resume
      </button>
    {:else if isRunning}
      <button
        onclick={handlePause}
        disabled={pausing}
        class="px-3 py-1.5 text-[10px] tracking-widest uppercase border border-[var(--color-border)] text-[var(--color-muted)]
               hover:border-[var(--color-warning)] hover:text-[var(--color-warning)] transition-colors disabled:opacity-40 disabled:cursor-not-allowed"
        title="Pause project"
      >
        ◼ Pause
      </button>
    {/if}
  </div>
</div>
