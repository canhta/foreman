<script lang="ts">
  import type { Task, EventRecord, LlmCallRecord } from '../types';
  import { appState, retryTask } from '../state.svelte';
  import { taskIcon, formatCost, formatRelative } from '../format';
  import ConfirmDialog from './ConfirmDialog.svelte';

  let { task, events = [], llmCalls = [] }: { task: Task; events?: EventRecord[]; llmCalls?: LlmCallRecord[] } = $props();

  let confirmOpen = $state(false);

  let expanded = $derived(appState.expandedTasks[task.ID] ?? false);

  let taskEvents = $derived(
    events.filter(e => e.TaskID === task.ID).slice(0, 10)
  );

  let taskCalls = $derived(llmCalls.filter(c => c.TaskID === task.ID));

  // Derive unique models from llm calls for this task.
  let taskModels = $derived(
    [...new Set(taskCalls.map(c => c.Model).filter(Boolean))].join(', ')
  );

  // Runner label: prefer the task-level field, fall back to deriving from llm calls.
  let runnerLabel = $derived(
    task.AgentRunner || (taskCalls.length > 0 ? taskCalls[0].AgentRunner : '') || ''
  );

  function runnerBadgeCls(runner: string): string {
    if (runner === 'claudecode') return 'text-accent border-accent/40';
    if (runner === 'copilot')    return 'text-purple-400 border-purple-400/40';
    if (runner === 'builtin')    return 'text-muted border-border-strong';
    return 'text-muted border-border-strong';
  }

  let isActive = $derived(
    ['implementing', 'tdd_verifying', 'testing', 'spec_review', 'quality_review'].includes(task.Status)
  );

  function toggle() {
    appState.expandedTasks[task.ID] = !expanded;
  }

  function leftBorderCls(): string {
    if (task.Status === 'done') return 'border-l-4 border-l-success';
    if (task.Status === 'failed') return 'border-l-4 border-l-danger';
    if (isActive) return 'border-l-4 border-l-accent';
    return 'border-l-4 border-l-border-strong';
  }

  function statusBadge(): { text: string; cls: string } {
    if (task.Status === 'done') return { text: 'DONE', cls: 'text-success border-success/40' };
    if (task.Status === 'failed') return { text: 'FAILED', cls: 'text-danger border-danger/40' };
    if (isActive) return { text: task.Status.toUpperCase().replace('_', ' '), cls: 'text-accent border-accent/40' };
    if (task.Status === 'skipped') return { text: 'SKIP', cls: 'text-muted border-border-strong' };
    return { text: task.Status.toUpperCase(), cls: 'text-muted border-border-strong' };
  }

  let badge = $derived(statusBadge());

  let liveProgress = $derived(appState.activeTaskProgress[task.ID]);

  function handleRetry(e: MouseEvent) {
    e.stopPropagation();
    confirmOpen = true;
  }
</script>

<div class="border border-border {leftBorderCls()} bg-surface">
  <!-- Summary row -->
  <div
    class="w-full text-left px-3 py-2 hover:bg-surface-hover flex items-center gap-2 cursor-pointer"
    onclick={toggle}
    onkeydown={(e) => e.key === 'Enter' && toggle()}
    role="button"
    tabindex="0"
    aria-expanded={expanded}
  >
    <!-- Sequence + icon -->
    <span class="text-muted-bright text-xs shrink-0 w-5 text-center">
      {#if isActive}
        <span class="animate-pulse text-accent">{taskIcon(task.Status)}</span>
      {:else}
        <span>{taskIcon(task.Status)}</span>
      {/if}
    </span>

    <!-- Title -->
    <span class="text-xs flex-1 truncate text-text">{task.Sequence}. {task.Title}</span>

    <!-- Complexity -->
    {#if task.EstimatedComplexity}
      <span class="text-[10px] text-muted border border-border px-1 shrink-0">{task.EstimatedComplexity}</span>
    {/if}

    <!-- Status badge -->
    <span class="text-[10px] border px-1 py-0.5 leading-none shrink-0 {badge.cls}">{badge.text}</span>

    <!-- Retry -->
    {#if task.Status === 'failed'}
      <button
        class="text-[10px] text-danger hover:text-text border border-danger/40 px-1.5 py-0.5 hover:bg-danger/10 transition-colors shrink-0"
        onclick={handleRetry}
      >↺</button>
    {/if}

    <!-- Expand indicator -->
    <span class="text-muted text-[10px] shrink-0">{expanded ? '▲' : '▼'}</span>
  </div>

  {#if expanded}
    <div class="border-t border-border bg-bg text-xs">
      <!-- Stats row -->
      <div class="flex flex-wrap items-center gap-3 px-3 py-2 text-muted border-b border-border">
        {#if task.ImplementationAttempts > 0}
          <span>Attempt <span class="text-text">{task.ImplementationAttempts}</span></span>
        {/if}
        <span>Cost <span class="text-text">{formatCost(task.CostUSD)}</span></span>
        {#if task.TotalLlmCalls > 0}
          <span><span class="text-text">{task.TotalLlmCalls}</span> LLM calls</span>
        {/if}
        {#if runnerLabel}
          <span class="text-[10px] border px-1 py-0.5 leading-none shrink-0 {runnerBadgeCls(runnerLabel)}">{runnerLabel}</span>
        {/if}
        {#if taskModels}
          <span class="text-[10px] text-muted-bright truncate max-w-[140px]" title={taskModels}>{taskModels}</span>
        {/if}
        {#if task.StartedAt}
          <span>{formatRelative(task.StartedAt)}</span>
        {/if}
      </div>

      <!-- Live execution progress -->
      {#if isActive && liveProgress}
        <div class="px-3 py-2 border-b border-border bg-accent-bg">
          <div class="flex items-center gap-2 text-xs">
            <span class="text-accent animate-pulse">►</span>
            <span class="text-text">TURN {liveProgress.turn}/{liveProgress.maxTurns}</span>
            {#if liveProgress.runner}
              <span class="text-[10px] border px-1 py-0.5 leading-none {runnerBadgeCls(liveProgress.runner)}">{liveProgress.runner}</span>
            {/if}
            {#if liveProgress.model}
              <span class="text-[10px] text-muted-bright">{liveProgress.model.replace('claude-', '')}</span>
            {/if}
          </div>
          {#if liveProgress.lastTool}
            <div class="text-[10px] text-muted mt-1 pl-4">
              Last tool: <span class="text-text">{liveProgress.lastTool}</span>
              {#if liveProgress.lastToolTime}
                <span class="text-muted"> {formatRelative(liveProgress.lastToolTime)}</span>
              {/if}
            </div>
          {/if}
        </div>
      {/if}

      <!-- Error -->
      {#if task.ErrorMessage}
        <div class="mx-3 my-2 border-l-4 border-l-danger bg-danger-bg p-2">
          <div class="text-danger font-bold text-[10px] tracking-wider mb-1">ERROR</div>
          <div class="text-text/80">{task.ErrorMessage}</div>
        </div>
      {/if}

      <!-- Files -->
      {#if task.FilesToModify?.length}
        <div class="px-3 py-1.5 border-b border-border">
          <div class="text-muted-bright text-[10px] tracking-wider mb-1">FILES</div>
          <div class="space-y-0.5">
            {#each task.FilesToModify as f}
              <div class="text-text/70 text-[10px] truncate">· {f}</div>
            {/each}
          </div>
        </div>
      {/if}

      <!-- Acceptance criteria -->
      {#if task.AcceptanceCriteria?.length}
        <div class="px-3 py-1.5 border-b border-border">
          <div class="text-muted-bright text-[10px] tracking-wider mb-1">ACCEPTANCE CRITERIA</div>
          <div class="space-y-1">
            {#each task.AcceptanceCriteria as criterion}
              <div class="flex items-start gap-1.5">
                <span class="{task.Status === 'done' ? 'text-success' : 'text-muted'}">
                  {task.Status === 'done' ? '✓' : '○'}
                </span>
                <span class="text-text/70">{criterion}</span>
              </div>
            {/each}
          </div>
        </div>
      {/if}

      <!-- Recent events -->
      {#if taskEvents.length > 0}
        <div class="px-3 py-1.5">
          <div class="text-muted-bright text-[10px] tracking-wider mb-1">ACTIVITY</div>
          <div class="space-y-0.5">
            {#each taskEvents as evt}
              <div class="flex gap-2 py-0.5 text-[10px]">
                <span class="text-muted shrink-0">{formatRelative(evt.CreatedAt)}</span>
                <span class="text-text/60 truncate">{evt.Message || evt.EventType}</span>
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
  title="RETRY TASK"
  message="Re-run this task through the pipeline again?"
  confirmLabel="↺ RETRY"
  confirmClass="bg-warning text-bg hover:bg-text"
  onconfirm={() => { retryTask(task.ID); confirmOpen = false; }}
  oncancel={() => { confirmOpen = false; }}
/>
