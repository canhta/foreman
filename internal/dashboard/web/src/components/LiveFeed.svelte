<script lang="ts">
  import { projectState } from '../state/project.svelte';
  import { formatTime, severityIcon, runnerBadgeCls, shortModel, linkifyParts } from '../format';

  let feedCollapsed = $state(localStorage.getItem('feed_collapsed') === 'true');

  function toggleCollapse() {
    feedCollapsed = !feedCollapsed;
    localStorage.setItem('feed_collapsed', String(feedCollapsed));
  }

  function eventTypeCls(eventType: string): string {
    if (!eventType) return 'text-muted-bright';
    if (eventType.includes('fail') || eventType.includes('error')) return 'text-danger';
    if (eventType.includes('complet') || eventType.includes('done') || eventType.includes('pass') || eventType.includes('merg')) return 'text-success';
    if (eventType.includes('start') || eventType.includes('creat') || eventType.includes('new')) return 'text-accent';
    return 'text-muted-bright';
  }
</script>

<section
  class="flex flex-col h-full bg-surface {feedCollapsed ? 'w-8' : 'w-72'} transition-[width] duration-200"
  aria-label="Live event feed"
>
  <!-- Header -->
  <div class="flex items-center px-2 border-b-2 border-border" style="min-height:40px">
    {#if !feedCollapsed}
      <span class="text-[10px] font-bold tracking-[0.2em] text-text flex-1">LIVE FEED</span>
    {/if}
    <button
      class="text-muted hover:text-accent transition-colors text-xs {feedCollapsed ? 'mx-auto' : ''}"
      onclick={toggleCollapse}
      aria-label={feedCollapsed ? 'Expand feed' : 'Collapse feed'}
    >{feedCollapsed ? '▶' : '◀'}</button>
  </div>

  {#if !feedCollapsed}
    <div class="flex-1 overflow-y-auto">
      {#each projectState.events as evt (evt.ID)}
        <div class="px-2 py-2 border-b border-border text-[10px] hover:bg-surface-hover transition-colors">

          <!-- Type + time -->
          <div class="flex items-center gap-1.5 leading-tight">
            <span class="{
              evt.Severity === 'success' ? 'text-success' :
              evt.Severity === 'error' ? 'text-danger' :
              evt.Severity === 'warning' ? 'text-warning' :
              'text-muted'
            }">{severityIcon(evt.Severity)}</span>
            <span class="font-bold {eventTypeCls(evt.EventType)} truncate flex-1">
              {evt.EventType || '—'}
            </span>
            <span class="text-muted shrink-0 tabular-nums">{formatTime(evt.CreatedAt)}</span>
          </div>

          <!-- Message -->
          {#if evt.Message}
            <div class="text-text/50 mt-0.5 pl-3.5 break-words">
              {#each linkifyParts(evt.Message) as part}
                {#if part.type === 'url'}
                  <a href={part.content} target="_blank" rel="noopener noreferrer"
                    class="text-accent hover:underline break-all">{part.content}</a>
                {:else}
                  {part.content}
                {/if}
              {/each}
            </div>
          {/if}

          <!-- Ticket link -->
          {#if evt.TicketID}
            <div class="pl-3.5 mt-0.5">
              <button
                class="text-accent/60 hover:text-accent transition-colors truncate cursor-pointer block max-w-full"
                onclick={() => projectState.loadTicketDetail(evt.TicketID)}
              >{evt.TicketID}</button>
            </div>
          {/if}
        </div>
      {/each}

      {#if projectState.events.length === 0}
        <div class="px-2 py-4 text-center text-muted-bright text-[10px] tracking-wider">WAITING...</div>
      {/if}
    </div>
  {:else}
    <!-- Collapsed: severity column -->
    <div class="flex flex-col items-center gap-0.5 py-2 overflow-hidden flex-1">
      {#each projectState.events.slice(0, 60) as evt (evt.ID)}
        <span class="w-1.5 h-1.5 shrink-0 {
          evt.Severity === 'success' ? 'bg-success' :
          evt.Severity === 'error' ? 'bg-danger' :
          evt.Severity === 'warning' ? 'bg-warning' :
          'bg-border-strong'
        }"></span>
      {/each}
    </div>
  {/if}
</section>
