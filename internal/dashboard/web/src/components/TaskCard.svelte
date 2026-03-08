<script lang="ts">
  import type { Task, EventRecord } from '../types';
  import { retryTask, expandedTasks } from '../state.svelte';
  import { taskIcon, formatCost, formatRelative } from '../format';

  let { task, events = [] }: { task: Task; events?: EventRecord[] } = $props();

  let expanded = $derived(expandedTasks[task.ID] ?? false);

  let taskEvents = $derived(
    events.filter(e => e.TaskID === task.ID).slice(0, 10)
  );

  let isActive = $derived(
    ['implementing', 'tdd_verifying', 'testing', 'spec_review', 'quality_review'].includes(task.Status)
  );

  function toggle() {
    expandedTasks[task.ID] = !expanded;
  }

  function statusColor(): string {
    if (task.Status === 'done') return 'text-success';
    if (task.Status === 'failed') return 'text-danger';
    if (isActive) return 'text-accent';
    return 'text-muted';
  }

  function handleRetry(e: MouseEvent) {
    e.stopPropagation();
    if (confirm('Retry this task?')) retryTask(task.ID);
  }
</script>

<div class="border border-border {isActive ? 'border-l-2 border-l-accent' : ''} {task.Status === 'failed' ? 'border-l-2 border-l-danger' : ''}">
  <div
    class="w-full text-left px-3 py-2 hover:bg-surface-hover flex items-center gap-2 cursor-pointer"
    onclick={toggle}
    onkeydown={(e) => e.key === 'Enter' && toggle()}
    role="button"
    tabindex="0"
    aria-expanded={expanded}
  >
    <span class="{statusColor()} {isActive ? 'animate-pulse' : ''}">{taskIcon(task.Status)}</span>
    <span class="text-sm flex-1 truncate">{task.Sequence}. {task.Title}</span>
    <span class="text-xs text-muted">{task.EstimatedComplexity}</span>
    {#if task.Status === 'failed'}
      <button class="text-xs text-danger hover:text-text" onclick={handleRetry}>[retry]</button>
    {/if}
  </div>

  {#if expanded}
    <div class="px-3 py-2 border-t border-border text-xs space-y-1 bg-bg">
      <div class="flex gap-4 text-muted">
        <span>Status: <span class={statusColor()}>{task.Status}</span></span>
        {#if task.ImplementationAttempts > 0}
          <span>Attempt {task.ImplementationAttempts}</span>
        {/if}
        <span>Cost: {formatCost(task.CostUSD)}</span>
      </div>

      {#if task.FilesToModify?.length}
        <div class="text-muted">
          Files: <span class="text-text">{task.FilesToModify.join(', ')}</span>
        </div>
      {/if}

      {#if task.ErrorMessage}
        <div class="text-danger mt-1 p-2 bg-danger/10 border border-danger/20">
          {task.ErrorMessage}
        </div>
      {/if}

      {#if task.AcceptanceCriteria?.length}
        <div class="mt-2">
          <div class="text-muted mb-1">Acceptance Criteria:</div>
          {#each task.AcceptanceCriteria as criterion}
            <div class="flex items-center gap-1">
              <span class="text-muted">{task.Status === 'done' ? '\u2713' : '\u25CB'}</span>
              <span>{criterion}</span>
            </div>
          {/each}
        </div>
      {/if}

      <!-- Activity stream for this task -->
      {#if taskEvents.length > 0}
        <div class="mt-2 border-t border-border pt-2">
          <div class="text-muted mb-1">Activity:</div>
          {#each taskEvents as evt}
            <div class="flex gap-2 py-0.5">
              <span class="text-muted shrink-0">{formatRelative(evt.CreatedAt)}</span>
              <span class="truncate">{evt.Message || evt.EventType}</span>
            </div>
          {/each}
        </div>
      {/if}
    </div>
  {/if}
</div>
