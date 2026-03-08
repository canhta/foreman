<script lang="ts">
  import { appState, selectTicket } from '../state.svelte';
  import { severityIcon } from '../format';
</script>

{#if appState.toasts.length > 0}
  <div class="fixed bottom-16 right-4 z-50 space-y-1.5 md:bottom-4" role="alert" aria-live="polite">
    {#each appState.toasts as toast (toast.id)}
      <div class="flex items-stretch animate-slide-in max-w-xs border-2 border-border bg-surface shadow-2xl
        {toast.severity === 'error' ? 'border-l-4 border-l-danger' : 'border-l-4 border-l-success'}">
        <!-- Icon block -->
        <div class="px-2 py-2 flex items-center {toast.severity === 'error' ? 'bg-danger-bg' : 'bg-success-bg'}">
          <span class="text-sm {toast.severity === 'error' ? 'text-danger' : 'text-success'}">
            {severityIcon(toast.severity)}
          </span>
        </div>

        <!-- Message -->
        <div class="px-3 py-2 flex-1 min-w-0 flex items-center">
          {#if toast.ticketId}
            <button
              class="text-xs text-text hover:text-accent cursor-pointer truncate text-left transition-colors"
              onclick={() => selectTicket(toast.ticketId!)}
            >{toast.message}</button>
          {:else}
            <span class="text-xs text-text truncate">{toast.message}</span>
          {/if}
        </div>
      </div>
    {/each}
  </div>
{/if}
