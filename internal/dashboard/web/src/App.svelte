<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import { setToken, getToken } from './api';
  import {
    appState,
    startPolling, stopPolling, connectWebSocket, restoreFromURL,
    setActivePanel,
  } from './state.svelte';
  import Header from './components/Header.svelte';
  import TicketList from './components/TicketList.svelte';
  import TicketDetail from './components/TicketDetail.svelte';
  import TeamSummary from './components/TeamSummary.svelte';
  import SystemHealth from './components/SystemHealth.svelte';
  import LiveFeed from './components/LiveFeed.svelte';
  import Toasts from './components/Toasts.svelte';

  let authenticated = $state(!!getToken());
  let tokenInput = $state('');

  function handleAuth() {
    if (tokenInput.trim()) {
      setToken(tokenInput.trim());
      authenticated = true;
    }
  }

  onMount(() => {
    if (authenticated) {
      startPolling();
      connectWebSocket();
      restoreFromURL();
    }
    window.addEventListener('popstate', restoreFromURL);
  });

  onDestroy(() => {
    stopPolling();
    window.removeEventListener('popstate', restoreFromURL);
  });

  function handleKeydown(e: KeyboardEvent) {
    if (e.target instanceof HTMLInputElement || e.target instanceof HTMLTextAreaElement) return;
    if (e.key === '?' && !e.ctrlKey && !e.metaKey) {
      // TODO: show keyboard shortcut overlay
    }
  }
</script>

<svelte:window onkeydown={handleKeydown} />

{#if !authenticated}
  <!-- Auth gate -->
  <div class="min-h-screen bg-bg text-text font-mono flex items-center justify-center">
    <div class="border border-border p-6 bg-surface w-80 space-y-4">
      <h1 class="text-accent font-bold text-lg text-center">FOREMAN</h1>
      <input
        type="password"
        bind:value={tokenInput}
        placeholder="Enter auth token..."
        class="w-full bg-bg border border-border px-3 py-2 text-sm text-text placeholder:text-muted focus:border-accent outline-none"
        onkeydown={(e) => e.key === 'Enter' && handleAuth()}
      />
      <button
        class="w-full py-2 bg-accent text-bg font-bold text-sm hover:opacity-90"
        onclick={handleAuth}
      >AUTHENTICATE</button>
    </div>
  </div>
{:else}
  <div class="min-h-screen bg-bg text-text font-mono flex flex-col">
    <Header />

    <main class="flex-1 flex overflow-hidden">
      <!-- Left: Ticket List (always visible on desktop) -->
      <div class="hidden md:flex w-64 shrink-0">
        <TicketList />
      </div>
      <!-- Mobile: show based on activePanel -->
      <div class="flex md:hidden w-full {appState.activePanel === 'tickets' ? '' : 'hidden'}">
        <TicketList />
      </div>

      <!-- Center: Detail / Team Summary / System Health (always visible on desktop) -->
      <div class="hidden md:flex flex-1 min-w-0">
        {#if appState.activePanel === 'health'}
          <SystemHealth />
        {:else if appState.selectedTicketId}
          <TicketDetail />
        {:else}
          <TeamSummary />
        {/if}
      </div>
      <div class="flex md:hidden w-full {appState.activePanel === 'detail' || appState.activePanel === 'health' ? '' : 'hidden'}">
        {#if appState.activePanel === 'health'}
          <SystemHealth />
        {:else if appState.selectedTicketId}
          <TicketDetail />
        {:else}
          <TeamSummary />
        {/if}
      </div>

      <!-- Right: Live Feed (always on desktop) -->
      <div class="hidden md:flex shrink-0">
        <LiveFeed />
      </div>
      <div class="flex md:hidden w-full {appState.activePanel === 'feed' ? '' : 'hidden'}">
        <LiveFeed />
      </div>
    </main>

    <!-- Mobile tab bar -->
    <nav class="flex md:hidden border-t border-border bg-surface" aria-label="Navigation">
      {#each [
        { key: 'tickets', icon: '\u2630', label: 'TICKETS' },
        { key: 'detail', icon: '\u25B6', label: 'DETAIL' },
        { key: 'feed', icon: '\u26A1', label: 'FEED' },
        { key: 'health', icon: '\u2699', label: 'SYSTEM' },
      ] as tab}
        <button
          class="flex-1 py-2 text-center text-xs {appState.activePanel === tab.key ? 'text-accent border-t-2 border-accent' : 'text-muted'}"
          onclick={() => setActivePanel(tab.key as any)}
        >{tab.icon}<br>{tab.label}</button>
      {/each}
    </nav>

    <!-- Footer -->
    <footer class="hidden md:flex items-center justify-center gap-4 px-4 py-1 border-t border-border text-xs text-muted bg-surface">
      <span>DAILY: {appState.dailyCost.toFixed(2)} / ${appState.dailyBudget.toFixed(0)}</span>
      <span>|</span>
      <span>WEEKLY: ${appState.weeklyCost.toFixed(2)}</span>
      <span>|</span>
      <span>MONTHLY: ${appState.monthlyCost.toFixed(2)} / ${appState.monthlyBudget.toFixed(0)}</span>
    </footer>

    <Toasts />
  </div>
{/if}
