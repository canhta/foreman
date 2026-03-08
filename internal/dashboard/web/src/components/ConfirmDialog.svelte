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
    confirmClass = 'bg-accent text-bg hover:bg-text',
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
    class="fixed inset-0 z-[100] flex items-start justify-center pt-16 sm:pt-24 px-4 bg-bg/80"
    onclick={oncancel}
    role="presentation"
  >
    <!-- Dialog box -->
    <div
      class="relative bg-surface border-2 border-border w-full max-w-sm font-mono"
      onclick={(e) => e.stopPropagation()}
      onkeydown={(e) => e.stopPropagation()}
      role="dialog"
      tabindex="-1"
      aria-modal="true"
      aria-labelledby="confirm-title"
    >
      <!-- Title bar -->
      <div class="px-4 py-2.5 border-b-2 border-border bg-surface-active">
        <span id="confirm-title" class="text-xs font-bold tracking-[0.2em] text-text">{title}</span>
      </div>

      <!-- Body -->
      {#if message}
        <div class="px-4 py-3 text-xs text-muted leading-relaxed">{message}</div>
      {/if}

      <!-- Actions -->
      <div class="flex border-t-2 border-border">
        <button
          class="flex-1 py-3 text-xs font-bold tracking-wider text-muted hover:text-text hover:bg-surface-hover transition-colors border-r border-border"
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
