<script lang="ts">
	import { onMount } from 'svelte';
	import { deps, type DagPayload } from '$lib/api/client';
	import { layoutDag, type LaidOutEdge, type LaidOutNode } from '$lib/dag-layout';
	import EmptyState from '$lib/components/EmptyState.svelte';
	import Skeleton from '$lib/components/Skeleton.svelte';

	let payload = $state<DagPayload | null>(null);
	let loading = $state(true);
	let error = $state<string | null>(null);

	// Hover state — used to highlight edges + adjacent nodes when the user
	// hovers over a node or edge so the relationships in a busy graph stay
	// readable.
	let hoverNodeID = $state<string | null>(null);
	let hoverEdgeKey = $state<string | null>(null);

	const layout = $derived(payload ? layoutDag(payload.nodes, payload.edges) : null);

	const edgeKey = (e: LaidOutEdge) => `${e.upstream_id}->${e.downstream_id}`;

	function isEdgeHighlighted(e: LaidOutEdge): boolean {
		if (hoverEdgeKey === edgeKey(e)) return true;
		if (hoverNodeID && (e.upstream_id === hoverNodeID || e.downstream_id === hoverNodeID)) return true;
		return false;
	}

	function isNodeHighlighted(n: LaidOutNode): boolean {
		if (hoverNodeID === n.id) return true;
		if (!layout) return false;
		for (const e of layout.edges) {
			if (hoverEdgeKey === edgeKey(e) && (e.upstream_id === n.id || e.downstream_id === n.id)) {
				return true;
			}
			if (hoverNodeID && (e.upstream_id === hoverNodeID || e.downstream_id === hoverNodeID)) {
				if (e.upstream_id === n.id || e.downstream_id === n.id) return true;
			}
		}
		return false;
	}

	function edgeBadge(e: LaidOutEdge): string {
		const parts: string[] = [];
		if (e.trigger_when_field) {
			parts.push(`${e.trigger_when_field} ${e.trigger_when_op} ${e.trigger_when_value}`);
		}
		if (e.retry_count > 0) {
			parts.push(`retry ×${e.retry_count}`);
		}
		return parts.join(' · ');
	}

	onMount(async () => {
		try {
			payload = await deps.graph();
		} catch (e) {
			error = (e as Error).message;
		} finally {
			loading = false;
		}
	});

	function trunc(s: string, max = 22): string {
		return s.length > max ? s.slice(0, max - 1) + '…' : s;
	}
</script>

<div class="p-4 md:p-6 space-y-4">
	<div class="flex items-center justify-between flex-wrap gap-3">
		<div>
			<h1 class="text-lg font-semibold text-white">Stack dependencies</h1>
			<p class="mt-0.5 text-sm text-zinc-500">
				Org-wide DAG of upstream → downstream relationships. Click a node to open the stack; edge labels show conditional triggers and retry config.
			</p>
		</div>
	</div>

	{#if loading}
		<Skeleton variant="card" rows={6} />
	{:else if error}
		<div class="rounded-lg border border-red-800 bg-red-950 px-4 py-3 text-sm text-red-300">{error}</div>
	{:else if payload && payload.nodes.length === 0}
		<EmptyState
			icon="M6.429 9.75 2.25 12l4.179 2.25m0-4.5 5.571 3 5.571-3m-11.142 0L2.25 7.5 12 2.25l9.75 5.25-4.179 2.25m0 0L21.75 12l-4.179 2.25m0 0 4.179 2.25L12 21.75 2.25 16.5l4.179-2.25m11.142 0-5.571 3-5.571-3"
			heading="No stacks yet"
			sub="Create a stack to see it appear in the dependency graph."
		/>
	{:else if payload && payload.edges.length === 0}
		<EmptyState
			icon="M3.75 3v11.25A2.25 2.25 0 0 0 6 16.5h12M3.75 3h-1.5m1.5 0h16.5m0 0h1.5m-1.5 0v11.25A2.25 2.25 0 0 1 18 16.5h-2.25m-7.5 0h7.5m-7.5 0-1 3m8.5-3 1 3m0 0 .5 1.5m-.5-1.5h-9.5m0 0-.5 1.5m.75-9 3-3 2.148 2.148A12.061 12.061 0 0 1 16.5 7.605"
			heading="No dependencies configured"
			sub="Add upstream / downstream relationships from each stack's Config tab to populate this graph."
		/>
	{:else if layout}
		<div class="rounded-xl border border-zinc-800 bg-zinc-950 p-4 overflow-auto">
			<svg
				viewBox="0 0 {layout.width} {layout.height}"
				style="width: {layout.width}px; height: {layout.height}px; max-width: none;"
				role="img"
				aria-label="Stack dependency graph"
			>
				<defs>
					<marker id="dag-arr" markerWidth="8" markerHeight="8" refX="6" refY="4" orient="auto">
						<path d="M0,1 L7,4 L0,7 z" fill="#52525b" />
					</marker>
					<marker id="dag-arr-active" markerWidth="8" markerHeight="8" refX="6" refY="4" orient="auto">
						<path d="M0,1 L7,4 L0,7 z" style="fill: var(--accent);" />
					</marker>
				</defs>

				<!-- Edges first, so nodes draw on top -->
				{#each layout.edges as e (edgeKey(e))}
					{@const active = isEdgeHighlighted(e)}
					<!-- svelte-ignore a11y_mouse_events_have_key_events -->
					<path
						d={e.d}
						fill="none"
						stroke-width={active ? 2 : 1.4}
						stroke={active ? 'currentColor' : '#3f3f46'}
						style={active ? 'color: var(--accent);' : ''}
						marker-end={active ? 'url(#dag-arr-active)' : 'url(#dag-arr)'}
						onmouseenter={() => (hoverEdgeKey = edgeKey(e))}
						onmouseleave={() => (hoverEdgeKey = null)}
					/>
					{#if e.hasBadge}
						<g transform="translate({e.labelX}, {e.labelY})" pointer-events="none">
							<rect x={-edgeBadge(e).length * 3.2 - 8} y="-9" width={edgeBadge(e).length * 6.4 + 16} height="18" rx="9"
								fill="#18181b" stroke={active ? 'currentColor' : '#3f3f46'} style={active ? 'color: var(--accent);' : ''} />
							<text x="0" y="3.5" text-anchor="middle" font-family="ui-sans-serif,system-ui" font-size="10"
								style={active ? 'fill: var(--accent);' : 'fill: #a1a1aa;'}>
								{edgeBadge(e)}
							</text>
						</g>
					{/if}
				{/each}

				<!-- Nodes -->
				{#each layout.nodes as n (n.id)}
					{@const active = isNodeHighlighted(n)}
					<a
						href="/stacks/{n.id}"
						onmouseenter={() => (hoverNodeID = n.id)}
						onmouseleave={() => (hoverNodeID = null)}
					>
						<rect
							x={n.x}
							y={n.y}
							width="180"
							height="50"
							rx="8"
							fill={active ? '#27272a' : '#18181b'}
							stroke={active ? 'currentColor' : '#3f3f46'}
							stroke-width={active ? 2 : 1}
							style={active ? 'color: var(--accent);' : ''}
						/>
						<text x={n.x + 90} y={n.y + 22} text-anchor="middle" font-family="ui-sans-serif,system-ui" font-size="12" font-weight="500" fill="#e4e4e7">
							{trunc(n.name)}
						</text>
						<text x={n.x + 90} y={n.y + 38} text-anchor="middle" font-family="ui-monospace,monospace" font-size="10" fill="#71717a">
							{trunc(n.slug, 24)}
						</text>
					</a>
				{/each}
			</svg>
		</div>

		<div class="flex items-center gap-4 text-xs text-zinc-500">
			<span>{layout.nodes.length} stacks</span>
			<span>{layout.edges.length} edges</span>
			<span class="text-zinc-600">Hover a node or edge to highlight its relationships</span>
		</div>
	{/if}
</div>

<style>
	a {
		cursor: pointer;
	}
	a rect {
		transition: stroke 0.12s, fill 0.12s;
	}
</style>
