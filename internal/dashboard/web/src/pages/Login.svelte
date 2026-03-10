<script lang="ts">
  interface Props {
    onLogin: (token: string) => void;
  }
  let { onLogin }: Props = $props();

  let tokenInput = $state('');
  let error = $state('');

  async function handleSubmit() {
    error = '';
    try {
      const res = await fetch('/api/status', {
        headers: { Authorization: `Bearer ${tokenInput}` },
      });
      if (res.ok) {
        onLogin(tokenInput);
      } else {
        error = 'Invalid token';
      }
    } catch {
      error = 'Connection failed';
    }
  }
</script>

<div class="min-h-screen flex items-center justify-center bg-[var(--color-bg)] relative overflow-hidden">
  <!-- Grid backdrop -->
  <div class="absolute inset-0 opacity-[0.03]"
       style="background-image:
         linear-gradient(var(--color-accent) 1px, transparent 1px),
         linear-gradient(90deg, var(--color-accent) 1px, transparent 1px);
       background-size: 40px 40px;">
  </div>

  <!-- Scan line texture -->
  <div class="absolute inset-0 pointer-events-none opacity-[0.02]"
       style="background: repeating-linear-gradient(
         0deg, transparent, transparent 2px, var(--color-text) 2px, var(--color-text) 3px
       );">
  </div>

  <div class="w-80 border-2 border-[var(--color-border)] p-6 relative z-10 bg-[var(--color-bg)]
              shadow-[0_0_40px_rgba(255,230,0,0.03)]">
    <!-- Yellow header bar -->
    <div class="h-1 bg-[var(--color-accent)] -mx-6 -mt-6 mb-6"></div>
    <h1 class="text-sm font-bold tracking-[0.3em] text-[var(--color-accent)] mb-6">FOREMAN</h1>
    <form onsubmit={(e) => { e.preventDefault(); handleSubmit(); }}>
      <input
        type="password"
        bind:value={tokenInput}
        placeholder="ACCESS TOKEN"
        class="w-full bg-[var(--color-surface)] border border-[var(--color-border)] px-3 py-2 text-xs tracking-wider text-[var(--color-text)] placeholder-[var(--color-muted)] focus:border-[var(--color-accent)] focus:outline-none"
      />
      {#if error}
        <p class="text-[var(--color-danger)] text-xs mt-2">{error}</p>
      {/if}
      <button
        type="submit"
        class="w-full mt-4 bg-[var(--color-accent)] text-[var(--color-bg)] py-2 text-xs font-bold tracking-widest hover:opacity-90"
      >
        AUTHENTICATE
      </button>
    </form>
  </div>
</div>
