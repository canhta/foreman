<script lang="ts">
  import { appState, selectTicket, setFeedCollapsed } from '../state.svelte';
  import { formatTime, severityIcon, formatSender } from '../format';

  function toggleCollapse() {
    setFeedCollapsed(!appState.feedCollapsed);
  }
</script>

<section class="flex flex-col h-full border-l border-border bg-surface {appState.feedCollapsed ? 'w-8' : 'w-72'}
  transition-[width] duration-200">
  <div class="flex items-center justify-between px-2 py-2 border-b border-border">
    {#if !appState.feedCollapsed}
      <span class="text-xs text-muted font-bold tracking-wider">LIVE FEED</span>
    {/if}
    <button
      class="text-xs text-muted hover:text-accent"
      onclick={toggleCollapse}
      aria-label={appState.feedCollapsed ? 'Expand feed' : 'Collapse feed'}
    >{appState.feedCollapsed ? '\u25B6' : '\u25C0'}</button>
  </div>

  {#if !appState.feedCollapsed}
    <div class="flex-1 overflow-y-auto">
      {#each appState.events as evt (evt.ID)}
        <div class="px-2 py-1.5 border-b border-border text-xs hover:bg-surface-hover
          {evt.isNew ? 'animate-fade-in bg-accent/5' : ''}">
          <div class="flex gap-1.5 items-start">
            <span class="text-muted shrink-0">{formatTime(evt.CreatedAt)}</span>
            <span class="{
              evt.Severity === 'success' ? 'text-success' :
              evt.Severity === 'error' ? 'text-danger' :
              evt.Severity === 'warning' ? 'text-warning' :
              'text-muted'
            }">{severityIcon(evt.Severity)}</span>
            <span class="text-text">{evt.EventType}</span>
          </div>
          {#if evt.Message}
            <div class="text-muted ml-5 truncate">{evt.Message}</div>
          {/if}
          {#if evt.ticket_title}
            <div class="ml-5 mt-0.5">
              <button
                class="text-accent/70 hover:text-accent text-xs cursor-pointer"
                onclick={() => selectTicket(evt.TicketID)}
              >[{evt.ticket_title}]</button>
              <span class="text-muted">{formatSender(evt.submitter || '')}</span>
            </div>
          {/if}
        </div>
      {/each}
    </div>
  {:else}
    <!-- Collapsed: severity dots -->
    <div class="flex flex-col items-center gap-0.5 py-2 overflow-hidden">
      {#each appState.events.slice(0, 50) as evt (evt.ID)}
        <span class="w-1.5 h-1.5 rounded-full {
          evt.Severity === 'success' ? 'bg-success' :
          evt.Severity === 'error' ? 'bg-danger' :
          evt.Severity === 'warning' ? 'bg-warning' :
          'bg-muted'
        }"></span>
      {/each}
    </div>
  {/if}
</section>
