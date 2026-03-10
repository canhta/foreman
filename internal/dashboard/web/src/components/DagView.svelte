<script lang="ts">
  import type { Task } from '../types';
  import { taskIcon } from '../format';

  let { tasks = [] }: { tasks: Task[] } = $props();

  let hasDeps = $derived(tasks.some(t => t.DependsOn?.length > 0));

  interface DagNode {
    task: Task;
    rank: number;
    x: number;
    y: number;
  }

  let nodes = $derived.by(() => {
    if (!hasDeps) return [];

    const taskMap = new Map(tasks.map(t => [t.ID, t]));
    const ranks = new Map<string, number>();

    function getRank(id: string, visiting = new Set<string>()): number {
      if (ranks.has(id)) return ranks.get(id)!;
      if (visiting.has(id)) return 0; // cycle — break it
      const task = taskMap.get(id);
      if (!task || !task.DependsOn?.length) {
        ranks.set(id, 0);
        return 0;
      }
      visiting.add(id);
      const maxDep = Math.max(...task.DependsOn.map(d => getRank(d, visiting)));
      visiting.delete(id);
      const rank = maxDep + 1;
      ranks.set(id, rank);
      return rank;
    }

    tasks.forEach(t => getRank(t.ID));

    const byRank = new Map<number, Task[]>();
    tasks.forEach(t => {
      const r = ranks.get(t.ID) || 0;
      if (!byRank.has(r)) byRank.set(r, []);
      byRank.get(r)!.push(t);
    });

    const nodeWidth = 172;
    const nodeHeight = 44;
    const gapX = 60;
    const gapY = 20;

    const result: DagNode[] = [];
    for (const [rank, rankTasks] of byRank) {
      rankTasks.forEach((task, i) => {
        result.push({
          task,
          rank,
          x: rank * (nodeWidth + gapX) + 16,
          y: i * (nodeHeight + gapY) + 16,
        });
      });
    }
    return result;
  });

  let nodeMap = $derived(new Map(nodes.map(n => [n.task.ID, n])));
  let maxRank = $derived(Math.max(0, ...nodes.map(n => n.rank)));

  let svgWidth = $derived((maxRank + 1) * (172 + 60) + 32);
  let svgHeight = $derived(Math.max(90, ...nodes.map(n => n.y + 56)));

  function statusStroke(status: string): string {
    if (status === 'done') return '#00E868';
    if (status === 'failed') return '#FF4040';
    if (['implementing', 'tdd_verifying', 'testing', 'spec_review', 'quality_review'].includes(status)) return '#FFE600';
    return '#3c3c3c';
  }

  function statusTextColor(status: string): string {
    if (status === 'done') return '#00E868';
    if (status === 'failed') return '#FF4040';
    if (['implementing', 'tdd_verifying', 'testing', 'spec_review', 'quality_review'].includes(status)) return '#FFE600';
    return '#9a9a9a';
  }

  let edges = $derived.by(() => {
    const result: { from: DagNode; to: DagNode }[] = [];
    for (const node of nodes) {
      for (const depId of node.task.DependsOn || []) {
        const from = nodeMap.get(depId);
        if (from) result.push({ from, to: node });
      }
    }
    return result;
  });
</script>

{#if hasDeps}
  <div class="overflow-x-auto border border-[var(--color-border)] bg-[var(--color-bg)]">
    <div class="px-3 py-2 border-b border-[var(--color-border)] text-[10px] tracking-[0.15em] text-[var(--color-muted)] uppercase">Task Dependencies</div>
    <svg width={svgWidth} height={svgHeight} class="block">
      <defs>
        <marker id="arrow" viewBox="0 0 8 6" refX="8" refY="3" markerWidth="6" markerHeight="6" orient="auto-start-reverse">
          <path d="M 0 0 L 8 3 L 0 6 z" fill="#666666" />
        </marker>
      </defs>

      <!-- Edges -->
      {#each edges as edge}
        <line
          x1={edge.from.x + 172} y1={edge.from.y + 22}
          x2={edge.to.x} y2={edge.to.y + 22}
          stroke="#444444" stroke-width="1.5" marker-end="url(#arrow)"
        />
      {/each}

      <!-- Nodes -->
      {#each nodes as node}
        {@const stroke = statusStroke(node.task.Status)}
        {@const textColor = statusTextColor(node.task.Status)}
        <g transform="translate({node.x},{node.y})">
          <!-- Node box — square corners (brutalist) -->
          <rect
            width="172" height="44"
            fill="#111111"
            stroke={stroke}
            stroke-width="1.5"
          />
          <!-- Left accent bar -->
          <rect width="3" height="44" fill={stroke} />
          <!-- Task label -->
          <text x="12" y="17" fill="#F0F0F0" font-size="11" font-family="monospace" font-weight="600">
            {node.task.Sequence}. {node.task.Title.slice(0, 16)}{node.task.Title.length > 16 ? '…' : ''}
          </text>
          <!-- Status -->
          <text x="12" y="33" fill={textColor} font-size="10" font-family="monospace">
            {taskIcon(node.task.Status)} {node.task.Status.toUpperCase().replace('_', ' ')}
          </text>
        </g>
      {/each}
    </svg>
  </div>
{/if}
