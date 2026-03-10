<script lang="ts">
  import { projectState } from '../state/project.svelte';
  import { globalState } from '../state/global.svelte';
  import ProjectTabs from '../components/ProjectTabs.svelte';
  import TicketCard from '../components/TicketCard.svelte';
  import TicketPanel from '../components/TicketPanel.svelte';
  import TicketFullView from '../components/TicketFullView.svelte';
  import type { TicketSummary } from '../types';

  let { params }: { params: { pid: string } } = $props();

  const project = $derived(globalState.projects.find((p: { id: string; name: string }) => p.id === params.pid));

  $effect(() => {
    if (params.pid) {
      projectState.switchProject(params.pid);
    }
  });

  const columns = [
    { label: 'Queued',         statuses: ['queued', 'clarification_needed', 'decomposed'], accent: 'border-t-[var(--color-border-strong)]' },
    { label: 'Planning',       statuses: ['planning', 'plan_validating', 'decomposing'],   accent: 'border-t-[var(--color-accent-dim)]' },
    { label: 'In Progress',    statuses: ['implementing'],                                  accent: 'border-t-[var(--color-accent)]' },
    { label: 'In Review',      statuses: ['reviewing'],                                     accent: 'border-t-[var(--color-info)]' },
    { label: 'Awaiting Merge', statuses: ['pr_created', 'pr_updated', 'awaiting_merge'],   accent: 'border-t-[var(--color-warning)]' },
    { label: 'Done',           statuses: ['done', 'merged'],                                accent: 'border-t-[var(--color-success)]' },
    { label: 'Failed',         statuses: ['failed', 'blocked', 'partial'],                  accent: 'border-t-[var(--color-danger)]' },
  ] as const;

  function ticketsForColumn(statuses: readonly string[]): TicketSummary[] {
    return projectState.tickets.filter(t => statuses.includes(t.Status));
  }

  // Dot color per column accent
  function columnDotColor(label: string): string {
    switch (label) {
      case 'In Progress':    return 'text-[var(--color-accent)]';
      case 'In Review':      return 'text-[var(--color-info)]';
      case 'Awaiting Merge': return 'text-[var(--color-warning)]';
      case 'Done':           return 'text-[var(--color-success)]';
      case 'Failed':         return 'text-[var(--color-danger)]';
      case 'Planning':       return 'text-[var(--color-accent-dim)]';
      default:               return 'text-[var(--color-muted)]';
    }
  }
</script>

{#if project}
  <ProjectTabs projectId={params.pid} projectName={project.name} />
{/if}

<div class="flex relative {project ? 'h-[calc(100vh-3rem)]' : 'h-screen'}">
  <!-- Board columns -->
  <div class="flex-1 overflow-x-auto" class:invisible={projectState.panelExpanded}>
    <div class="flex min-w-max h-full">
      {#each columns as col}
        {@const tickets = ticketsForColumn(col.statuses)}
        {@const hasTickets = tickets.length > 0}
        <div class="w-[252px] border-r border-[var(--color-border)] flex flex-col">
          <!-- Column header -->
          <div class="px-3 py-3 border-b border-[var(--color-border)] border-t-2 {col.accent}
                      flex items-center justify-between shrink-0">
            <div class="flex items-center gap-2">
              <span class="text-[10px] font-bold tracking-[0.15em] uppercase
                           {hasTickets ? columnDotColor(col.label) : 'text-[var(--color-muted)]'}">
                {col.label}
              </span>
            </div>
            {#if hasTickets}
              <span class="text-[10px] font-bold text-[var(--color-bg)] bg-[var(--color-muted-bright)]
                           px-1.5 py-0.5 min-w-[1.4rem] text-center leading-none tabular-nums">
                {tickets.length}
              </span>
            {/if}
          </div>

          <!-- Cards -->
          <div class="flex-1 overflow-y-auto p-2 space-y-2 board-column">
            {#each tickets as ticket, i (ticket.ID)}
              <div style="animation-delay: {i * 25}ms"
                   class="animate-[fade-in_0.18s_ease-out] opacity-0 [animation-fill-mode:forwards]">
                <TicketCard {ticket} onclick={() => projectState.loadTicketDetail(ticket.ID)} />
              </div>
            {/each}

            {#if !hasTickets}
              <div class="text-center py-8 px-3">
                <div class="text-[10px] text-[var(--color-muted)] tracking-wider">—</div>
              </div>
            {/if}
          </div>
        </div>
      {/each}
    </div>
  </div>

  <!-- Side panel -->
  {#if projectState.selectedTicketId && !projectState.panelExpanded}
    <div class="w-[38%] min-w-96 border-l border-[var(--color-border)] overflow-y-auto shrink-0">
      <TicketPanel />
    </div>
  {/if}

  <!-- Full-page overlay -->
  {#if projectState.panelExpanded && projectState.selectedTicketId}
    <TicketFullView />
  {/if}
</div>
