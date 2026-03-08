<script lang="ts">
  import {
    appState, pauseDaemon, resumeDaemon, setActivePanel,
  } from '../state.svelte';
  import { formatCost } from '../format';

  let dotClass = $derived(
    !appState.wsConnected ? 'bg-danger' :
    appState.daemonState === 'running' ? 'bg-success' :
    'bg-warning'
  );

  let label = $derived(!appState.wsConnected ? 'DISCONNECTED' : appState.daemonState.toUpperCase());

  let costLabel = $derived(
    appState.dailyBudget > 0
      ? `COST: ${formatCost(appState.dailyCost)} / $${Math.round(appState.dailyBudget)}`
      : `COST: ${formatCost(appState.dailyCost)}`
  );

  let costOverBudget = $derived(appState.dailyBudget > 0 && (appState.dailyCost / appState.dailyBudget) * 100 >= 80);

  function handlePause() {
    if (confirm('Pause the daemon?')) pauseDaemon();
  }
  function handleResume() {
    if (confirm('Resume the daemon?')) resumeDaemon();
  }
</script>

<header class="flex items-center justify-between px-4 py-2 border-b border-border bg-surface sticky top-0 z-50">
  <button
    class="text-accent font-bold text-lg tracking-wider hover:opacity-80 cursor-pointer"
    onclick={() => setActivePanel('tickets')}
  >FOREMAN</button>

  <div class="flex items-center gap-3 text-sm">
    <span class="inline-block w-2 h-2 rounded-full {dotClass}"></span>
    <span class="text-muted">{label}</span>

    {#if appState.whatsapp !== null}
      <span class="text-border">|</span>
      <span class="inline-block w-2 h-2 rounded-full {appState.whatsapp ? 'bg-success' : 'bg-danger'}"></span>
      <span class:text-danger={!appState.whatsapp}>{appState.whatsapp ? 'WA: OK' : 'WA: DOWN'}</span>
    {/if}

    <span class="text-border hidden md:inline">|</span>
    <span class="hidden md:inline {costOverBudget ? 'text-danger' : 'text-muted'}">{costLabel}</span>
    <span class="text-border hidden md:inline">|</span>
    <span class="hidden md:inline text-muted">ACTIVE: {appState.activeCount}</span>

    <span class="text-border">|</span>
    {#if appState.daemonState === 'running'}
      <button class="text-accent hover:text-text text-xs" onclick={handlePause}>PAUSE</button>
    {:else}
      <button class="text-accent hover:text-text text-xs" onclick={handleResume}>RESUME</button>
    {/if}

    <button
      class="text-muted hover:text-accent text-xs hidden md:inline"
      onclick={() => setActivePanel('health')}
    >SYSTEM</button>
  </div>
</header>
