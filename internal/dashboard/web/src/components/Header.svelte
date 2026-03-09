<script lang="ts">
  import {
    appState, pauseDaemon, resumeDaemon, setActivePanel, syncTracker,
  } from '../state.svelte';
  import { formatCost } from '../format';
  import ConfirmDialog from './ConfirmDialog.svelte';

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

  let confirmDialog = $state<{ open: boolean; title: string; message: string; confirmLabel: string; confirmClass: string; action: () => void }>({
    open: false, title: '', message: '', confirmLabel: 'CONFIRM', confirmClass: 'bg-accent text-bg hover:bg-text', action: () => {},
  });

  function handlePause() {
    confirmDialog = {
      open: true,
      title: 'PAUSE DAEMON',
      message: 'Stop processing tickets until manually resumed?',
      confirmLabel: 'PAUSE',
      confirmClass: 'bg-warning text-bg hover:bg-text',
      action: pauseDaemon,
    };
  }
  function handleResume() {
    confirmDialog = {
      open: true,
      title: 'START DAEMON',
      message: 'Resume processing tickets?',
      confirmLabel: 'START',
      confirmClass: 'bg-success text-bg hover:bg-text',
      action: resumeDaemon,
    };
  }
  function closeDialog() { confirmDialog = { ...confirmDialog, open: false }; }
  function runDialog() { confirmDialog.action(); closeDialog(); }

  // ── Sync state ──
  type SyncPhase = 'idle' | 'syncing' | 'done' | 'err';
  let syncPhase = $state<SyncPhase>('idle');
  let syncDots = $state(1);
  let dotTimer: ReturnType<typeof setInterval> | null = null;

  let syncLabel = $derived(
    syncPhase === 'syncing' ? 'SYNC' + '·'.repeat(syncDots) :
    syncPhase === 'done'    ? 'DONE' :
    syncPhase === 'err'     ? 'ERR'  :
    'SYNC'
  );

  let syncClass = $derived(
    syncPhase === 'done'    ? 'bg-success text-bg cursor-default' :
    syncPhase === 'err'     ? 'bg-danger text-bg cursor-default'  :
    syncPhase === 'syncing' ? 'bg-accent text-bg cursor-wait'     :
    'text-accent hover:bg-accent hover:text-bg'
  );

  async function handleSync() {
    if (syncPhase === 'syncing') return;
    syncPhase = 'syncing';
    syncDots = 1;
    dotTimer = setInterval(() => {
      syncDots = (syncDots % 3) + 1;
    }, 320);

    try {
      await syncTracker();
      clearInterval(dotTimer!);
      dotTimer = null;
      syncPhase = 'done';
      setTimeout(() => { syncPhase = 'idle'; }, 1400);
    } catch {
      clearInterval(dotTimer!);
      dotTimer = null;
      syncPhase = 'err';
      setTimeout(() => { syncPhase = 'idle'; }, 1400);
    }
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

    <!-- Sync button -->
    <button
      class="px-3 border-r border-border transition-colors tracking-wider min-w-[4rem] tabular-nums {syncClass}"
      onclick={handleSync}
      disabled={syncPhase === 'syncing'}
      title="Force-pull tickets from tracker"
      aria-label="Sync tickets from tracker"
    >{syncLabel}</button>

    <button
      class="px-3 text-muted hover:text-accent hover:bg-surface-hover transition-colors tracking-wider hidden md:block"
      onclick={() => setActivePanel('health')}
    >SYS</button>
  </div>
</header>

<ConfirmDialog
  open={confirmDialog.open}
  title={confirmDialog.title}
  message={confirmDialog.message}
  confirmLabel={confirmDialog.confirmLabel}
  confirmClass={confirmDialog.confirmClass}
  onconfirm={runDialog}
  oncancel={closeDialog}
/>
