<script lang="ts">
  import Router from 'svelte-spa-router';
  import routes from './routes';
  import Sidebar from './components/Sidebar.svelte';
  import Login from './pages/Login.svelte';
  import Toasts from './components/Toasts.svelte';
  import { globalState } from './state/global.svelte';
  import { setToken, getToken } from './api';

  function handleLogin(token: string) {
    setToken(token);
    globalState.authenticated = true;
    globalState.startPolling();
  }

  // Auto-start polling if already authenticated
  if (getToken()) {
    globalState.startPolling();
  }
</script>

{#if !globalState.authenticated}
  <Login onLogin={handleLogin} />
{:else}
  <div class="flex h-screen bg-[var(--color-bg)] text-[var(--color-text)]">
    <Sidebar />
    <main class="flex-1 overflow-y-auto">
      <Router {routes} />
    </main>
  </div>
  <Toasts />
{/if}
