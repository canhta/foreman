<script lang="ts">
  import type { EventRecord, Task } from '../types';
  import { severityIcon, formatRelative, taskIcon } from '../format';

  let { events = [], tasks = [] }: { events: EventRecord[]; tasks: Task[] } = $props();

  let container: HTMLElement;
  let autoScroll = $state(true);

  function handleScroll() {
    if (!container) return;
    const { scrollTop, scrollHeight, clientHeight } = container;
    autoScroll = scrollHeight - scrollTop - clientHeight < 50;
  }

  $effect(() => {
    if (autoScroll && container && events.length) {
      container.scrollTop = 0; // Events prepend, so scroll to top
    }
  });

  function eventLabel(evt: EventRecord): string {
    // Derive human-readable labels from event types
    const type = evt.EventType;
    if (type === 'planning_started') return 'Planning started';
    if (type === 'planning_complete') return 'Planning complete';
    if (type === 'task_started') return `Task started`;
    if (type === 'task_completed') return `Task completed`;
    if (type === 'task_failed') return `Task failed`;
    if (type === 'tests_passed') return 'Tests passed';
    if (type === 'tests_failed') return 'Tests failed';
    if (type === 'pr_created') return 'PR created';
    if (type === 'ticket_completed') return 'Ticket completed';
    if (type === 'ticket_failed') return 'Ticket failed';
    return type?.replace(/_/g, ' ') || 'event';
  }

  function taskTitle(taskId: string): string {
    const task = tasks.find(t => t.ID === taskId);
    return task ? `${task.Sequence}. ${task.Title}` : '';
  }

  function expandDetails(details: string): Record<string, string> | null {
    if (!details) return null;
    try { return JSON.parse(details); } catch { return null; }
  }
</script>

<div
  bind:this={container}
  onscroll={handleScroll}
  class="flex-1 overflow-y-auto space-y-0"
  role="log"
  aria-label="Activity stream"
>
  {#each events as evt (evt.ID)}
    <div
      class="px-3 py-2 border-b border-border hover:bg-surface-hover
        {evt.Severity === 'error' ? 'bg-danger/5 border-l-2 border-l-danger' : ''}
        {evt.isNew ? 'animate-fade-in' : ''}"
    >
      <div class="flex items-start gap-2">
        <span class="shrink-0 {
          evt.Severity === 'success' ? 'text-success' :
          evt.Severity === 'error' ? 'text-danger' :
          evt.Severity === 'warning' ? 'text-warning' :
          'text-muted'
        }">{severityIcon(evt.Severity)}</span>

        <div class="flex-1 min-w-0">
          <div class="flex items-center gap-2">
            <span class="text-sm font-medium">{eventLabel(evt)}</span>
            <span class="text-xs text-muted ml-auto shrink-0">{formatRelative(evt.CreatedAt)}</span>
          </div>

          {#if evt.TaskID && taskTitle(evt.TaskID)}
            <div class="text-xs text-muted mt-0.5">{taskTitle(evt.TaskID)}</div>
          {/if}

          {#if evt.Message}
            <div class="text-xs text-text/80 mt-0.5">{evt.Message}</div>
          {/if}
        </div>
      </div>

      <!-- Expandable raw details -->
      {#if evt.Details}
        {@const details = expandDetails(evt.Details)}
        {#if details}
          <details class="mt-1 ml-5">
            <summary class="text-xs text-muted cursor-pointer hover:text-accent">raw detail</summary>
            <pre class="text-xs text-muted mt-1 overflow-x-auto">{JSON.stringify(details, null, 2)}</pre>
          </details>
        {/if}
      {/if}
    </div>
  {/each}

  {#if events.length === 0}
    <div class="px-3 py-8 text-center text-muted text-sm">No activity yet.</div>
  {/if}
</div>
