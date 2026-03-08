<script lang="ts">
  import type { Task } from '../types';
  import { taskIcon } from '../format';

  let { tasks = [] }: { tasks: Task[] } = $props();

  // Only show when there are dependencies
  let hasDeps = $derived(tasks.some(t => t.DependsOn?.length > 0));

  // Build ranks (depth) by topological sort
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

    function getRank(id: string): number {
      if (ranks.has(id)) return ranks.get(id)!;
      const task = taskMap.get(id);
      if (!task || !task.DependsOn?.length) {
        ranks.set(id, 0);
        return 0;
      }
      const maxDep = Math.max(...task.DependsOn.map(d => getRank(d)));
      const rank = maxDep + 1;
      ranks.set(id, rank);
      return rank;
    }

    tasks.forEach(t => getRank(t.ID));

    // Group by rank
    const byRank = new Map<number, Task[]>();
    tasks.forEach(t => {
      const r = ranks.get(t.ID) || 0;
      if (!byRank.has(r)) byRank.set(r, []);
      byRank.get(r)!.push(t);
    });

    const nodeWidth = 160;
    const nodeHeight = 40;
    const gapX = 60;
    const gapY = 20;

    const result: DagNode[] = [];
    for (const [rank, rankTasks] of byRank) {
      rankTasks.forEach((task, i) => {
        result.push({
          task,
          rank,
          x: rank * (nodeWidth + gapX) + 20,
          y: i * (nodeHeight + gapY) + 20,
        });
      });
    }
    return result;
  });

  let nodeMap = $derived(new Map(nodes.map(n => [n.task.ID, n])));
  let maxRank = $derived(Math.max(0, ...nodes.map(n => n.rank)));

  let svgWidth = $derived((maxRank + 1) * 220 + 40);
  let svgHeight = $derived(Math.max(100, ...nodes.map(n => n.y + 60)));

  function statusColor(status: string): string {
    if (status === 'done') return '#00CC66';
    if (status === 'failed') return '#FF4444';
    if (['implementing', 'tdd_verifying', 'testing', 'spec_review', 'quality_review'].includes(status)) return '#FFE600';
    return '#2a2a2a';
  }

  // Build edges
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
  <div class="overflow-x-auto border border-border bg-bg p-2">
    <svg width={svgWidth} height={svgHeight} class="block">
      <defs>
        <marker id="arrow" viewBox="0 0 10 6" refX="10" refY="3" markerWidth="8" markerHeight="6" orient="auto-start-reverse">
          <path d="M 0 0 L 10 3 L 0 6 z" fill="#888" />
        </marker>
      </defs>

      <!-- Edges -->
      {#each edges as edge}
        <line
          x1={edge.from.x + 160} y1={edge.from.y + 20}
          x2={edge.to.x} y2={edge.to.y + 20}
          stroke="#444" stroke-width="1.5" marker-end="url(#arrow)"
        />
      {/each}

      <!-- Nodes -->
      {#each nodes as node}
        <g transform="translate({node.x},{node.y})" class="cursor-pointer">
          <rect
            width="160" height="40" rx="4"
            fill="#111" stroke={statusColor(node.task.Status)} stroke-width="2"
          />
          <text x="8" y="16" fill="#F0F0F0" font-size="10" font-family="monospace">
            {taskIcon(node.task.Status)} {node.task.Sequence}. {node.task.Title.slice(0, 18)}
          </text>
          <text x="8" y="30" fill="#888" font-size="9" font-family="monospace">
            {node.task.Status}
          </text>
        </g>
      {/each}
    </svg>
  </div>
{/if}
