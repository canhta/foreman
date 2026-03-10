<script lang="ts">
  interface Props {
    onLogin: (token: string) => void;
  }
  let { onLogin }: Props = $props();

  let tokenInput = $state('');
  let error = $state('');
  let loading = $state(false);

  async function handleSubmit() {
    error = '';
    loading = true;
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
    } finally {
      loading = false;
    }
  }
</script>

<div class="min-h-screen flex items-center justify-center bg-[var(--color-bg)] relative overflow-hidden">

  <!-- Grid backdrop — more visible -->
  <div class="absolute inset-0 pointer-events-none"
       style="background-image:
         linear-gradient(var(--color-border) 1px, transparent 1px),
         linear-gradient(90deg, var(--color-border) 1px, transparent 1px);
       background-size: 48px 48px; opacity: 0.5;">
  </div>

  <!-- Subtle radial vignette from center -->
  <div class="absolute inset-0 pointer-events-none"
       style="background: radial-gradient(ellipse 60% 60% at 50% 50%, transparent 0%, var(--color-bg) 100%); opacity: 0.7;">
  </div>

  <!-- Scan lines -->
  <div class="absolute inset-0 pointer-events-none"
       style="background: repeating-linear-gradient(
         0deg, transparent, transparent 3px, rgba(255,255,255,0.012) 3px, rgba(255,255,255,0.012) 4px
       );">
  </div>

  <!-- Glow behind card -->
  <div class="absolute w-80 h-80 rounded-full pointer-events-none"
       style="background: radial-gradient(circle, rgba(255,230,0,0.04) 0%, transparent 70%); filter: blur(40px);">
  </div>

  <!-- Card -->
  <div class="w-80 border border-[var(--color-border-strong)] relative z-10 bg-[var(--color-surface)]
              shadow-[0_0_0_1px_rgba(255,230,0,0.04),0_24px_64px_rgba(0,0,0,0.6)]
              animate-[zoom-in_0.2s_ease-out]">

    <!-- Top accent bar -->
    <div class="h-0.5 bg-[var(--color-accent)] w-full"></div>

    <div class="p-8">
      <!-- Logo -->
      <div class="mb-8">
        <div class="text-lg font-bold tracking-[0.3em] text-[var(--color-accent)] leading-none mb-1">
          FOREMAN
        </div>
        <div class="text-[10px] text-[var(--color-muted)] tracking-[0.2em] uppercase">
          Autonomous Dev Agent
        </div>
      </div>

      <!-- Form -->
      <form onsubmit={(e) => { e.preventDefault(); handleSubmit(); }} class="space-y-4">
        <div class="space-y-1.5">
          <label for="token-input" class="text-[10px] tracking-[0.15em] text-[var(--color-muted)] uppercase block">
            Access Token
          </label>
          <input
            id="token-input"
            type="password"
            bind:value={tokenInput}
            placeholder="••••••••••••••••"
            autocomplete="current-password"
            class="w-full bg-[var(--color-bg)] border border-[var(--color-border-strong)] px-3 py-2.5 text-xs
                   tracking-wider text-[var(--color-text)] placeholder-[var(--color-muted)]
                   focus:border-[var(--color-accent)] focus:outline-none transition-colors"
          />
          {#if error}
            <p class="text-[var(--color-danger)] text-[10px] tracking-wider">{error}</p>
          {/if}
        </div>

        <button
          type="submit"
          disabled={loading || !tokenInput.trim()}
          class="w-full bg-[var(--color-accent)] text-[var(--color-bg)] py-2.5 text-xs font-bold
                 tracking-[0.2em] uppercase hover:opacity-90 transition-opacity disabled:opacity-50"
        >
          {loading ? 'Authenticating…' : 'Authenticate'}
        </button>
      </form>
    </div>
  </div>
</div>
