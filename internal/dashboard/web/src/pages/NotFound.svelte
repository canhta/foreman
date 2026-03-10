<script lang="ts">
  import { push } from 'svelte-spa-router';
</script>

<div class="flex flex-col items-center justify-center h-full p-12 text-center animate-[fade-in_0.2s_ease-out]">
  <div class="text-6xl font-bold text-[var(--color-muted)] tracking-[0.3em]">404</div>
  <div class="text-[10px] uppercase tracking-[0.4em] text-[var(--color-muted)] mt-2">NOT FOUND</div>
  <div class="text-sm font-bold text-[var(--color-text)] mt-2">Page not found</div>
  <div class="w-16 h-px bg-[var(--color-border-bright)] my-6"></div>
  <button
    onclick={() => push('/')}
    class="text-xs tracking-widest text-[var(--color-accent)] hover:text-[var(--color-text)] transition-colors border border-[var(--color-border)] px-4 py-2 hover:border-[var(--color-border-strong)]"
  >
    ← Back to overview
  </button>
</div>
