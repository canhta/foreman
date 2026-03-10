<script lang="ts">
  import { push } from 'svelte-spa-router';
</script>

<div class="flex flex-col items-center justify-center h-full p-12 text-center">
  <div class="text-[var(--color-muted)] text-[10px] tracking-[0.4em] uppercase mb-2">404</div>
  <div class="text-sm font-bold text-[var(--color-text)] mb-6">Page not found</div>
  <button
    onclick={() => push('/')}
    class="text-[10px] tracking-widest text-[var(--color-accent)] hover:underline"
  >
    ← Back to overview
  </button>
</div>
