<script lang="ts">
  import type { EventRecord, Task } from '../types';
  import { severityIcon, formatRelative, taskIcon, linkifyParts } from '../format';

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
      container.scrollTop = 0;
    }
  });

  function eventLabel(evt: EventRecord): string {
    const type = evt.EventType;
    if (type === 'planning_started') return 'Planning started';
    if (type === 'planning_complete') return 'Planning complete';
    if (type === 'task_started') return 'Task started';
    if (type === 'task_completed') return 'Task completed';
    if (type === 'task_failed') return 'Task failed';
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

  function severityLeft(severity: string): string {
    if (severity === 'error') return 'border-l-4 border-l-danger bg-danger-bg';
    if (severity === 'success') return 'border-l-4 border-l-success';
    if (severity === 'warning') return 'border-l-4 border-l-warning bg-warning-bg';
    return 'border-l-4 border-l-border';
  }

  function severityTextCls(severity: string): string {
    if (severity === 'error') return 'text-danger';
    if (severity === 'success') return 'text-success';
    if (severity === 'warning') return 'text-warning';
    return 'text-muted-bright';
  }
</script>

<div
  bind:this={container}
  onscroll={handleScroll}
  class="flex-1 overflow-y-auto"
  role="log"
  aria-label="Activity stream"
>
  {#each events as evt (evt.ID)}
    <div class="px-3 py-2.5 border-b border-border {severityLeft(evt.Severity)} {evt.isNew ? 'animate-fade-in' : ''}">
      <div class="flex items-start gap-2">
        <!-- Icon -->
        <span class="text-xs shrink-0 mt-0.5 w-3 text-center {severityTextCls(evt.Severity)}">
          {severityIcon(evt.Severity)}
        </span>

        <div class="flex-1 min-w-0">
          <!-- Header row -->
          <div class="flex items-center gap-2">
            <span class="text-xs font-bold text-text tracking-wide">{eventLabel(evt)}</span>
            <span class="text-[10px] text-muted ml-auto shrink-0">{formatRelative(evt.CreatedAt)}</span>
          </div>

          <!-- Task reference -->
          {#if evt.TaskID && taskTitle(evt.TaskID)}
            <div class="text-[10px] text-muted-bright mt-0.5">{taskTitle(evt.TaskID)}</div>
          {/if}

          <!-- Message -->
          {#if evt.Message}
            <div class="text-[10px] text-text/70 mt-0.5 break-words">
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

          <!-- Raw detail expand -->
          {#if evt.Details}
            {@const details = expandDetails(evt.Details)}
            {#if details}
              <details class="mt-1">
                <summary class="text-[10px] text-muted cursor-pointer hover:text-accent transition-colors select-none">
                  raw detail ▸
                </summary>
                <pre class="text-[10px] text-muted mt-1 overflow-x-auto bg-bg p-2 border border-border">{JSON.stringify(details, null, 2)}</pre>
              </details>
            {/if}
          {/if}
        </div>
      </div>
    </div>
  {/each}

  {#if events.length === 0}
    <div class="px-3 py-8 text-center">
      <div class="text-muted text-xs tracking-wider">NO ACTIVITY YET</div>
    </div>
  {/if}
</div>
