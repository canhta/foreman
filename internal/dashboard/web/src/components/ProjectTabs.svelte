<script lang="ts">
  import { link, location } from 'svelte-spa-router';

  interface Props {
    projectId: string;
    projectName: string;
  }
  let { projectId, projectName }: Props = $props();

  const tabs = [
    { label: 'Board', path: 'board' },
    { label: 'Dashboard', path: 'dashboard' },
    { label: 'Settings', path: 'settings' },
  ] as const;

  function isActive(path: string): boolean {
    return $location.endsWith(`/${path}`);
  }
</script>

<div class="border-b border-[var(--color-border)] px-6 flex items-center gap-6">
  <span class="text-xs font-bold tracking-widest text-[var(--color-text)] py-3">{projectName.toUpperCase()}</span>
  <div class="flex gap-1">
    {#each tabs as tab}
      <a
        href="/projects/{projectId}/{tab.path}"
        use:link
        class="px-3 py-3 text-[10px] tracking-widest uppercase border-b-2 transition-colors"
        class:border-[var(--color-accent)]={isActive(tab.path)}
        class:text-[var(--color-accent)]={isActive(tab.path)}
        class:border-transparent={!isActive(tab.path)}
        class:text-[var(--color-muted)]={!isActive(tab.path)}
        class:hover:text-[var(--color-text)]={!isActive(tab.path)}
      >
        {tab.label}
      </a>
    {/each}
  </div>
</div>
