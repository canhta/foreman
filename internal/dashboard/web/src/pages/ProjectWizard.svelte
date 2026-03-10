<script lang="ts">
  import { globalState } from '../state/global.svelte';
  import { getToken } from '../api';
  import { toasts } from '../state/toasts.svelte';
  import { push } from 'svelte-spa-router';

  // Step state
  let step = $state(1);
  const totalSteps = 5;

  // Form fields
  let name = $state('');
  let description = $state('');

  let gitProvider = $state<'github' | 'gitlab' | 'gitea' | 'other'>('github');
  let gitCloneUrl = $state('');
  let gitBranch = $state('main');
  let gitToken = $state('');
  let testingGit = $state(false);
  let gitTestResult = $state<{ ok: boolean; message: string } | null>(null);

  let trackerProvider = $state<'github' | 'jira' | 'linear' | 'local'>('github');
  let trackerEmail = $state('');
  let trackerToken = $state('');
  let trackerProjectKey = $state('');
  let trackerLabels = $state('');
  let trackerUrl = $state('');
  let testingTracker = $state(false);
  let trackerTestResult = $state<{ ok: boolean; message: string } | null>(null);

  let agentRunner = $state<'builtin' | 'claudecode' | 'copilot'>('builtin');
  let modelPlanner = $state('');
  let modelImplementer = $state('');

  let creating = $state(false);
  let createError = $state('');

  function canProceed(): boolean {
    switch (step) {
      case 1: return name.trim().length > 0;
      case 2: return gitCloneUrl.trim().length > 0;
      case 3: return trackerProvider === 'local' || trackerProjectKey.trim().length > 0;
      case 4: return true;
      case 5: return true;
      default: return false;
    }
  }

  async function testGitConnection() {
    testingGit = true;
    gitTestResult = null;
    try {
      const res = await fetch('/api/projects/test-connection', {
        method: 'POST',
        headers: { Authorization: `Bearer ${getToken()}`, 'Content-Type': 'application/json' },
        body: JSON.stringify({ type: 'git', clone_url: gitCloneUrl, token: gitToken }),
      });
      const data = await res.json();
      gitTestResult = { ok: data.ok, message: data.ok ? 'Connection successful' : (data.error ?? 'Failed') };
    } catch (e: any) {
      gitTestResult = { ok: false, message: e.message };
    } finally {
      testingGit = false;
    }
  }

  async function testTrackerConnection() {
    testingTracker = true;
    trackerTestResult = null;
    try {
      const res = await fetch('/api/projects/test-connection', {
        method: 'POST',
        headers: { Authorization: `Bearer ${getToken()}`, 'Content-Type': 'application/json' },
        body: JSON.stringify({ type: 'tracker', provider: trackerProvider, email: trackerEmail, token: trackerToken, project_key: trackerProjectKey, url: trackerUrl }),
      });
      const data = await res.json();
      trackerTestResult = { ok: data.ok, message: data.ok ? 'Connection successful' : (data.error ?? 'Failed') };
    } catch (e: any) {
      trackerTestResult = { ok: false, message: e.message };
    } finally {
      testingTracker = false;
    }
  }

  async function createProject() {
    creating = true;
    createError = '';
    try {
      const config: Record<string, unknown> = {
        name: name.trim(),
        description: description.trim(),
        git_clone_url: gitCloneUrl.trim(),
        git_default_branch: gitBranch.trim() || 'main',
        git_token: gitToken,
        git_provider: gitProvider,
        tracker_provider: trackerProvider,
        tracker_token: trackerToken,
        tracker_project_key: trackerProjectKey.trim(),
        tracker_labels: trackerLabels.trim(),
        tracker_url: trackerUrl.trim(),
        tracker_email: trackerEmail.trim(),
        agent_runner: agentRunner,
        model_planner: modelPlanner.trim(),
        model_implementer: modelImplementer.trim(),
      };
      const id = await globalState.createProject(config);
      toasts.add(`Project "${name}" created`, 'success');
      push(`/projects/${id}/board`);
    } catch (e: any) {
      createError = e.message ?? 'Failed to create project';
    } finally {
      creating = false;
    }
  }

  const stepLabels = ['Basics', 'Repository', 'Tracker', 'Config', 'Review'];
</script>

<div class="min-h-screen p-6 max-w-2xl mx-auto">
  <h1 class="text-sm font-bold tracking-[0.3em] text-[var(--color-accent)] mb-6">NEW PROJECT</h1>

  <!-- Step indicator -->
  <div class="flex items-center gap-0 mb-4">
    {#each stepLabels as label, i}
      {@const n = i + 1}
      <div class="flex items-center">
        <div class="flex items-center gap-2">
          <div
            class="flex items-center justify-center text-[10px] font-bold border transition-colors"
            class:w-7={step === n}
            class:h-7={step === n}
            class:w-6={step !== n}
            class:h-6={step !== n}
            class:bg-[var(--color-accent)]={step === n}
            class:text-[var(--color-bg)]={step === n}
            class:border-[var(--color-accent)]={step === n}
            class:border-[var(--color-border)]={step !== n}
            class:text-[var(--color-muted)]={step < n}
            class:text-[var(--color-success)]={step > n}
          >
            {step > n ? '✓' : n}
          </div>
          <span class="text-[10px] tracking-wider hidden sm:inline"
                class:text-[var(--color-accent)]={step === n}
                class:font-bold={step === n}
                class:text-[var(--color-muted)]={step !== n}>
            {label}
          </span>
        </div>
        {#if i < stepLabels.length - 1}
          <div
            class="w-8 h-px mx-2"
            class:bg-[var(--color-success)]={step > n + 1}
            class:bg-[var(--color-border)]={step <= n + 1}
          ></div>
        {/if}
      </div>
    {/each}
  </div>

  <!-- Step progress bar -->
  <div class="flex items-center gap-3 mb-6">
    <div class="progress-track flex-1">
      <div class="progress-fill" style="width: {(step / totalSteps) * 100}%"></div>
    </div>
    <span class="text-[10px] text-[var(--color-muted)] tracking-widest shrink-0">STEP {step} OF {totalSteps}</span>
  </div>

  <!-- Step content -->
  <div class="border border-[var(--color-border)] p-6 animate-[zoom-in_0.15s_ease-out]">

    {#if step === 1}
      <!-- Step 1: Basics -->
      <h2 class="text-xs font-bold tracking-widest text-[var(--color-text)] mb-4 uppercase">Project Basics</h2>
      <div class="space-y-4">
        <label class="block">
          <span class="text-[10px] tracking-widest text-[var(--color-muted)] uppercase">Project Name *</span>
          <input
            bind:value={name}
            placeholder="my-project"
            class="mt-1 w-full bg-[var(--color-surface)] border border-[var(--color-border)] px-3 py-2 text-xs text-[var(--color-text)] focus:border-[var(--color-accent)] focus:outline-none"
          />
        </label>
        <label class="block">
          <span class="text-[10px] tracking-widest text-[var(--color-muted)] uppercase">Description</span>
          <textarea
            bind:value={description}
            rows="3"
            placeholder="What does this project do?"
            class="mt-1 w-full bg-[var(--color-surface)] border border-[var(--color-border)] px-3 py-2 text-xs text-[var(--color-text)] focus:border-[var(--color-accent)] focus:outline-none resize-none"
          ></textarea>
        </label>
      </div>

    {:else if step === 2}
      <!-- Step 2: Repository -->
      <h2 class="text-xs font-bold tracking-widest text-[var(--color-text)] mb-4 uppercase">Repository</h2>
      <div class="space-y-4">
        <label class="block">
          <span class="text-[10px] tracking-widest text-[var(--color-muted)] uppercase">Provider</span>
          <select bind:value={gitProvider} class="mt-1 w-full bg-[var(--color-surface)] border border-[var(--color-border)] px-3 py-2 text-xs text-[var(--color-text)] focus:border-[var(--color-accent)] focus:outline-none">
            <option value="github">GitHub</option>
            <option value="gitlab">GitLab</option>
            <option value="gitea">Gitea</option>
            <option value="other">Other</option>
          </select>
        </label>
        <label class="block">
          <span class="text-[10px] tracking-widest text-[var(--color-muted)] uppercase">Clone URL *</span>
          <input
            bind:value={gitCloneUrl}
            placeholder="https://github.com/owner/repo.git"
            class="mt-1 w-full bg-[var(--color-surface)] border border-[var(--color-border)] px-3 py-2 text-xs text-[var(--color-text)] focus:border-[var(--color-accent)] focus:outline-none"
          />
        </label>
        <label class="block">
          <span class="text-[10px] tracking-widest text-[var(--color-muted)] uppercase">Default Branch</span>
          <input
            bind:value={gitBranch}
            placeholder="main"
            class="mt-1 w-full bg-[var(--color-surface)] border border-[var(--color-border)] px-3 py-2 text-xs text-[var(--color-text)] focus:border-[var(--color-accent)] focus:outline-none"
          />
        </label>
        <label class="block">
          <span class="text-[10px] tracking-widest text-[var(--color-muted)] uppercase">Access Token</span>
          <input
            type="password"
            bind:value={gitToken}
            placeholder="ghp_..."
            class="mt-1 w-full bg-[var(--color-surface)] border border-[var(--color-border)] px-3 py-2 text-xs text-[var(--color-text)] focus:border-[var(--color-accent)] focus:outline-none"
          />
        </label>
        <div class="flex flex-wrap items-center gap-3">
          <button
            onclick={testGitConnection}
            disabled={testingGit || !gitCloneUrl.trim()}
            class="text-[10px] px-3 py-1.5 border border-[var(--color-border)] text-[var(--color-accent)] hover:bg-[var(--color-accent-bg)] disabled:opacity-50 tracking-wider"
          >
            {testingGit ? 'TESTING...' : 'TEST CONNECTION'}
          </button>
          {#if gitTestResult}
            <span class="status-chip" class:status-chip-done={gitTestResult.ok} class:status-chip-failed={!gitTestResult.ok}>
              {gitTestResult.message}
            </span>
          {/if}
        </div>
      </div>

    {:else if step === 3}
      <!-- Step 3: Tracker -->
      <h2 class="text-xs font-bold tracking-widest text-[var(--color-text)] mb-4 uppercase">Issue Tracker</h2>
      <div class="space-y-4">
        <label class="block">
          <span class="text-[10px] tracking-widest text-[var(--color-muted)] uppercase">Provider</span>
          <select bind:value={trackerProvider} class="mt-1 w-full bg-[var(--color-surface)] border border-[var(--color-border)] px-3 py-2 text-xs text-[var(--color-text)] focus:border-[var(--color-accent)] focus:outline-none">
            <option value="github">GitHub Issues</option>
            <option value="jira">Jira</option>
            <option value="linear">Linear</option>
            <option value="local">Local File</option>
          </select>
        </label>

        {#if trackerProvider !== 'local'}
          {#if trackerProvider === 'jira'}
            <label class="block">
              <span class="text-[10px] tracking-widest text-[var(--color-muted)] uppercase">Jira Base URL</span>
              <input
                bind:value={trackerUrl}
                placeholder="https://yourorg.atlassian.net"
                class="mt-1 w-full bg-[var(--color-surface)] border border-[var(--color-border)] px-3 py-2 text-xs text-[var(--color-text)] focus:border-[var(--color-accent)] focus:outline-none"
              />
            </label>
            <label class="block">
              <span class="text-[10px] tracking-widest text-[var(--color-muted)] uppercase">Atlassian Email</span>
              <input
                bind:value={trackerEmail}
                placeholder="you@example.com"
                class="mt-1 w-full bg-[var(--color-surface)] border border-[var(--color-border)] px-3 py-2 text-xs text-[var(--color-text)] focus:border-[var(--color-accent)] focus:outline-none"
              />
            </label>
          {/if}
          <label class="block">
            <span class="text-[10px] tracking-widest text-[var(--color-muted)] uppercase">
              {trackerProvider === 'jira' ? 'Project Key *' : trackerProvider === 'github' ? 'Repo (owner/repo) *' : 'Team ID *'}
            </span>
            <input
              bind:value={trackerProjectKey}
              placeholder={trackerProvider === 'jira' ? 'PROJ' : trackerProvider === 'github' ? 'owner/repo' : 'team-id'}
              class="mt-1 w-full bg-[var(--color-surface)] border border-[var(--color-border)] px-3 py-2 text-xs text-[var(--color-text)] focus:border-[var(--color-accent)] focus:outline-none"
            />
          </label>
          <label class="block">
            <span class="text-[10px] tracking-widest text-[var(--color-muted)] uppercase">Access Token</span>
            <input
              type="password"
              bind:value={trackerToken}
              class="mt-1 w-full bg-[var(--color-surface)] border border-[var(--color-border)] px-3 py-2 text-xs text-[var(--color-text)] focus:border-[var(--color-accent)] focus:outline-none"
            />
          </label>
          <label class="block">
            <span class="text-[10px] tracking-widest text-[var(--color-muted)] uppercase">Labels (comma-separated)</span>
            <input
              bind:value={trackerLabels}
              placeholder="foreman, ready"
              class="mt-1 w-full bg-[var(--color-surface)] border border-[var(--color-border)] px-3 py-2 text-xs text-[var(--color-text)] focus:border-[var(--color-accent)] focus:outline-none"
            />
          </label>
          <div class="flex flex-wrap items-center gap-3">
            <button
              onclick={testTrackerConnection}
              disabled={testingTracker || !trackerProjectKey.trim()}
              class="text-[10px] px-3 py-1.5 border border-[var(--color-border)] text-[var(--color-accent)] hover:bg-[var(--color-accent-bg)] disabled:opacity-50 tracking-wider"
            >
              {testingTracker ? 'TESTING...' : 'TEST CONNECTION'}
            </button>
            {#if trackerTestResult}
              <span class="status-chip" class:status-chip-done={trackerTestResult.ok} class:status-chip-failed={!trackerTestResult.ok}>
                {trackerTestResult.message}
              </span>
            {/if}
          </div>
        {:else}
          <p class="text-xs text-[var(--color-muted)]">Local file tracker — issues will be read from a YAML file in the repository.</p>
        {/if}
      </div>

    {:else if step === 4}
      <!-- Step 4: Configuration -->
      <h2 class="text-xs font-bold tracking-widest text-[var(--color-text)] mb-4 uppercase">Configuration</h2>
      <div class="space-y-4">
        <label class="block">
          <span class="text-[10px] tracking-widest text-[var(--color-muted)] uppercase">Agent Runner</span>
          <select bind:value={agentRunner} class="mt-1 w-full bg-[var(--color-surface)] border border-[var(--color-border)] px-3 py-2 text-xs text-[var(--color-text)] focus:border-[var(--color-accent)] focus:outline-none">
            <option value="builtin">Builtin (default)</option>
            <option value="claudecode">Claude Code</option>
            <option value="copilot">GitHub Copilot</option>
          </select>
        </label>
        <label class="block">
          <span class="text-[10px] tracking-widest text-[var(--color-muted)] uppercase">Planner Model</span>
          <input
            bind:value={modelPlanner}
            placeholder="leave blank for default"
            class="mt-1 w-full bg-[var(--color-surface)] border border-[var(--color-border)] px-3 py-2 text-xs text-[var(--color-text)] focus:border-[var(--color-accent)] focus:outline-none"
          />
        </label>
        <label class="block">
          <span class="text-[10px] tracking-widest text-[var(--color-muted)] uppercase">Implementer Model</span>
          <input
            bind:value={modelImplementer}
            placeholder="leave blank for default"
            class="mt-1 w-full bg-[var(--color-surface)] border border-[var(--color-border)] px-3 py-2 text-xs text-[var(--color-text)] focus:border-[var(--color-accent)] focus:outline-none"
          />
        </label>
      </div>

    {:else if step === 5}
      <!-- Step 5: Review -->
      <h2 class="text-xs font-bold tracking-widest text-[var(--color-text)] mb-4 uppercase">Review & Create</h2>
      <div class="space-y-3 text-xs">
        <div class="border border-[var(--color-border)] divide-y divide-[var(--color-border)]">
          <!-- Header row -->
          <div class="px-3 py-2 flex justify-between bg-[var(--color-surface-active)]">
            <span class="text-[10px] uppercase tracking-widest text-[var(--color-muted)]">FIELD</span>
            <span class="text-[10px] uppercase tracking-widest text-[var(--color-muted)]">VALUE</span>
          </div>
          <div class="px-3 py-2 flex justify-between">
            <span class="text-[10px] uppercase tracking-widest text-[var(--color-muted)]">Name</span>
            <span class="text-xs font-mono text-[var(--color-text)]">{name}</span>
          </div>
          {#if description}
            <div class="px-3 py-2 flex justify-between">
              <span class="text-[10px] uppercase tracking-widest text-[var(--color-muted)]">Description</span>
              <span class="text-xs font-mono text-[var(--color-text)] truncate max-w-[60%] text-right">{description}</span>
            </div>
          {/if}
          <div class="px-3 py-2 flex justify-between">
            <span class="text-[10px] uppercase tracking-widest text-[var(--color-muted)]">Repository</span>
            <span class="text-xs font-mono text-[var(--color-text)] truncate max-w-[60%] text-right">{gitCloneUrl}</span>
          </div>
          <div class="px-3 py-2 flex justify-between">
            <span class="text-[10px] uppercase tracking-widest text-[var(--color-muted)]">Branch</span>
            <span class="text-xs font-mono text-[var(--color-text)]">{gitBranch || 'main'}</span>
          </div>
          <div class="px-3 py-2 flex justify-between">
            <span class="text-[10px] uppercase tracking-widest text-[var(--color-muted)]">Tracker</span>
            <span class="text-xs font-mono text-[var(--color-text)]">{trackerProvider}{trackerProjectKey ? ` / ${trackerProjectKey}` : ''}</span>
          </div>
          <div class="px-3 py-2 flex justify-between">
            <span class="text-[10px] uppercase tracking-widest text-[var(--color-muted)]">Agent Runner</span>
            <span class="text-xs font-mono text-[var(--color-text)]">{agentRunner}</span>
          </div>
          {#if modelPlanner}
            <div class="px-3 py-2 flex justify-between">
              <span class="text-[10px] uppercase tracking-widest text-[var(--color-muted)]">Planner Model</span>
              <span class="text-xs font-mono text-[var(--color-text)]">{modelPlanner}</span>
            </div>
          {/if}
          {#if modelImplementer}
            <div class="px-3 py-2 flex justify-between">
              <span class="text-[10px] uppercase tracking-widest text-[var(--color-muted)]">Implementer Model</span>
              <span class="text-xs font-mono text-[var(--color-text)]">{modelImplementer}</span>
            </div>
          {/if}
        </div>

        {#if createError}
          <p class="text-[var(--color-danger)] text-xs">{createError}</p>
        {/if}
      </div>
    {/if}
  </div>

  <!-- Navigation buttons -->
  <div class="flex justify-between mt-6">
    <div>
      {#if step > 1}
        <button
          onclick={() => step--}
          class="px-4 py-2 text-[10px] tracking-widest border border-[var(--color-border)] text-[var(--color-muted)] hover:text-[var(--color-text)] hover:border-[var(--color-border-strong)] transition-colors"
        >
          ← PREVIOUS
        </button>
      {:else}
        <button
          onclick={() => push('/')}
          class="px-4 py-2 text-[10px] tracking-widest text-[var(--color-muted)] hover:text-[var(--color-text)] transition-colors"
        >
          Cancel
        </button>
      {/if}
    </div>

    <div>
      {#if step < totalSteps}
        <button
          onclick={() => step++}
          disabled={!canProceed()}
          class="px-6 py-2 text-[10px] tracking-widest bg-[var(--color-accent)] text-[var(--color-bg)] font-bold disabled:opacity-40 hover:opacity-90 transition-opacity"
        >
          NEXT →
        </button>
      {:else}
        <button
          onclick={createProject}
          disabled={creating}
          class="px-6 py-2 text-[10px] tracking-widest bg-[var(--color-accent)] text-[var(--color-bg)] font-bold disabled:opacity-40 hover:opacity-90 transition-opacity"
        >
          {creating ? 'CREATING...' : 'CREATE PROJECT'}
        </button>
      {/if}
    </div>
  </div>
</div>
