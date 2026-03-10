<script lang="ts">
  import { projectState } from '../state/project.svelte';
  import { globalState } from '../state/global.svelte';
  import ProjectTabs from '../components/ProjectTabs.svelte';
  import TicketCard from '../components/TicketCard.svelte';
  import TicketPanel from '../components/TicketPanel.svelte';
  import type { TicketSummary } from '../types';

  let { params }: { params: { pid: string } } = $props();

  const project = $derived(globalState.projects.find((p: { id: string; name: string }) => p.id === params.pid));

  $effect(() => {
    if (params.pid) {
      projectState.switchProject(params.pid);
    }
  });

  const columns = [
    { label: 'Queued', statuses: ['queued', 'clarification_needed', 'decomposed'] },
    { label: 'Planning', statuses: ['planning', 'plan_validating', 'decomposing'] },
    { label: 'In Progress', statuses: ['implementing'] },
    { label: 'In Review', statuses: ['reviewing'] },
    { label: 'Awaiting Merge', statuses: ['pr_created', 'pr_updated', 'awaiting_merge'] },
    { label: 'Done', statuses: ['done', 'merged'] },
    { label: 'Failed', statuses: ['failed', 'blocked', 'partial'] },
  ] as const;

  function ticketsForColumn(statuses: readonly string[]): TicketSummary[] {
    return projectState.tickets.filter(t => statuses.includes(t.Status));
  }
</script>

{#if project}
  <ProjectTabs projectId={params.pid} projectName={project.name} />
{/if}

<div class="flex h-[calc(100vh-theme(spacing.12))]">
  <!-- Board columns -->
  <div class="flex-1 overflow-x-auto">
    <div class="flex gap-0 min-w-max h-full">
      {#each columns as col}
        {@const tickets = ticketsForColumn(col.statuses)}
        <div class="w-56 border-r border-[var(--color-border)] flex flex-col">
          <div class="px-3 py-2 border-b border-[var(--color-border)] flex items-center gap-2">
            <span class="text-[10px] tracking-widest text-[var(--color-muted)] uppercase">{col.label}</span>
            {#if tickets.length > 0}
              <span class="text-[10px] text-[var(--color-bg)] bg-[var(--color-muted)] px-1.5 min-w-[1.25rem] text-center">
                {tickets.length}
              </span>
            {/if}
          </div>
          <div class="flex-1 overflow-y-auto p-2 space-y-2 board-column">
            {#each tickets as ticket, i (ticket.ID)}
              <div style="animation-delay: {i * 30}ms" class="animate-fade-in opacity-0 [animation-fill-mode:forwards]">
                <TicketCard {ticket} onclick={() => projectState.loadTicketDetail(ticket.ID)} />
              </div>
            {/each}
          </div>
        </div>
      {/each}
    </div>
  </div>

  <!-- Side panel -->
  {#if projectState.selectedTicketId && !projectState.panelExpanded}
    <div class="w-[40%] min-w-96 border-l border-[var(--color-border)] overflow-y-auto
                animate-[slide-in-right_0.2s_ease-out]">
      <TicketPanel />
    </div>
  {/if}
</div>

{#if projectState.panelExpanded && projectState.selectedTicketId}
  <!-- Full page ticket view overlays the board -->
  <!-- Implemented in Phase 3 with chat interface -->
{/if}
