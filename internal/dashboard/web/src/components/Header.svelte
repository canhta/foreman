<script lang="ts">
  import {
    appState, pauseDaemon, resumeDaemon, setActivePanel,
  } from '../state.svelte';
  import { formatCost } from '../format';

  let dotBg = $derived(
    !appState.wsConnected ? 'bg-danger' :
    appState.daemonState === 'running' ? 'bg-success' :
    'bg-warning'
  );
  let label = $derived(!appState.wsConnected ? 'OFFLINE' : appState.daemonState.toUpperCase());
  let costOverBudget = $derived(appState.dailyBudget > 0 && (appState.dailyCost / appState.dailyBudget) >= 0.8);

  let costLabel = $derived(
    appState.dailyBudget > 0
      ? `${formatCost(appState.dailyCost)} / $${Math.round(appState.dailyBudget)}`
      : formatCost(appState.dailyCost)
  );

  function handlePause() {
    if (confirm('Pause the daemon?')) pauseDaemon();
  }
  function handleResume() {
    if (confirm('Resume the daemon?')) resumeDaemon();
  }
</script>

<header class="flex items-stretch border-b-2 border-border bg-surface sticky top-0 z-50" style="height:40px">
  <!-- Brand -->
  <button
    class="px-4 flex items-center border-r-2 border-border text-accent font-bold tracking-[0.3em] text-sm
           hover:bg-accent hover:text-bg transition-colors shrink-0"
    onclick={() => setActivePanel('tickets')}
    aria-label="Go to tickets"
  >FOREMAN</button>

  <!-- Status chips -->
  <div class="flex items-center gap-3 px-3 text-xs flex-1 overflow-hidden">
    <!-- Daemon state -->
    <span class="flex items-center gap-1.5 shrink-0">
      <span class="w-1.5 h-1.5 {dotBg} {appState.daemonState === 'running' && appState.wsConnected ? 'animate-pulse' : ''}"></span>
      <span class="text-text">{label}</span>
    </span>

    <!-- WA status -->
    {#if appState.whatsapp !== null}
      <span class="text-border-strong hidden md:inline">·</span>
      <span class="flex items-center gap-1.5 shrink-0">
        <span class="w-1.5 h-1.5 {appState.whatsapp ? 'bg-success' : 'bg-danger'}"></span>
        <span class="{appState.whatsapp ? 'text-muted' : 'text-danger'}">WA</span>
      </span>
    {/if}

    <!-- Cost -->
    <span class="text-border-strong hidden md:inline">·</span>
    <span class="hidden md:inline shrink-0 {costOverBudget ? 'text-danger' : 'text-muted'}">
      {costLabel}
    </span>

    <!-- Active count -->
    {#if appState.activeCount > 0}
      <span class="text-border-strong hidden md:inline">·</span>
      <span class="hidden md:inline text-muted shrink-0">
        <span class="text-accent">{appState.activeCount}</span> ACTIVE
      </span>
    {/if}
  </div>

  <!-- Actions -->
  <div class="flex items-stretch border-l-2 border-border text-xs shrink-0">
    {#if appState.daemonState === 'running'}
      <button
        class="px-3 text-warning border-r border-border hover:bg-warning hover:text-bg transition-colors tracking-wider"
        onclick={handlePause}
      >PAUSE</button>
    {:else}
      <button
        class="px-3 text-success border-r border-border hover:bg-success hover:text-bg transition-colors tracking-wider"
        onclick={handleResume}
      >START</button>
    {/if}
    <button
      class="px-3 text-muted hover:text-accent hover:bg-surface-hover transition-colors tracking-wider hidden md:block"
      onclick={() => setActivePanel('health')}
    >SYS</button>
  </div>
</header>
