<script lang="ts">
  import Header from './components/Header.svelte';
  let message = $state('FOREMAN');
</script>

<div class="min-h-screen bg-bg text-text font-mono">
  <Header />
  <div class="flex items-center justify-center h-96">
    <h1 class="text-accent text-2xl font-bold">{message}</h1>
  </div>
</div>
