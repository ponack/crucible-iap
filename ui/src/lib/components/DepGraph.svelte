<script lang="ts">
	import type { StackDep } from '$lib/api/client';

	interface Props {
		current: { id: string; name: string; slug: string };
		upstream: StackDep[];
		downstream: StackDep[];
	}

	const { current, upstream, downstream }: Props = $props();

	// Layout constants (SVG user units)
	const NW = 160; // node width
	const NH = 42;  // node height
	const GY = 10;  // vertical gap between nodes in a column
	const CG = 72;  // column gap (space for arrows)
	const PX = 4;   // left/right padding
	const PY = 28;  // top padding (room for column labels)
	const PB = 10;  // bottom padding

	function colH(n: number): number {
		const count = Math.max(1, n);
		return count * NH + (count - 1) * GY;
	}

	const upH    = colH(upstream.length);
	const downH  = colH(downstream.length);
	const bodyH  = Math.max(upH, NH, downH);

	const W = PX + NW + CG + NW + CG + NW + PX;
	const H = PY + bodyH + PB;

	// X positions of each column's left edge
	const X_UP   = PX;
	const X_CUR  = PX + NW + CG;
	const X_DOWN = PX + NW + CG + NW + CG;

	function nodeY(colLen: number, i: number): number {
		const h = colH(colLen);
		const top = PY + (bodyH - h) / 2;
		return top + i * (NH + GY);
	}

	const curY = PY + (bodyH - NH) / 2;

	function upPath(i: number): string {
		const x1 = X_UP + NW;
		const y1 = nodeY(Math.max(1, upstream.length), i) + NH / 2;
		const x2 = X_CUR;
		const y2 = curY + NH / 2;
		const mx = x1 + CG / 2;
		return `M${x1},${y1} C${mx},${y1} ${mx},${y2} ${x2},${y2}`;
	}

	function downPath(i: number): string {
		const x1 = X_CUR + NW;
		const y1 = curY + NH / 2;
		const x2 = X_DOWN;
		const y2 = nodeY(Math.max(1, downstream.length), i) + NH / 2;
		const mx = x1 + CG / 2;
		return `M${x1},${y1} C${mx},${y1} ${mx},${y2} ${x2},${y2}`;
	}

	function trunc(s: string, max = 20): string {
		return s.length > max ? s.slice(0, max - 1) + '…' : s;
	}
</script>

<svg viewBox="0 0 {W} {H}" class="dep-graph w-full" style="height:{H}px; max-height:{H}px">
	<defs>
		<marker id="dg-arr" markerWidth="7" markerHeight="7" refX="5.5" refY="3.5" orient="auto">
			<path d="M0,1 L6,3.5 L0,6 z" fill="#52525b"/>
		</marker>
		<marker id="dg-arr-dim" markerWidth="7" markerHeight="7" refX="5.5" refY="3.5" orient="auto">
			<path d="M0,1 L6,3.5 L0,6 z" fill="#3f3f46"/>
		</marker>
	</defs>

	<!-- Column labels -->
	<text x={X_UP + NW/2}   y="16" text-anchor="middle" fill="#71717a" font-family="ui-sans-serif,system-ui" font-size="9" letter-spacing="0.08em">UPSTREAM</text>
	<text x={X_CUR + NW/2}  y="16" text-anchor="middle" fill="#71717a" font-family="ui-sans-serif,system-ui" font-size="9" letter-spacing="0.08em">THIS STACK</text>
	<text x={X_DOWN + NW/2} y="16" text-anchor="middle" fill="#71717a" font-family="ui-sans-serif,system-ui" font-size="9" letter-spacing="0.08em">DOWNSTREAM</text>

	<!-- Upstream arrows -->
	{#each upstream as _, i}
		<path d={upPath(i)} fill="none" stroke="#3f3f46" stroke-width="1.5" marker-end="url(#dg-arr)"/>
	{/each}

	<!-- Downstream arrows -->
	{#each downstream as _, i}
		<path d={downPath(i)} fill="none" stroke="#3f3f46" stroke-width="1.5" marker-end="url(#dg-arr)"/>
	{/each}

	<!-- Upstream nodes -->
	{#if upstream.length > 0}
		{#each upstream as dep, i}
			{@const y = nodeY(upstream.length, i)}
			<a href="/stacks/{dep.id}" class="dep-node">
				<rect x={X_UP} y={y} width={NW} height={NH} rx="7" fill="#18181b" stroke="#3f3f46" stroke-width="1"/>
				<text x={X_UP + NW/2} y={y + NH/2 - 4} text-anchor="middle" fill="#e4e4e7" font-family="ui-sans-serif,system-ui" font-size="11.5">{trunc(dep.name)}</text>
				<text x={X_UP + NW/2} y={y + NH/2 + 11} text-anchor="middle" fill="#71717a" font-family="ui-monospace,monospace" font-size="9.5">{trunc(dep.slug, 22)}</text>
			</a>
		{/each}
	{:else}
		<!-- Empty upstream placeholder -->
		<rect x={X_UP} y={curY} width={NW} height={NH} rx="7" fill="none" stroke="#27272a" stroke-width="1" stroke-dasharray="4 3"/>
		<text x={X_UP + NW/2} y={curY + NH/2 + 4} text-anchor="middle" fill="#3f3f46" font-family="ui-sans-serif,system-ui" font-size="11">no upstream</text>
	{/if}

	<!-- Current stack node -->
	<rect x={X_CUR} y={curY} width={NW} height={NH} rx="7" fill="#1e1b4b" stroke="#6366f1" stroke-width="1.5"/>
	<text x={X_CUR + NW/2} y={curY + NH/2 - 4} text-anchor="middle" fill="#e0e7ff" font-family="ui-sans-serif,system-ui" font-size="11.5" font-weight="600">{trunc(current.name)}</text>
	<text x={X_CUR + NW/2} y={curY + NH/2 + 11} text-anchor="middle" fill="#818cf8" font-family="ui-monospace,monospace" font-size="9.5">{trunc(current.slug, 22)}</text>

	<!-- Downstream nodes -->
	{#if downstream.length > 0}
		{#each downstream as dep, i}
			{@const y = nodeY(downstream.length, i)}
			<a href="/stacks/{dep.id}" class="dep-node">
				<rect x={X_DOWN} y={y} width={NW} height={NH} rx="7" fill="#18181b" stroke="#3f3f46" stroke-width="1"/>
				<text x={X_DOWN + NW/2} y={y + NH/2 - 4} text-anchor="middle" fill="#e4e4e7" font-family="ui-sans-serif,system-ui" font-size="11.5">{trunc(dep.name)}</text>
				<text x={X_DOWN + NW/2} y={y + NH/2 + 11} text-anchor="middle" fill="#71717a" font-family="ui-monospace,monospace" font-size="9.5">{trunc(dep.slug, 22)}</text>
			</a>
		{/each}
	{:else}
		<!-- Empty downstream placeholder -->
		<rect x={X_DOWN} y={curY} width={NW} height={NH} rx="7" fill="none" stroke="#27272a" stroke-width="1" stroke-dasharray="4 3"/>
		<text x={X_DOWN + NW/2} y={curY + NH/2 + 4} text-anchor="middle" fill="#3f3f46" font-family="ui-sans-serif,system-ui" font-size="11">no downstream</text>
	{/if}
</svg>

<style>
	.dep-graph { display: block; }
	.dep-node rect { transition: stroke 0.12s, fill 0.12s; }
	.dep-node:hover rect { stroke: #71717a; fill: #27272a; }
</style>
