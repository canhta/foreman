<script lang="ts">
  import { toasts, selectTicket } from '../state.svelte';
  import { severityIcon } from '../format';
</script>

{#if toasts.length > 0}
  <div class="fixed bottom-16 right-4 z-50 space-y-2 md:bottom-4" role="alert">
    {#each toasts as toast (toast.id)}
      <div
        class="flex items-center gap-2 px-3 py-2 bg-surface border border-border text-xs shadow-lg
          animate-fade-in max-w-xs
          {toast.severity === 'error' ? 'border-l-2 border-l-danger' : 'border-l-2 border-l-success'}"
      >
        <span class="{toast.severity === 'error' ? 'text-danger' : 'text-success'}">
          {severityIcon(toast.severity)}
        </span>
        {#if toast.ticketId}
          <button
            class="text-text hover:text-accent cursor-pointer"
            onclick={() => selectTicket(toast.ticketId!)}
          >{toast.message}</button>
        {:else}
          <span>{toast.message}</span>
        {/if}
      </div>
    {/each}
  </div>
{/if}
