import { fetchJSON, postJSONBody, clearToken, getToken, setOnUnauthorized } from '../api';
import type { ProjectEntry, ProjectOverview } from '../types';
import { toasts } from './toasts.svelte';

class GlobalState {
  // Auth
  authenticated = $state(!!getToken());

  // Projects
  projects = $state<ProjectEntry[]>([]);

  // Overview metrics
  overview = $state<ProjectOverview>({ active_tickets: 0, open_prs: 0, need_input: 0, cost_today: 0, projects: 0 });

  // Global status
  daemonState = $state<string>('stopped');
  wsConnected = $state(false);

  // Global WebSocket
  private ws: WebSocket | null = null;
  private pollIntervals: number[] = [];

  async loadProjects() {
    try {
      const entries = await fetchJSON<ProjectEntry[]>('/api/projects');
      this.projects = entries;
    } catch (e) {
      console.error('loadProjects', e);
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
    await this.loadProjects();
    return data.id;
  }

  async deleteProject(id: string) {
    await fetch(`/api/projects/${id}`, {
      method: 'DELETE',
      headers: { Authorization: `Bearer ${getToken()}` },
    });
    await this.loadProjects();
  }

  connectGlobalWebSocket() {
    const protocol = location.protocol === 'https:' ? 'wss:' : 'ws:';
    const url = `${protocol}//${location.host}/ws/global`;
    this.ws = new WebSocket(url, [`bearer.${getToken()}`]);
    this.ws.onopen = () => { this.wsConnected = true; };
    this.ws.onclose = () => {
      this.wsConnected = false;
      setTimeout(() => this.connectGlobalWebSocket(), 5000);
    };
    this.ws.onmessage = (ev) => {
      try {
        const event = JSON.parse(ev.data);
        // Route to project-specific handlers or global overview refresh
        if (event.severity === 'warning' || event.severity === 'error') {
          toasts.add(event.message, event.severity, event.ticket_id);
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
