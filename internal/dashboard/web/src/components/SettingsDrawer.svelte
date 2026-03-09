<script lang="ts">
  import { appState, closeSettings, selectTicket, setSettingsTab } from '../state.svelte';
  import { formatCost, formatTokens, formatRelative, runnerBadgeCls, shortModel } from '../format';

  function budgetPct(used: number, budget: number): number {
    if (!budget) return 0;
    return Math.min(100, (used / budget) * 100);
  }

  function budgetBarCls(pct: number): string {
    if (pct >= 90) return 'bg-danger';
    if (pct >= 75) return 'bg-warning';
    return 'bg-accent';
  }

  $effect(() => {
    const handleKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape' && appState.settingsOpen) closeSettings();
    };
    document.addEventListener('keydown', handleKey);
    return () => document.removeEventListener('keydown', handleKey);
  });

  let maxDayCost = $derived(
    appState.weekDays.length > 0
      ? Math.max(...appState.weekDays.map(d => d.cost_usd || 0), 0.0001)
      : 0.0001
  );

  function formatDayLabel(date: string): string {
    if (!date) return '';
    const d = new Date(date);
    return `${d.getMonth() + 1}/${d.getDate()}`;
  }

  let drawerPanel = $state<HTMLDivElement | null>(null);

  $effect(() => {
    if (appState.settingsOpen && drawerPanel) {
      drawerPanel.focus();
    }
  });
</script>

<!-- Backdrop -->
{#if appState.settingsOpen}
  <div
    class="fixed inset-0 z-[59] bg-bg/60"
    onclick={closeSettings}
    role="presentation"
  ></div>
{/if}

<!-- Drawer -->
<div
  bind:this={drawerPanel}
  class="fixed right-0 top-0 h-full w-[420px] z-[60] bg-surface border-l-2 border-border flex flex-col"
  style="transform: {appState.settingsOpen ? 'translateX(0)' : 'translateX(100%)'}; transition: transform 150ms ease-out;"
  role="dialog"
  aria-modal="true"
  aria-label="Settings & Usage"
  tabindex="-1"
>
  <!-- Drawer header -->
  <div class="flex items-center justify-between px-4 py-2.5 border-b-2 border-border bg-surface-active">
    <span class="text-xs font-bold tracking-[0.2em] text-muted-bright">SETTINGS & USAGE</span>
    <button
      class="text-muted hover:text-text transition-colors text-xs tracking-wider"
      onclick={closeSettings}
    >✕</button>
  </div>

  <!-- Tab bar -->
  <div class="flex border-b-2 border-border">
    {#each ['config', 'usage'] as tab}
      <button
        class="flex-1 px-4 py-2 text-xs tracking-[0.15em] transition-colors
          {appState.settingsTab === tab ? 'bg-accent text-bg font-bold' : 'text-muted hover:text-text'}"
        onclick={() => setSettingsTab(tab as 'config' | 'usage')}
      >{tab.toUpperCase()}</button>
    {/each}
  </div>

  <!-- Content -->
  <div class="flex-1 overflow-y-auto">

    <!-- ══════════════ CONFIG TAB ══════════════ -->
    {#if appState.settingsTab === 'config'}
      {#if appState.configSummary === null}
        <div class="text-muted text-[10px] tracking-wider px-3 py-4">LOADING...</div>
      {:else}
        {@const summary = appState.configSummary}
        <div class="p-4 space-y-4">

          <!-- LLM -->
          <section class="border-2 border-border">
            <div class="px-3 py-1.5 border-b border-border bg-surface-active border-l-4 border-l-accent">
              <span class="text-[10px] font-bold tracking-[0.2em] text-text">LLM</span>
            </div>
            <div class="p-3 space-y-1.5">
              <div class="flex justify-between">
                <span class="text-[10px] text-muted tracking-wider">provider</span>
                <span class="text-xs text-text">{summary.llm.provider}</span>
              </div>
              {#each Object.entries(summary.llm.models) as [role, model]}
                <div class="flex justify-between">
                  <span class="text-[10px] text-muted tracking-wider">{role}</span>
                  <span class="text-xs text-text">{model}</span>
                </div>
              {/each}
              <div class="flex justify-between">
                <span class="text-[10px] text-muted tracking-wider">api_key</span>
                <span class="text-xs text-text">{summary.llm.api_key}</span>
              </div>
            </div>
          </section>

          <!-- TRACKER -->
          <section class="border-2 border-border">
            <div class="px-3 py-1.5 border-b border-border bg-surface-active border-l-4 border-l-accent">
              <span class="text-[10px] font-bold tracking-[0.2em] text-text">TRACKER</span>
            </div>
            <div class="p-3 space-y-1.5">
              <div class="flex justify-between">
                <span class="text-[10px] text-muted tracking-wider">provider</span>
                <span class="text-xs text-text">{summary.tracker.provider}</span>
              </div>
              <div class="flex justify-between">
                <span class="text-[10px] text-muted tracking-wider">poll_interval</span>
                <span class="text-xs text-text">{summary.tracker.poll_interval}</span>
              </div>
            </div>
          </section>

          <!-- GIT -->
          <section class="border-2 border-border">
            <div class="px-3 py-1.5 border-b border-border bg-surface-active border-l-4 border-l-accent">
              <span class="text-[10px] font-bold tracking-[0.2em] text-text">GIT</span>
            </div>
            <div class="p-3 space-y-1.5">
              <div class="flex justify-between">
                <span class="text-[10px] text-muted tracking-wider">provider</span>
                <span class="text-xs text-text">{summary.git.provider}</span>
              </div>
              <div class="flex justify-between">
                <span class="text-[10px] text-muted tracking-wider">clone_url</span>
                <span class="text-xs text-text truncate max-w-[260px]">{summary.git.clone_url}</span>
              </div>
              <div class="flex justify-between">
                <span class="text-[10px] text-muted tracking-wider">branch_prefix</span>
                <span class="text-xs text-text">{summary.git.branch_prefix}</span>
              </div>
              <div class="flex justify-between">
                <span class="text-[10px] text-muted tracking-wider">auto_merge</span>
                <span class="text-xs text-text">{summary.git.auto_merge ? 'ON' : 'OFF'}</span>
              </div>
            </div>
          </section>

          <!-- AGENT RUNNER -->
          <section class="border-2 border-border">
            <div class="px-3 py-1.5 border-b border-border bg-surface-active border-l-4 border-l-accent">
              <span class="text-[10px] font-bold tracking-[0.2em] text-text">AGENT RUNNER</span>
            </div>
            <div class="p-3 space-y-1.5">
              <div class="flex justify-between">
                <span class="text-[10px] text-muted tracking-wider">provider</span>
                <span class="text-xs text-text">{summary.agent_runner.provider}</span>
              </div>
              <div class="flex justify-between">
                <span class="text-[10px] text-muted tracking-wider">max_turns</span>
                <span class="text-xs text-text">{summary.agent_runner.max_turns}</span>
              </div>
              <div class="flex justify-between">
                <span class="text-[10px] text-muted tracking-wider">token_budget</span>
                <span class="text-xs text-text">{summary.agent_runner.token_budget.toLocaleString()}</span>
              </div>
            </div>
          </section>

          <!-- COST BUDGETS -->
          <section class="border-2 border-border">
            <div class="px-3 py-1.5 border-b border-border bg-surface-active border-l-4 border-l-accent">
              <span class="text-[10px] font-bold tracking-[0.2em] text-text">COST BUDGETS</span>
            </div>
            <div class="p-3 space-y-1.5">
              <div class="flex justify-between">
                <span class="text-[10px] text-muted tracking-wider">daily_budget</span>
                <span class="text-xs text-text">${summary.cost.daily_budget.toFixed(2)}</span>
              </div>
              <div class="flex justify-between">
                <span class="text-[10px] text-muted tracking-wider">monthly_budget</span>
                <span class="text-xs text-text">${summary.cost.monthly_budget.toFixed(2)}</span>
              </div>
              <div class="flex justify-between">
                <span class="text-[10px] text-muted tracking-wider">per_ticket_budget</span>
                <span class="text-xs text-text">${summary.cost.per_ticket_budget.toFixed(2)}</span>
              </div>
              <div class="flex justify-between">
                <span class="text-[10px] text-muted tracking-wider">alert_threshold</span>
                <span class="text-xs text-text">{summary.cost.alert_threshold}%</span>
              </div>
            </div>
          </section>

          <!-- DAEMON -->
          <section class="border-2 border-border">
            <div class="px-3 py-1.5 border-b border-border bg-surface-active border-l-4 border-l-accent">
              <span class="text-[10px] font-bold tracking-[0.2em] text-text">DAEMON</span>
            </div>
            <div class="p-3 space-y-1.5">
              <div class="flex justify-between">
                <span class="text-[10px] text-muted tracking-wider">max_parallel_tickets</span>
                <span class="text-xs text-text">{summary.daemon.max_parallel_tickets}</span>
              </div>
              <div class="flex justify-between">
                <span class="text-[10px] text-muted tracking-wider">max_parallel_tasks</span>
                <span class="text-xs text-text">{summary.daemon.max_parallel_tasks}</span>
              </div>
              <div class="flex justify-between">
                <span class="text-[10px] text-muted tracking-wider">work_dir</span>
                <span class="text-xs text-text truncate max-w-[260px]">{summary.daemon.work_dir}</span>
              </div>
              <div class="flex justify-between">
                <span class="text-[10px] text-muted tracking-wider">log_level</span>
                <span class="text-xs text-text">{summary.daemon.log_level}</span>
              </div>
            </div>
          </section>

          <!-- DATABASE -->
          <section class="border-2 border-border">
            <div class="px-3 py-1.5 border-b border-border bg-surface-active border-l-4 border-l-accent">
              <span class="text-[10px] font-bold tracking-[0.2em] text-text">DATABASE</span>
            </div>
            <div class="p-3 space-y-1.5">
              <div class="flex justify-between">
                <span class="text-[10px] text-muted tracking-wider">driver</span>
                <span class="text-xs text-text">{summary.database.driver}</span>
              </div>
              <div class="flex justify-between">
                <span class="text-[10px] text-muted tracking-wider">path</span>
                <span class="text-xs text-text truncate max-w-[260px]">{summary.database.path}</span>
              </div>
            </div>
          </section>

          <!-- MCP SERVERS -->
          <section class="border-2 border-border">
            <div class="px-3 py-1.5 border-b border-border bg-surface-active border-l-4 border-l-accent">
              <span class="text-[10px] font-bold tracking-[0.2em] text-text">MCP SERVERS</span>
            </div>
            <div class="p-3 space-y-1.5">
              {#if summary.mcp.servers.length > 0}
                {#each summary.mcp.servers as server}
                  <div class="text-xs text-text">{server}</div>
                {/each}
              {:else}
                <span class="text-xs text-muted">None configured</span>
              {/if}
            </div>
          </section>

          <!-- RATE LIMIT -->
          <section class="border-2 border-border">
            <div class="px-3 py-1.5 border-b border-border bg-surface-active border-l-4 border-l-accent">
              <span class="text-[10px] font-bold tracking-[0.2em] text-text">RATE LIMIT</span>
            </div>
            <div class="p-3">
              <div class="flex justify-between">
                <span class="text-[10px] text-muted tracking-wider">requests_per_minute</span>
                <span class="text-xs text-text">{summary.rate_limit.requests_per_minute}</span>
              </div>
            </div>
          </section>

        </div>
      {/if}

    <!-- ══════════════ USAGE TAB ══════════════ -->
    {:else}
      <div class="p-4 space-y-4">

        <!-- FOREMAN COSTS -->
        <section class="border-2 border-border">
          <div class="px-3 py-1.5 border-b border-border bg-surface-active border-l-4 border-l-accent">
            <span class="text-[10px] font-bold tracking-[0.2em] text-text">FOREMAN COSTS</span>
          </div>
          <div class="p-3 space-y-3">
            {#each [
              { label: 'DAILY', used: appState.dailyCost, budget: appState.dailyBudget },
              { label: 'MONTHLY', used: appState.monthlyCost, budget: appState.monthlyBudget },
            ] as gauge}
              {@const pct = budgetPct(gauge.used, gauge.budget)}
              {@const barCls = budgetBarCls(pct)}
              <div>
                <div class="flex justify-between text-xs mb-1.5">
                  <span class="text-muted tracking-wider">{gauge.label}</span>
                  <span>
                    <span class="{pct >= 80 ? (pct >= 90 ? 'text-danger' : 'text-warning') : 'text-text'}">{formatCost(gauge.used)}</span>
                    {#if gauge.budget > 0}<span class="text-muted"> / ${Math.round(gauge.budget)}</span>{/if}
                  </span>
                </div>
                <div class="h-2 bg-border overflow-hidden">
                  <div class="h-full {barCls} transition-all duration-500" style="width:{pct}%"></div>
                </div>
                {#if pct >= 80}
                  <div class="text-[10px] text-warning mt-1">{Math.round(pct)}% used{pct >= 90 ? ' — near limit' : ''}</div>
                {/if}
              </div>
            {/each}

            <!-- 7-day bar chart -->
            {#if appState.weekDays.length > 0}
              <div class="h-10 flex items-end gap-0.5 mt-1">
                {#each appState.weekDays as day}
                  {@const pct = (day.cost_usd / maxDayCost) * 100}
                  {@const label = formatDayLabel(day.date)}
                  <div class="flex-1 flex flex-col items-center gap-0.5">
                    <div class="flex-1 w-full flex items-end">
                      <div
                        class="w-full bg-accent opacity-80"
                        style="height:{pct}%"
                      ></div>
                    </div>
                    <span class="text-[9px] text-muted">{label}</span>
                  </div>
                {/each}
              </div>
            {/if}
          </div>
        </section>

        <!-- ACTIVITY BREAKDOWN -->
        <section class="border-2 border-border">
          <div class="px-3 py-1.5 border-b border-border bg-surface-active border-l-4 border-l-accent">
            <span class="text-[10px] font-bold tracking-[0.2em] text-text">ACTIVITY BREAKDOWN</span>
          </div>
          {#if appState.activityBreakdown === null}
            <div class="text-muted text-[10px] tracking-wider px-3 py-4">LOADING...</div>
          {:else}
            {@const breakdown = appState.activityBreakdown}

            <!-- BY RUNNER -->
            <div class="px-3 pt-3 pb-1">
              <div class="text-[10px] font-bold tracking-[0.15em] text-muted-bright mb-1.5">BY RUNNER</div>
              {#if breakdown.by_runner.length === 0}
                <div class="text-[10px] text-muted py-1">No data</div>
              {:else}
                <div class="space-y-1">
                  {#each breakdown.by_runner as entry}
                    <div class="flex items-center justify-between text-xs">
                      <span class="text-text">{entry.runner || 'builtin'}</span>
                      <span class="text-muted">{entry.calls} calls</span>
                      <span class="text-text">{formatCost(entry.cost_usd)}</span>
                    </div>
                  {/each}
                </div>
              {/if}
            </div>

            <!-- BY MODEL -->
            <div class="px-3 pt-2 pb-1 border-t border-border mt-2">
              <div class="text-[10px] font-bold tracking-[0.15em] text-muted-bright mb-1.5">BY MODEL</div>
              {#if breakdown.by_model.length === 0}
                <div class="text-[10px] text-muted py-1">No data</div>
              {:else}
                <div class="space-y-1">
                  {#each breakdown.by_model as entry}
                    <div class="flex items-center justify-between text-xs">
                      <span class="text-text flex-1 truncate mr-2">{shortModel(entry.model)}</span>
                      <span class="text-muted mr-2">{entry.calls} calls</span>
                      <span class="text-text">{formatCost(entry.cost_usd)}</span>
                    </div>
                  {/each}
                </div>
              {/if}
            </div>

            <!-- BY ROLE -->
            {#if breakdown.by_role.length > 0}
              <div class="border-2 border-border mx-3 mt-2">
                <div class="px-3 py-1 text-[10px] tracking-widest text-muted border-l-4 border-l-accent bg-surface-active">ROLE MAPPING</div>
                {#each breakdown.by_role as row}
                  <div class="flex items-center gap-2 px-3 py-1 border-b border-border last:border-0">
                    <span class="text-[10px] text-text w-28 truncate">{row.role}</span>
                    <span class="text-[9px] border px-1 py-0.5 leading-none {runnerBadgeCls(row.runner)}">{row.runner}</span>
                    <span class="text-[10px] text-muted-bright flex-1">{shortModel(row.model)}</span>
                    <span class="text-[10px] text-text font-mono">${row.cost_usd.toFixed(4)}</span>
                  </div>
                {/each}
              </div>
            {/if}

            <!-- RECENT CALLS -->
            <div class="px-3 pt-2 pb-3 border-t border-border mt-2">
              <div class="text-[10px] font-bold tracking-[0.15em] text-muted-bright mb-1.5">RECENT CALLS</div>
              {#if breakdown.recent_calls.length === 0}
                <div class="text-[10px] text-muted py-1">No data</div>
              {:else}
                <div class="space-y-2">
                  {#each breakdown.recent_calls.slice(0, 10) as call}
                    <div class="text-[10px] space-y-0.5">
                      <div class="flex items-center gap-1.5 flex-wrap">
                        <span class="text-muted">{formatRelative(call.timestamp)}</span>
                        <span class="text-muted-bright">{call.role}</span>
                        <span class="border px-1 py-px {runnerBadgeCls(call.runner)}">{call.runner || 'builtin'}</span>
                        <span class="text-text">{shortModel(call.model)}</span>
                        <span class="text-accent ml-auto">{formatCost(call.cost_usd)}</span>
                      </div>
                      {#if call.ticket_title}
                        <button
                          class="text-[10px] text-muted hover:text-accent transition-colors underline underline-offset-2 text-left"
                          onclick={() => { if (call.ticket_id) { selectTicket(call.ticket_id); closeSettings(); } }}
                        >{call.ticket_title}</button>
                      {/if}
                    </div>
                  {/each}
                </div>
              {/if}
            </div>
          {/if}
        </section>

        <!-- CLAUDE CODE -->
        <section class="border-2 border-border">
          <div class="px-3 py-1.5 border-b border-border bg-surface-active border-l-4 border-l-accent">
            <span class="text-[10px] font-bold tracking-[0.2em] text-text">CLAUDE CODE</span>
          </div>
          {#if appState.claudeCodeUsage === null}
            <div class="text-muted text-[10px] tracking-wider px-3 py-4">LOADING...</div>
          {:else}
            {@const cc = appState.claudeCodeUsage}
            {#if !cc.available}
              <div class="text-muted text-xs px-3 py-2">Claude Code CLI data not found.</div>
            {:else}
              <div class="p-3 space-y-3">
                {#if cc.estimate_note}
                  <p class="text-[10px] italic text-muted">{cc.estimate_note}</p>
                {/if}
                <!-- Today's summary -->
                {#if cc.today}
                  <div>
                    <div class="text-[10px] font-bold tracking-[0.15em] text-muted-bright mb-1.5">TODAY</div>
                    <div class="space-y-1">
                      <div class="flex justify-between">
                        <span class="text-[10px] text-muted tracking-wider">sessions</span>
                        <span class="text-xs text-text">{cc.today.sessions}</span>
                      </div>
                      <div class="flex justify-between">
                        <span class="text-[10px] text-muted tracking-wider">input_tokens</span>
                        <span class="text-xs text-text">{formatTokens(cc.today.input_tokens)}</span>
                      </div>
                      <div class="flex justify-between">
                        <span class="text-[10px] text-muted tracking-wider">output_tokens</span>
                        <span class="text-xs text-text">{formatTokens(cc.today.output_tokens)}</span>
                      </div>
                      <div class="flex justify-between">
                        <span class="text-[10px] text-muted tracking-wider">estimated_cost</span>
                        <span class="text-xs text-text">{formatCost(cc.today.estimated_cost_usd)}</span>
                      </div>
                    </div>
                  </div>
                {/if}

                <!-- Last 7 days table -->
                {#if cc.last_7_days && cc.last_7_days.length > 0}
                  <div class="border-t border-border pt-3">
                    <div class="text-[10px] font-bold tracking-[0.15em] text-muted-bright mb-1.5">LAST 7 DAYS</div>
                    <table class="w-full text-[10px]">
                      <thead>
                        <tr class="text-muted border-b border-border">
                          <th class="text-left pb-1 font-normal tracking-wider">DATE</th>
                          <th class="text-right pb-1 font-normal tracking-wider">IN</th>
                          <th class="text-right pb-1 font-normal tracking-wider">OUT</th>
                          <th class="text-right pb-1 font-normal tracking-wider">COST</th>
                        </tr>
                      </thead>
                      <tbody>
                        {#each cc.last_7_days as day}
                          <tr class="border-b border-border/50">
                            <td class="py-1 text-muted">{day.date}</td>
                            <td class="py-1 text-right text-text">{formatTokens(day.input_tokens)}</td>
                            <td class="py-1 text-right text-text">{formatTokens(day.output_tokens)}</td>
                            <td class="py-1 text-right text-text">{formatCost(day.cost_usd)}</td>
                          </tr>
                        {/each}
                      </tbody>
                    </table>
                  </div>
                {/if}
              </div>
            {/if}
          {/if}
        </section>

      </div>
    {/if}
  </div>
</div>
