<script lang="ts">
  import { projectState } from '../state/project.svelte';
  import { formatTime, severityIcon, linkifyParts } from '../format';

  let feedCollapsed = $state(localStorage.getItem('feed_collapsed') === 'true');

  function toggleCollapse() {
    feedCollapsed = !feedCollapsed;
    localStorage.setItem('feed_collapsed', String(feedCollapsed));
  }

  function severityCls(sev: string): string {
    if (sev === 'success') return 'text-[var(--color-success)]';
    if (sev === 'error')   return 'text-[var(--color-danger)]';
    if (sev === 'warning') return 'text-[var(--color-warning)]';
    return 'text-[var(--color-muted-bright)]';
  }

  function eventTypeCls(eventType: string): string {
    if (!eventType) return 'text-[var(--color-muted-bright)]';
    if (eventType.includes('fail') || eventType.includes('error'))  return 'text-[var(--color-danger)]';
    if (eventType.includes('complet') || eventType.includes('done') || eventType.includes('pass') || eventType.includes('merg'))
      return 'text-[var(--color-success)]';
    if (eventType.includes('start') || eventType.includes('creat') || eventType.includes('new'))
      return 'text-[var(--color-accent)]';
    return 'text-[var(--color-muted-bright)]';
  }
</script>

<section
  class="flex flex-col h-full bg-[var(--color-surface)] border-l border-[var(--color-border)] shrink-0
         {feedCollapsed ? 'w-8' : 'w-[272px]'} transition-[width] duration-200"
  aria-label="Live event feed"
>
  <!-- Header -->
  <div class="flex items-center px-3 border-b border-[var(--color-border)] shrink-0" style="min-height:40px">
    {#if !feedCollapsed}
      <span class="text-[10px] font-bold tracking-[0.2em] text-[var(--color-muted-bright)] uppercase flex-1">Live Feed</span>
    {/if}
    <button
      class="text-[var(--color-muted)] hover:text-[var(--color-accent)] transition-colors text-xs leading-none shrink-0"
      class:mx-auto={feedCollapsed}
      onclick={toggleCollapse}
      aria-label={feedCollapsed ? 'Expand feed' : 'Collapse feed'}
    >{feedCollapsed ? '◁' : '▷'}</button>
  </div>

  {#if !feedCollapsed}
    <div class="flex-1 overflow-y-auto divide-y divide-[var(--color-border)]">
      {#each projectState.events as evt (evt.ID)}
        <div class="px-3 py-2 text-[10px] hover:bg-[var(--color-surface-hover)] transition-colors">

          <!-- Type + time row -->
          <div class="flex items-center gap-1.5 mb-0.5">
            <span class="shrink-0 leading-none {severityCls(evt.Severity)}">{severityIcon(evt.Severity)}</span>
            <span class="font-bold {eventTypeCls(evt.EventType)} truncate flex-1 leading-none">
              {evt.EventType || '—'}
            </span>
            <span class="text-[var(--color-muted)] shrink-0 tabular-nums">{formatTime(evt.CreatedAt)}</span>
          </div>

          <!-- Message -->
          {#if evt.Message}
            <div class="text-[var(--color-muted-bright)] mt-1 pl-3.5 break-words leading-snug">
              {#each linkifyParts(evt.Message) as part}
                {#if part.type === 'url'}
                  <a href={part.content} target="_blank" rel="noopener noreferrer"
                     class="text-[var(--color-accent)] hover:opacity-80 break-all">{part.content}</a>
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
                class="text-[var(--color-accent)]/60 hover:text-[var(--color-accent)] transition-colors truncate cursor-pointer block max-w-full"
                onclick={() => projectState.loadTicketDetail(evt.TicketID)}
              >{evt.TicketID.slice(0, 8)}</button>
            </div>
          {/if}
        </div>
      {/each}

      {#if projectState.events.length === 0}
        <div class="px-3 py-8 text-center text-[var(--color-muted)] text-[10px] tracking-wider">
          Waiting for events…
        </div>
      {/if}
    </div>
  {:else}
    <!-- Collapsed: severity dots -->
    <div class="flex flex-col items-center gap-1 py-2 overflow-hidden flex-1">
      {#each projectState.events.slice(0, 60) as evt (evt.ID)}
        <span class="w-1.5 h-1.5 shrink-0 rounded-none"
              class:bg-[var(--color-success)]={evt.Severity === 'success'}
              class:bg-[var(--color-danger)]={evt.Severity === 'error'}
              class:bg-[var(--color-warning)]={evt.Severity === 'warning'}
              class:bg-[var(--color-border-strong)]={evt.Severity !== 'success' && evt.Severity !== 'error' && evt.Severity !== 'warning'}
        ></span>
      {/each}
    </div>
  {/if}
</section>
