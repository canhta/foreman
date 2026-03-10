<script lang="ts">
  import { projectState } from '../state/project.svelte';
  import { globalState } from '../state/global.svelte';
  import { fetchJSON, getToken } from '../api';
  import { toasts } from '../state/toasts.svelte';
  import { push } from 'svelte-spa-router';
  import type { ProjectConfig } from '../types';
  import ProjectTabs from '../components/ProjectTabs.svelte';
  import ConfirmDialog from '../components/ConfirmDialog.svelte';

  let { params }: { params: { pid: string } } = $props();

  const project = $derived(globalState.projects.find(p => p.id === params.pid));

  $effect(() => {
    if (params.pid) {
      projectState.switchProject(params.pid);
      loadConfig();
    }
  });

  let config = $state<Partial<ProjectConfig>>({});
  let saving = $state(false);
  let testingGit = $state(false);
  let testingTracker = $state(false);
  let showDeleteConfirm = $state(false);
  let expandedSections = $state<Record<string, boolean>>({
    project: true, git: true, tracker: true, models: false, limits: false, danger: false
  });

  async function loadConfig() {
    try {
      config = await fetchJSON(`/api/projects/${params.pid}`);
    } catch (e) {
      console.error('loadConfig', e);
    }
  }

  async function saveConfig() {
    saving = true;
    try {
      const res = await fetch(`/api/projects/${params.pid}`, {
        method: 'PUT',
        headers: { Authorization: `Bearer ${getToken()}`, 'Content-Type': 'application/json' },
        body: JSON.stringify(config),
      });
      if (!res.ok) throw new Error(await res.text());
      toasts.add('Settings saved', 'success');
      await globalState.loadProjects();
    } catch (e: any) {
      toasts.add(`Save failed: ${e.message}`, 'error');
    } finally {
      saving = false;
    }
  }

  async function testConnection(type: 'git' | 'tracker') {
    if (type === 'git') testingGit = true;
    else testingTracker = true;
    try {
      const res = await fetch(`/api/projects/${params.pid}/config/test`, {
        method: 'POST',
        headers: { Authorization: `Bearer ${getToken()}`, 'Content-Type': 'application/json' },
        body: JSON.stringify({ type }),
      });
      const data = await res.json();
      toasts.add(data.ok ? `${type} connection OK` : `${type}: ${data.error}`, data.ok ? 'success' : 'error');
    } catch (e: any) {
      toasts.add(`Test failed: ${e.message}`, 'error');
    } finally {
      if (type === 'git') testingGit = false;
      else testingTracker = false;
    }
  }

  async function deleteProject() {
    await globalState.deleteProject(params.pid);
    push('/');
  }

  function toggleSection(key: string) {
    expandedSections[key] = !expandedSections[key];
  }
</script>

{#if project}
  <ProjectTabs projectId={params.pid} projectName={project.name} />
{/if}

<div class="p-6 max-w-3xl">
  <!-- Section: Project -->
  <div class="border border-[var(--color-border)] mb-4 animate-fade-in">
    <button onclick={() => toggleSection('project')}
            class="w-full px-4 py-3 flex items-center justify-between text-left hover:bg-[var(--color-surface-hover)] transition-colors">
      <span class="text-[10px] tracking-widest text-[var(--color-muted)] uppercase">Project</span>
      <span class="text-[var(--color-muted)] text-xs">{expandedSections.project ? '−' : '+'}</span>
    </button>
    {#if expandedSections.project}
      <div class="px-4 pb-4 border-t border-[var(--color-border)] pt-3 space-y-3">
        <label class="block">
          <span class="text-[10px] tracking-widest text-[var(--color-muted)] uppercase">Name</span>
          <input bind:value={config.name} class="mt-1 w-full bg-[var(--color-surface)] border border-[var(--color-border)] px-3 py-2 text-xs text-[var(--color-text)] focus:border-[var(--color-accent)] focus:outline-none" />
        </label>
        <label class="block">
          <span class="text-[10px] tracking-widest text-[var(--color-muted)] uppercase">Description</span>
          <textarea bind:value={config.description} rows="2" class="mt-1 w-full bg-[var(--color-surface)] border border-[var(--color-border)] px-3 py-2 text-xs text-[var(--color-text)] focus:border-[var(--color-accent)] focus:outline-none resize-none"></textarea>
        </label>
      </div>
    {/if}
  </div>

  <!-- Section: Git -->
  <div class="border border-[var(--color-border)] mb-4">
    <button onclick={() => toggleSection('git')}
            class="w-full px-4 py-3 flex items-center justify-between text-left hover:bg-[var(--color-surface-hover)] transition-colors">
      <span class="text-[10px] tracking-widest text-[var(--color-muted)] uppercase">Git</span>
      <span class="text-[var(--color-muted)] text-xs">{expandedSections.git ? '−' : '+'}</span>
    </button>
    {#if expandedSections.git}
      <div class="px-4 pb-4 border-t border-[var(--color-border)] pt-3 space-y-3">
        <label class="block">
          <span class="text-[10px] tracking-widest text-[var(--color-muted)] uppercase">Clone URL</span>
          <input bind:value={config.git_clone_url} class="mt-1 w-full bg-[var(--color-surface)] border border-[var(--color-border)] px-3 py-2 text-xs text-[var(--color-text)] focus:border-[var(--color-accent)] focus:outline-none" />
        </label>
        <label class="block">
          <span class="text-[10px] tracking-widest text-[var(--color-muted)] uppercase">Default Branch</span>
          <input bind:value={config.git_default_branch} class="mt-1 w-full bg-[var(--color-surface)] border border-[var(--color-border)] px-3 py-2 text-xs text-[var(--color-text)] focus:border-[var(--color-accent)] focus:outline-none" />
        </label>
        <label class="block">
          <span class="text-[10px] tracking-widest text-[var(--color-muted)] uppercase">Access Token</span>
          <input type="password" bind:value={config.git_token} class="mt-1 w-full bg-[var(--color-surface)] border border-[var(--color-border)] px-3 py-2 text-xs text-[var(--color-text)] focus:border-[var(--color-accent)] focus:outline-none" />
        </label>
        <button onclick={() => testConnection('git')} disabled={testingGit}
                class="text-[10px] px-3 py-1.5 border border-[var(--color-border)] text-[var(--color-accent)] hover:bg-[var(--color-accent-bg)] disabled:opacity-50 tracking-wider">
          {testingGit ? 'TESTING...' : 'TEST CONNECTION'}
        </button>
      </div>
    {/if}
  </div>

  <!-- Section: Tracker -->
  <div class="border border-[var(--color-border)] mb-4">
    <button onclick={() => toggleSection('tracker')}
            class="w-full px-4 py-3 flex items-center justify-between text-left hover:bg-[var(--color-surface-hover)] transition-colors">
      <span class="text-[10px] tracking-widest text-[var(--color-muted)] uppercase">Tracker</span>
      <span class="text-[var(--color-muted)] text-xs">{expandedSections.tracker ? '−' : '+'}</span>
    </button>
    {#if expandedSections.tracker}
      <div class="px-4 pb-4 border-t border-[var(--color-border)] pt-3 space-y-3">
        <label class="block">
          <span class="text-[10px] tracking-widest text-[var(--color-muted)] uppercase">Provider</span>
          <select bind:value={config.tracker_provider} class="mt-1 w-full bg-[var(--color-surface)] border border-[var(--color-border)] px-3 py-2 text-xs text-[var(--color-text)] focus:border-[var(--color-accent)] focus:outline-none">
            <option value="github">GitHub Issues</option>
            <option value="jira">Jira</option>
            <option value="linear">Linear</option>
            <option value="local">Local</option>
          </select>
        </label>
        <label class="block">
          <span class="text-[10px] tracking-widest text-[var(--color-muted)] uppercase">Labels</span>
          <input bind:value={config.tracker_labels} class="mt-1 w-full bg-[var(--color-surface)] border border-[var(--color-border)] px-3 py-2 text-xs text-[var(--color-text)] focus:border-[var(--color-accent)] focus:outline-none" placeholder="comma-separated" />
        </label>
        <button onclick={() => testConnection('tracker')} disabled={testingTracker}
                class="text-[10px] px-3 py-1.5 border border-[var(--color-border)] text-[var(--color-accent)] hover:bg-[var(--color-accent-bg)] disabled:opacity-50 tracking-wider">
          {testingTracker ? 'TESTING...' : 'TEST CONNECTION'}
        </button>
      </div>
    {/if}
  </div>

  <!-- Section: Models (collapsed by default) -->
  <div class="border border-[var(--color-border)] mb-4">
    <button onclick={() => toggleSection('models')}
            class="w-full px-4 py-3 flex items-center justify-between text-left hover:bg-[var(--color-surface-hover)] transition-colors">
      <span class="text-[10px] tracking-widest text-[var(--color-muted)] uppercase">Models</span>
      <span class="text-[var(--color-muted)] text-xs">{expandedSections.models ? '−' : '+'}</span>
    </button>
    {#if expandedSections.models}
      <div class="px-4 pb-4 border-t border-[var(--color-border)] pt-3 space-y-3">
        <label class="block">
          <span class="text-[10px] tracking-widest text-[var(--color-muted)] uppercase">Planner</span>
          <input bind:value={config.model_planner} class="mt-1 w-full bg-[var(--color-surface)] border border-[var(--color-border)] px-3 py-2 text-xs text-[var(--color-text)] focus:border-[var(--color-accent)] focus:outline-none" />
        </label>
        <label class="block">
          <span class="text-[10px] tracking-widest text-[var(--color-muted)] uppercase">Implementer</span>
          <input bind:value={config.model_implementer} class="mt-1 w-full bg-[var(--color-surface)] border border-[var(--color-border)] px-3 py-2 text-xs text-[var(--color-text)] focus:border-[var(--color-accent)] focus:outline-none" />
        </label>
      </div>
    {/if}
  </div>

  <!-- Section: Limits (collapsed by default) -->
  <div class="border border-[var(--color-border)] mb-4">
    <button onclick={() => toggleSection('limits')}
            class="w-full px-4 py-3 flex items-center justify-between text-left hover:bg-[var(--color-surface-hover)] transition-colors">
      <span class="text-[10px] tracking-widest text-[var(--color-muted)] uppercase">Limits</span>
      <span class="text-[var(--color-muted)] text-xs">{expandedSections.limits ? '−' : '+'}</span>
    </button>
    {#if expandedSections.limits}
      <div class="px-4 pb-4 border-t border-[var(--color-border)] pt-3 space-y-3">
        <label class="block">
          <span class="text-[10px] tracking-widest text-[var(--color-muted)] uppercase">Max Parallel Tickets</span>
          <input type="number" bind:value={config.max_parallel_tickets} min="1" max="3" class="mt-1 w-full bg-[var(--color-surface)] border border-[var(--color-border)] px-3 py-2 text-xs text-[var(--color-text)] focus:border-[var(--color-accent)] focus:outline-none" />
        </label>
        <label class="block">
          <span class="text-[10px] tracking-widest text-[var(--color-muted)] uppercase">Max Tasks Per Ticket</span>
          <input type="number" bind:value={config.max_tasks_per_ticket} class="mt-1 w-full bg-[var(--color-surface)] border border-[var(--color-border)] px-3 py-2 text-xs text-[var(--color-text)] focus:border-[var(--color-accent)] focus:outline-none" />
        </label>
        <label class="block">
          <span class="text-[10px] tracking-widest text-[var(--color-muted)] uppercase">Max Cost Per Ticket ($)</span>
          <input type="number" step="0.01" bind:value={config.max_cost_per_ticket} class="mt-1 w-full bg-[var(--color-surface)] border border-[var(--color-border)] px-3 py-2 text-xs text-[var(--color-text)] focus:border-[var(--color-accent)] focus:outline-none" />
        </label>
      </div>
    {/if}
  </div>

  <!-- Save button -->
  <div class="mb-8">
    <button onclick={saveConfig} disabled={saving}
            class="px-6 py-2 bg-[var(--color-accent)] text-[var(--color-bg)] text-[10px] font-bold tracking-widest disabled:opacity-50 hover:opacity-90 transition-opacity">
      {saving ? 'SAVING...' : 'SAVE SETTINGS'}
    </button>
  </div>

  <!-- Danger zone -->
  <div class="border border-[var(--color-danger)] mb-4">
    <button onclick={() => toggleSection('danger')}
            class="w-full px-4 py-3 flex items-center justify-between text-left hover:bg-[var(--color-danger-bg)] transition-colors">
      <span class="text-[10px] tracking-widest text-[var(--color-danger)] uppercase">Danger Zone</span>
      <span class="text-[var(--color-danger)] text-xs">{expandedSections.danger ? '−' : '+'}</span>
    </button>
    {#if expandedSections.danger}
      <div class="px-4 pb-4 border-t border-[var(--color-danger)] pt-3">
        <p class="text-xs text-[var(--color-muted)] mb-3">Permanently delete this project and all its data.</p>
        <button onclick={() => showDeleteConfirm = true}
                class="text-[10px] px-3 py-1.5 border border-[var(--color-danger)] text-[var(--color-danger)] hover:bg-[var(--color-danger-bg)] tracking-wider">
          DELETE PROJECT
        </button>
      </div>
    {/if}
  </div>
</div>

<ConfirmDialog
  open={showDeleteConfirm}
  title="Delete Project"
  message="This will permanently delete the project, its database, and all ticket history. This cannot be undone."
  confirmLabel="DELETE"
  onconfirm={deleteProject}
  oncancel={() => showDeleteConfirm = false}
/>
