<script lang="ts">
  import { toasts } from '../state/toasts.svelte';
  import { projectState } from '../state/project.svelte';
  import { IconCheck, IconX, IconAlertTriangle, IconCircleFilled } from '@tabler/icons-svelte';
</script>

{#if toasts.toasts.length > 0}
  <div class="fixed bottom-16 right-4 z-50 space-y-1.5 md:bottom-4" role="alert" aria-live="polite">
    {#each toasts.toasts as toast (toast.id)}
      <div class="flex items-stretch animate-slide-in max-w-sm border border-[var(--color-border-strong)] bg-[var(--color-surface)] shadow-2xl transition-colors hover:border-[var(--color-border-bright)]
        {toast.severity === 'error' ? 'border-l-4 border-l-[var(--color-danger)]' : toast.severity === 'warning' ? 'border-l-4 border-l-[var(--color-warning)]' : toast.severity === 'info' ? 'border-l-4 border-l-[var(--color-accent)]' : 'border-l-4 border-l-[var(--color-success)]'}">
        <!-- Icon block -->
        <div class="w-10 flex items-center justify-center shrink-0 {toast.severity === 'error' ? 'bg-[var(--color-danger-bg)]' : toast.severity === 'warning' ? 'bg-[var(--color-warning-bg)]' : toast.severity === 'info' ? 'bg-[var(--color-surface-hover)]' : 'bg-[var(--color-success-bg)]'}">
          <span class="{toast.severity === 'error' ? 'text-[var(--color-danger)]' : toast.severity === 'warning' ? 'text-[var(--color-warning)]' : toast.severity === 'info' ? 'text-[var(--color-accent)]' : 'text-[var(--color-success)]'}">
            {#if toast.severity === 'success'}<IconCheck size={16} stroke={1.5} />
            {:else if toast.severity === 'error'}<IconX size={16} stroke={1.5} />
            {:else if toast.severity === 'warning'}<IconAlertTriangle size={16} stroke={1.5} />
            {:else}<IconCircleFilled size={16} stroke={1.5} />{/if}
          </span>
        </div>

        <!-- Message -->
        <div class="px-3 py-2 flex-1 min-w-0 flex items-center">
          {#if toast.ticketId}
            <button
              class="text-xs text-[var(--color-text)] hover:text-[var(--color-accent)] cursor-pointer truncate text-left transition-colors"
              onclick={() => projectState.loadTicketDetail(toast.ticketId!)}
            >{toast.message}</button>
          {:else}
            <span class="text-xs text-[var(--color-text)] truncate">{toast.message}</span>
          {/if}
        </div>
      </div>
    {/each}
  </div>
{/if}
