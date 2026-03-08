<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import { setToken } from './api';
  import {
    appState,
    startPolling, stopPolling, connectWebSocket, restoreFromURL,
    setActivePanel, logout,
  } from './state.svelte';
  import Header from './components/Header.svelte';
  import TicketList from './components/TicketList.svelte';
  import TicketDetail from './components/TicketDetail.svelte';
  import TeamSummary from './components/TeamSummary.svelte';
  import SystemHealth from './components/SystemHealth.svelte';
  import LiveFeed from './components/LiveFeed.svelte';
  import Toasts from './components/Toasts.svelte';

  let tokenInput = $state('');
  let sessionExpired = $state(false);

  // Track if we were previously authenticated (to detect 401 logout)
  let wasAuthenticated = false;
  $effect(() => {
    if (wasAuthenticated && !appState.authenticated) {
      sessionExpired = true;
    }
    wasAuthenticated = appState.authenticated;
  });

  function handleAuth() {
    if (tokenInput.trim()) {
      setToken(tokenInput.trim());
      appState.authenticated = true;
      sessionExpired = false;
      startPolling();
      connectWebSocket();
      restoreFromURL();
    }
  }

  onMount(() => {
    if (appState.authenticated) {
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

{#if !appState.authenticated}
  <!-- Auth gate -->
  <div class="min-h-screen bg-bg text-text font-mono flex items-center justify-center">
    <!-- Grid lines backdrop -->
    <div class="absolute inset-0 opacity-[0.03]"
      style="background-image: linear-gradient(var(--color-text) 1px, transparent 1px), linear-gradient(90deg, var(--color-text) 1px, transparent 1px); background-size: 40px 40px;">
    </div>

    <div class="relative border-2 border-border bg-surface w-80">
      <!-- Header bar -->
      <div class="bg-accent px-4 py-3 flex items-center justify-between">
        <span class="text-bg font-bold text-lg tracking-[0.3em]">FOREMAN</span>
        <span class="text-bg/60 text-xs">SECURE ACCESS</span>
      </div>

      <div class="p-5 space-y-4">
        {#if sessionExpired}
          <div class="border-2 border-danger bg-danger-bg px-3 py-2 text-xs text-danger tracking-wider">
            SESSION EXPIRED — PLEASE RE-AUTHENTICATE
          </div>
        {/if}
        <div class="text-xs text-muted-bright tracking-wider">AUTHENTICATION REQUIRED</div>
        <div class="space-y-2">
          <label for="auth-token" class="text-xs text-muted-bright block">AUTH TOKEN</label>
          <input
            id="auth-token"
            type="password"
            bind:value={tokenInput}
            placeholder="Enter token..."
            class="w-full bg-bg border-2 border-border px-3 py-2 text-sm text-text placeholder:text-muted focus:border-accent outline-none transition-colors"
            onkeydown={(e) => e.key === 'Enter' && handleAuth()}
          />
        </div>
        <button
          class="w-full py-2.5 bg-accent text-bg font-bold text-sm tracking-widest hover:bg-text hover:text-bg transition-colors"
          onclick={handleAuth}
        >AUTHENTICATE</button>
        <div class="text-xs text-muted text-center">Foreman Operator Dashboard</div>
      </div>
    </div>
  </div>
{:else}
  <div class="min-h-screen bg-bg text-text font-mono flex flex-col">
    <Header />

    <main class="flex-1 flex overflow-hidden">
      <!-- Left: Ticket List (hidden on mobile, visible tablet+) -->
      <div class="hidden md:flex w-56 lg:w-64 shrink-0 border-r-2 border-border">
        <TicketList />
      </div>
      <div class="flex md:hidden w-full {appState.activePanel === 'tickets' ? '' : 'hidden'}">
        <TicketList />
      </div>

      <!-- Center: Detail / Summary / Health (hidden on mobile, visible tablet+) -->
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

      <!-- Right: Live Feed (hidden on mobile+tablet, visible desktop only) -->
      <div class="hidden lg:flex shrink-0 border-l-2 border-border">
        <LiveFeed />
      </div>
      <div class="flex lg:hidden w-full {appState.activePanel === 'feed' ? '' : 'hidden'}">
        <LiveFeed />
      </div>
    </main>

    <!-- Mobile/tablet tab bar (hidden on desktop for feed, always visible below lg for feed access) -->
    <nav class="flex lg:hidden border-t-2 border-border bg-surface" aria-label="Navigation">
      {#each [
        { key: 'tickets', icon: '☰', label: 'TICKETS', hideTablet: true },
        { key: 'detail', icon: '▶', label: 'DETAIL', hideTablet: true },
        { key: 'feed', icon: '⚡', label: 'FEED', hideTablet: false },
        { key: 'health', icon: '⚙', label: 'SYSTEM', hideTablet: true },
      ] as tab}
        <button
          class="flex-1 py-2.5 text-center text-xs tracking-wider transition-colors
            {tab.hideTablet ? 'md:hidden' : ''}
            {appState.activePanel === tab.key
              ? 'text-bg bg-accent font-bold'
              : 'text-muted hover:text-text hover:bg-surface-hover'}"
          onclick={() => setActivePanel(tab.key as any)}
        >
          <div class="text-base leading-none">{tab.icon}</div>
          <div class="mt-0.5">{tab.label}</div>
        </button>
      {/each}
    </nav>

    <!-- Footer -->
    <footer class="hidden md:flex items-center gap-0 border-t-2 border-border bg-surface text-xs">
      <span class="px-3 py-1.5 border-r border-border text-muted-bright">
        DAILY: <span class="{appState.dailyBudget > 0 && appState.dailyCost / appState.dailyBudget >= 0.8 ? 'text-danger' : 'text-accent'}">
          ${appState.dailyCost.toFixed(2)}</span>{appState.dailyBudget > 0 ? ` / $${Math.round(appState.dailyBudget)}` : ''}
      </span>
      <span class="px-3 py-1.5 border-r border-border text-muted-bright hidden lg:inline">
        WEEKLY: <span class="text-text">${appState.weeklyCost.toFixed(2)}</span>
      </span>
      <span class="px-3 py-1.5 text-muted-bright hidden lg:inline">
        MONTHLY: <span class="text-text">${appState.monthlyCost.toFixed(2)}</span>
        {#if appState.monthlyBudget > 0}<span class="text-muted-bright"> / ${Math.round(appState.monthlyBudget)}</span>{/if}
      </span>
      <div class="flex-1"></div>
      <span class="px-3 py-1.5 border-l border-border {appState.wsConnected ? 'text-success' : 'text-danger'}">
        <span class="w-1.5 h-1.5 inline-block mr-1 {appState.wsConnected ? 'bg-success' : 'bg-danger'}"></span>
        {appState.wsConnected ? 'LIVE' : 'OFFLINE'}
      </span>
    </footer>

    <Toasts />
  </div>
{/if}
