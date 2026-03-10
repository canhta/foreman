import { fetchJSON, postJSON, postJSONBody, clearToken, getToken, setOnUnauthorized } from '../api';
import type { ProjectEntry, ProjectOverview } from '../types';
import { toasts } from './toasts.svelte';

class GlobalState {
  // Auth
  authenticated = $state(!!getToken());

  // Projects
  projects = $state<ProjectEntry[]>([]);

  // Loading flag — true until first loadProjects() completes
  loading = $state(true);

  // Overview metrics
  overview = $state<ProjectOverview>({ active_tickets: 0, open_prs: 0, need_input: 0, cost_today: 0, projects: 0 });

  // Global status
  daemonState = $state<string>('stopped');
  wsConnected = $state(false);

  // Global WebSocket
  private ws: WebSocket | null = null;
  private pollIntervals: number[] = [];
  private wsStopped = false;

  async loadProjects() {
    try {
      const entries = await fetchJSON<ProjectEntry[]>('/api/projects');
      this.projects = entries;
    } catch (e) {
      console.error('loadProjects', e);
    } finally {
      this.loading = false;
    }
  }

  async loadOverview() {
    try {
      this.overview = await fetchJSON<ProjectOverview>('/api/overview');
    } catch (e) {
      console.error('loadOverview', e);
    }
  }

  async createProject(config: Record<string, unknown>): Promise<string> {
    const res = await fetch('/api/projects', {
      method: 'POST',
      headers: { Authorization: `Bearer ${getToken()}`, 'Content-Type': 'application/json' },
      body: JSON.stringify(config),
    });
    if (!res.ok) throw new Error(await res.text());
    const data = await res.json();
    if (!data.id) {
      toasts.add('Project created but no ID returned', 'error');
      throw new Error('No project ID in response');
    }
    await this.loadProjects();
    return data.id;
  }

  async deleteProject(id: string) {
    const res = await fetch(`/api/projects/${id}`, {
      method: 'DELETE',
      headers: { Authorization: `Bearer ${getToken()}` },
    });
    if (!res.ok) {
      const msg = await res.text().catch(() => `HTTP ${res.status}`);
      toasts.add(`Failed to delete project: ${msg}`, 'error');
      throw new Error(msg);
    }
    await this.loadProjects();
  }

  async daemonPause() {
    try {
      await postJSON('/api/daemon/pause');
      toasts.add('Daemon paused', 'info');
    } catch (e) {
      toasts.add(`Failed to pause daemon: ${e instanceof Error ? e.message : 'Unknown error'}`, 'error');
    }
  }

  async daemonResume() {
    try {
      await postJSON('/api/daemon/resume');
      toasts.add('Daemon resumed', 'success');
    } catch (e) {
      toasts.add(`Failed to resume daemon: ${e instanceof Error ? e.message : 'Unknown error'}`, 'error');
    }
  }

  async daemonSync() {
    try {
      await postJSON('/api/daemon/sync');
      toasts.add('Sync triggered', 'info');
    } catch (e) {
      toasts.add(`Failed to trigger sync: ${e instanceof Error ? e.message : 'Unknown error'}`, 'error');
    }
  }

  connectGlobalWebSocket() {
    this.wsStopped = false;
    const protocol = location.protocol === 'https:' ? 'wss:' : 'ws:';
    const url = `${protocol}//${location.host}/ws/global`;
    this.ws = new WebSocket(url, [`bearer.${getToken()}`]);
    this.ws.onopen = () => { this.wsConnected = true; };
    this.ws.onclose = () => {
      this.wsConnected = false;
      if (this.wsStopped) return;
      setTimeout(() => this.connectGlobalWebSocket(), 5000);
    };
    this.ws.onmessage = (ev) => {
      try {
        const event = JSON.parse(ev.data);
        if (event.severity === 'warning' || event.severity === 'error') {
          toasts.add(event.message, event.severity, event.ticket_id);
        }
        if (['ticket_status_changed', 'project_status_changed', 'task_done', 'task_failed'].includes(event.event_type)) {
          this.loadProjects();
          this.loadOverview();
        }
      } catch {}
    };
  }

  startPolling() {
    this.loadProjects();
    this.loadOverview();
    this.pollIntervals.push(
      window.setInterval(() => this.loadProjects(), 30000),
      window.setInterval(() => this.loadOverview(), 15000),
    );
    this.connectGlobalWebSocket();
  }

  stopPolling() {
    this.wsStopped = true;
    this.pollIntervals.forEach(clearInterval);
    this.pollIntervals = [];
    this.ws?.close();
  }

  logout() {
    this.stopPolling();
    clearToken();
    this.authenticated = false;
    this.projects = [];
  }
}

export const globalState = new GlobalState();

setOnUnauthorized(() => globalState.logout());
