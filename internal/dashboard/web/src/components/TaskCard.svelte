<script lang="ts">
  import type { Task, EventRecord, LlmCallRecord } from '../types';
  import { projectState } from '../state/project.svelte';
  import { formatCost, formatRelative, runnerBadgeCls, shortModel } from '../format';
  import {
    IconCheck, IconX, IconSettings2, IconCircleOff, IconCircle,
    IconRefresh, IconPlayerPlay, IconChevronUp, IconChevronDown
  } from '@tabler/icons-svelte';
  import ConfirmDialog from './ConfirmDialog.svelte';

  let { task, events = [], llmCalls = [] }: { task: Task; events?: EventRecord[]; llmCalls?: LlmCallRecord[] } = $props();

  let confirmOpen = $state(false);

  let expanded = $derived(projectState.expandedTasks[task.ID] ?? false);

  let taskEvents = $derived(events.filter(e => e.TaskID === task.ID).slice(0, 10));
  let taskCalls  = $derived(llmCalls.filter(c => c.TaskID === task.ID));

  let taskModels = $derived(
    [...new Set(taskCalls.map(c => c.Model).filter(Boolean))].join(', ')
  );

  let runnerLabel = $derived(
    task.AgentRunner || (taskCalls.length > 0 ? taskCalls[0].AgentRunner : '') || ''
  );

  let isActive = $derived(
    ['implementing', 'tdd_verifying', 'testing', 'spec_review', 'quality_review'].includes(task.Status)
  );

  let isDone   = $derived(task.Status === 'done');
  let isFailed = $derived(task.Status === 'failed');

  function toggle() {
    projectState.expandedTasks[task.ID] = !expanded;
  }

  function leftBorderCls(): string {
    if (isDone)     return 'border-l-[var(--color-success)] border-l-2';
    if (isFailed)   return 'border-l-[var(--color-danger)] border-l-2';
    if (isActive)   return 'border-l-[var(--color-accent)] border-l-2';
    return 'border-l-[var(--color-border-strong)] border-l-2';
  }

  function statusBadgeCls(): string {
    if (isDone)               return 'status-chip status-chip-done';
    if (isFailed)             return 'status-chip status-chip-failed';
    if (task.Status === 'skipped') return 'status-chip status-chip-neutral';
    if (isActive)             return 'status-chip status-chip-active';
    return 'status-chip status-chip-neutral';
  }

  function statusText(): string {
    if (isDone)             return 'Done';
    if (isFailed)           return 'Failed';
    if (task.Status === 'skipped') return 'Skip';
    if (isActive) return task.Status.replace(/_/g, ' ');
    return task.Status.replace(/_/g, ' ');
  }

  let liveProgress = $derived(projectState.activeTaskProgress?.[task.ID]);

  function handleRetry(e: MouseEvent) {
    e.stopPropagation();
    confirmOpen = true;
  }
</script>

<div class="border border-[var(--color-border)] {leftBorderCls()} bg-[var(--color-surface)]">
  <!-- Summary row -->
  <div
    class="w-full text-left px-3 py-2.5 hover:bg-[var(--color-surface-hover)] flex items-center gap-2.5 cursor-pointer transition-colors"
    onclick={toggle}
    onkeydown={(e) => e.key === 'Enter' && toggle()}
    role="button"
    tabindex="0"
    aria-expanded={expanded}
  >
    <!-- Icon -->
    <span class="shrink-0 w-4 flex items-center justify-center"
          class:text-[var(--color-accent)]={isActive}
          class:animate-pulse={isActive}
          class:text-[var(--color-success)]={isDone}
          class:text-[var(--color-danger)]={isFailed}
          class:text-[var(--color-muted-bright)]={!isActive && !isDone && !isFailed}>
      {#if isDone}<IconCheck size={14} stroke={1.5} />
      {:else if isFailed}<IconX size={14} stroke={1.5} />
      {:else if isActive}<IconSettings2 size={14} stroke={1.5} />
      {:else if task.Status === 'skipped'}<IconCircleOff size={14} stroke={1.5} />
      {:else}<IconCircle size={14} stroke={1.5} />{/if}
    </span>

    <!-- Title -->
    <span class="text-xs flex-1 truncate leading-none">{task.Sequence}. {task.Title}</span>

    <!-- Complexity -->
    {#if task.EstimatedComplexity}
      <span class="text-[10px] text-[var(--color-muted)] border border-[var(--color-border-strong)] px-1.5 py-0.5 leading-none shrink-0">
        {task.EstimatedComplexity}
      </span>
    {/if}

    <!-- Status badge -->
    <span class="{statusBadgeCls()} shrink-0 capitalize">{statusText()}</span>

    <!-- Retry button for failed tasks -->
    {#if isFailed}
      <button
        class="text-[10px] text-[var(--color-danger)] hover:text-[var(--color-text)]
               border border-[var(--color-danger)]/40 px-1.5 py-0.5
               hover:bg-[var(--color-danger-bg)] transition-colors shrink-0 leading-none"
        onclick={handleRetry}
      ><IconRefresh size={14} stroke={1.5} /></button>
    {/if}

    <!-- Expand chevron -->
    <span class="text-[var(--color-muted)] shrink-0 flex items-center">
      {#if expanded}<IconChevronUp size={14} stroke={1.5} />{:else}<IconChevronDown size={14} stroke={1.5} />{/if}
    </span>
  </div>

  <!-- Live activity banner (shown even when collapsed if active) -->
  {#if isActive && liveProgress}
    <div class="px-3 py-2 border-t border-[var(--color-border)] bg-[var(--color-accent-bg)] flex items-center gap-3 flex-wrap">
      <span class="text-[var(--color-accent)] animate-pulse flex items-center"><IconPlayerPlay size={14} stroke={1.5} /></span>
      <span class="text-xs font-bold text-[var(--color-accent)]">
        Turn {liveProgress.turn}/{liveProgress.maxTurns}
      </span>
      {#if liveProgress.runner}
        <span class="text-[10px] border px-1.5 py-0.5 leading-none {runnerBadgeCls(liveProgress.runner)}">{liveProgress.runner}</span>
      {/if}
      {#if liveProgress.model}
        <span class="text-[10px] text-[var(--color-muted-bright)]">{shortModel(liveProgress.model)}</span>
      {/if}
      {#if liveProgress.lastTool}
        <span class="text-[10px] text-[var(--color-muted)] ml-auto">
          {liveProgress.lastTool}
          {#if liveProgress.lastToolTime}
            <span class="opacity-60"> · {formatRelative(liveProgress.lastToolTime)}</span>
          {/if}
        </span>
      {/if}
    </div>
  {/if}

  <!-- Expanded detail -->
  {#if expanded}
    <div class="border-t border-[var(--color-border)] bg-[var(--color-bg)] text-xs divide-y divide-[var(--color-border)]">

      <!-- Stats row -->
      <div class="flex flex-wrap items-center gap-x-4 gap-y-1 px-3 py-2.5 text-[10px] text-[var(--color-muted)]">
        {#if task.ImplementationAttempts > 0}
          <span>Attempt <span class="text-[var(--color-text)]">{task.ImplementationAttempts}</span></span>
        {/if}
        <span>Cost <span class="text-[var(--color-text)] tabular-nums">{formatCost(task.CostUSD)}</span></span>
        {#if task.TotalLlmCalls > 0}
          <span><span class="text-[var(--color-text)]">{task.TotalLlmCalls}</span> LLM calls</span>
        {/if}
        {#if runnerLabel}
          <span class="text-[10px] border px-1.5 py-0.5 leading-none {runnerBadgeCls(runnerLabel)}">{runnerLabel}</span>
        {/if}
        {#if taskModels}
          <span class="text-[10px] text-[var(--color-muted-bright)] truncate max-w-[140px]" title={taskModels}>{taskModels}</span>
        {/if}
        {#if task.StartedAt}
          <span class="ml-auto">{formatRelative(task.StartedAt)}</span>
        {/if}
      </div>

      <!-- Error -->
      {#if task.ErrorMessage}
        <div class="mx-3 my-2 border-l-2 border-l-[var(--color-danger)] bg-[var(--color-danger-bg)] p-2.5">
          <div class="text-[10px] font-bold tracking-[0.15em] text-[var(--color-danger)] uppercase mb-1">Error</div>
          <div class="text-xs text-[var(--color-text)]/80 leading-relaxed">{task.ErrorMessage}</div>
        </div>
      {/if}

      <!-- Files -->
      {#if task.FilesToModify?.length}
        <div class="px-3 py-2">
          <div class="text-[10px] tracking-[0.15em] text-[var(--color-muted)] uppercase mb-1.5">Files</div>
          <div class="space-y-0.5">
            {#each task.FilesToModify as f}
              <div class="text-[10px] text-[var(--color-muted-bright)] truncate">· {f}</div>
            {/each}
          </div>
        </div>
      {/if}

      <!-- Acceptance criteria -->
      {#if task.AcceptanceCriteria?.length}
        <div class="px-3 py-2">
          <div class="text-[10px] tracking-[0.15em] text-[var(--color-muted)] uppercase mb-1.5">Acceptance Criteria</div>
          <div class="space-y-1">
            {#each task.AcceptanceCriteria as criterion}
              <div class="flex items-start gap-2">
                <span class="shrink-0 flex items-center mt-0.5"
                      class:text-[var(--color-success)]={isDone}
                      class:text-[var(--color-muted)]={!isDone}>
                  {#if isDone}<IconCheck size={12} stroke={1.5} />{:else}<IconCircle size={12} stroke={1.5} />{/if}
                </span>
                <span class="text-[10px] text-[var(--color-muted-bright)] leading-snug">{criterion}</span>
              </div>
            {/each}
          </div>
        </div>
      {/if}

      <!-- Recent events -->
      {#if taskEvents.length > 0}
        <div class="px-3 py-2">
          <div class="text-[10px] tracking-[0.15em] text-[var(--color-muted)] uppercase mb-1.5">Activity</div>
          <div class="space-y-1">
            {#each taskEvents as evt}
              <div class="flex gap-3 text-[10px]">
                <span class="text-[var(--color-muted)] shrink-0 tabular-nums">{formatRelative(evt.CreatedAt)}</span>
                <span class="text-[var(--color-muted-bright)] truncate">{evt.Message || evt.EventType}</span>
              </div>
            {/each}
          </div>
        </div>
      {/if}
    </div>
  {/if}
</div>

<ConfirmDialog
  open={confirmOpen}
  title="Retry Ticket"
  message="Re-run the entire ticket through the pipeline again? (All tasks will be retried.)"
  confirmLabel="Retry"
  confirmClass="bg-warning text-bg hover:opacity-90"
  onconfirm={() => { projectState.retryTicket(task.TicketID); confirmOpen = false; }}
  oncancel={() => { confirmOpen = false; }}
/>
