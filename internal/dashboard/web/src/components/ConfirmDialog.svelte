<script lang="ts">
  interface Props {
    open: boolean;
    title: string;
    message?: string;
    confirmLabel?: string;
    confirmClass?: string;
    onconfirm: () => void;
    oncancel: () => void;
  }

  let {
    open,
    title,
    message = '',
    confirmLabel = 'CONFIRM',
    confirmClass = 'bg-[var(--color-accent)] text-[var(--color-bg)] hover:opacity-90',
    onconfirm,
    oncancel,
  }: Props = $props();

  function handleKeydown(e: KeyboardEvent) {
    if (!open) return;
    if (e.key === 'Escape') { oncancel(); e.preventDefault(); }
    if (e.key === 'Enter') { onconfirm(); e.preventDefault(); }
  }
</script>

<svelte:window onkeydown={handleKeydown} />

{#if open}
  <!-- Backdrop -->
  <div
    class="fixed inset-0 z-[100] flex items-start justify-center pt-16 sm:pt-24 px-4 bg-black/60"
    onclick={oncancel}
    role="presentation"
  >
    <!-- Dialog box -->
    <div
      class="relative bg-[var(--color-surface)] border border-[var(--color-border)] w-full max-w-sm font-mono animate-[zoom-in_0.15s_ease-out]"
      onclick={(e) => e.stopPropagation()}
      onkeydown={(e) => e.stopPropagation()}
      role="dialog"
      tabindex="-1"
      aria-modal="true"
      aria-labelledby="confirm-title"
    >
      <!-- Decorative top accent line -->
      <div class="absolute top-0 left-0 right-0 h-0.5 bg-[var(--color-accent)]"></div>

      <!-- Title bar -->
      <div class="px-4 py-2.5 border-b border-[var(--color-border)] bg-[var(--color-surface-active)] mt-0.5">
        <span id="confirm-title" class="text-xs font-bold tracking-[0.2em] text-[var(--color-text)]">{title}</span>
      </div>

      <!-- Body -->
      {#if message}
        <div class="px-4 py-3 text-xs text-[var(--color-muted)] leading-relaxed">{message}</div>
      {/if}

      <!-- Actions -->
      <div class="flex border-t border-[var(--color-border)]">
        <button
          class="flex-1 py-3 text-xs font-bold tracking-wider text-[var(--color-muted)] hover:text-[var(--color-text)] hover:bg-[var(--color-surface-hover)] transition-colors border-r border-[var(--color-border)]"
          onclick={oncancel}
        >CANCEL</button>
        <button
          class="flex-1 py-3 text-xs font-bold tracking-wider transition-colors {confirmClass}"
          onclick={onconfirm}
        >{confirmLabel}</button>
      </div>
    </div>
  </div>
{/if}
