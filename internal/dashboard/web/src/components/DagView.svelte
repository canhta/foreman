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

    const byRank = new Map<number, Task[]>();
    tasks.forEach(t => {
      const r = ranks.get(t.ID) || 0;
      if (!byRank.has(r)) byRank.set(r, []);
      byRank.get(r)!.push(t);
    });

    const nodeWidth = 160;
    const nodeHeight = 38;
    const gapX = 56;
    const gapY = 18;

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

  let svgWidth = $derived((maxRank + 1) * 216 + 32);
  let svgHeight = $derived(Math.max(90, ...nodes.map(n => n.y + 56)));

  function statusStroke(status: string): string {
    if (status === 'done') return '#00E060';
    if (status === 'failed') return '#FF2222';
    if (['implementing', 'tdd_verifying', 'testing', 'spec_review', 'quality_review'].includes(status)) return '#FFE600';
    return '#2e2e2e';
  }

  function statusTextColor(status: string): string {
    if (status === 'done') return '#00E060';
    if (status === 'failed') return '#FF2222';
    if (['implementing', 'tdd_verifying', 'testing', 'spec_review', 'quality_review'].includes(status)) return '#FFE600';
    return '#808080';
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
  <div class="overflow-x-auto border-2 border-border bg-bg">
    <svg width={svgWidth} height={svgHeight} class="block">
      <defs>
        <marker id="arrow" viewBox="0 0 8 6" refX="8" refY="3" markerWidth="6" markerHeight="6" orient="auto-start-reverse">
          <path d="M 0 0 L 8 3 L 0 6 z" fill="#444" />
        </marker>
      </defs>

      <!-- Edges -->
      {#each edges as edge}
        <line
          x1={edge.from.x + 160} y1={edge.from.y + 19}
          x2={edge.to.x} y2={edge.to.y + 19}
          stroke="#2e2e2e" stroke-width="1.5" marker-end="url(#arrow)"
        />
      {/each}

      <!-- Nodes -->
      {#each nodes as node}
        {@const stroke = statusStroke(node.task.Status)}
        {@const textColor = statusTextColor(node.task.Status)}
        <g transform="translate({node.x},{node.y})">
          <!-- Node box — square corners (brutalist) -->
          <rect
            width="160" height="38"
            fill="#0d0d0d"
            stroke={stroke}
            stroke-width="2"
          />
          <!-- Left accent bar -->
          <rect width="3" height="38" fill={stroke} />
          <!-- Task label -->
          <text x="12" y="15" fill="#EBEBEB" font-size="10" font-family="monospace" font-weight="600">
            {node.task.Sequence}. {node.task.Title.slice(0, 16)}{node.task.Title.length > 16 ? '…' : ''}
          </text>
          <!-- Status -->
          <text x="12" y="29" fill={textColor} font-size="9" font-family="monospace">
            {taskIcon(node.task.Status)} {node.task.Status.toUpperCase().replace('_', ' ')}
          </text>
        </g>
      {/each}
    </svg>
  </div>
{/if}
