<script lang="ts">
	// Shimmer-animated placeholder block. Use to communicate "still loading"
	// with a content-shaped silhouette instead of a bare "Loading…" string.
	//
	// Variants:
	//   - 'line'      single-line text placeholder; pass `lines` for multiple
	//   - 'card'      block sized like a content card; `rows` controls count
	//   - 'table-row' a tabular row strip with N columns
	//
	// All variants honour the parent's width. Heights are content-appropriate
	// defaults; override via `class` if needed.
	interface Props {
		variant?: 'line' | 'card' | 'table-row';
		lines?: number;
		rows?: number;
		columns?: number;
		class?: string;
	}

	let {
		variant = 'line',
		lines = 1,
		rows = 3,
		columns = 4,
		class: klass = ''
	}: Props = $props();

	const lineWidths = ['100%', '92%', '85%', '78%', '95%'];
</script>

<div class="skel-root {klass}" aria-busy="true" aria-live="polite">
	{#if variant === 'line'}
		{#each Array(lines) as _, i}
			<div class="skel" style="width: {lineWidths[i % lineWidths.length]}; height: 0.875rem;"></div>
		{/each}
	{:else if variant === 'card'}
		{#each Array(rows) as _, i (i)}
			<div class="skel-card">
				<div class="skel" style="width: 60%; height: 1rem;"></div>
				<div class="skel" style="width: 40%; height: 0.75rem; margin-top: 0.5rem;"></div>
			</div>
		{/each}
	{:else if variant === 'table-row'}
		{#each Array(rows) as _, r (r)}
			<div class="skel-row">
				{#each Array(columns) as _, c (c)}
					<div class="skel" style="height: 0.75rem; flex: {c === 0 ? 2 : 1};"></div>
				{/each}
			</div>
		{/each}
	{/if}
</div>

<style>
	.skel-root { width: 100%; }
	.skel-root > * + * { margin-top: 0.5rem; }
	.skel {
		display: block;
		border-radius: 0.25rem;
		background: linear-gradient(
			90deg,
			var(--color-zinc-800) 0%,
			var(--color-zinc-700) 50%,
			var(--color-zinc-800) 100%
		);
		background-size: 200% 100%;
		animation: skel-shimmer 1.4s ease-in-out infinite;
	}
	.skel-card {
		padding: 1rem;
		border: 1px solid var(--color-zinc-800);
		border-radius: 0.75rem;
		background: rgba(24, 24, 27, 0.5);
	}
	.skel-row {
		display: flex;
		gap: 1rem;
		padding: 0.625rem 0.75rem;
		align-items: center;
		border-bottom: 1px solid var(--color-zinc-800);
	}
	.skel-row:last-child { border-bottom: none; }
	@keyframes skel-shimmer {
		0%   { background-position: 200% 0; }
		100% { background-position: -200% 0; }
	}
	@media (prefers-reduced-motion: reduce) {
		.skel { animation: none; }
	}
</style>
