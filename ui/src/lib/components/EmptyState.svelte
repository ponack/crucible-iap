<script lang="ts">
	import type { Snippet } from 'svelte';

	interface Props {
		icon: string;
		heading: string;
		sub?: string;
		children?: Snippet;
	}

	const { icon, heading, sub, children }: Props = $props();
</script>

<div class="rounded-xl border p-12 flex flex-col items-center text-center" style="border-color: var(--color-zinc-800);">
	<div class="mb-4 rounded-xl p-3" style="background: var(--accent-muted); border: 1px solid var(--accent-border);">
		<svg class="h-5 w-5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" style="color: var(--accent);">
			<path d={icon}/>
		</svg>
	</div>
	<p class="text-sm font-medium text-zinc-200">{heading}</p>
	{#if sub}
		<p class="mt-1 text-xs text-zinc-500 max-w-xs leading-relaxed">{sub}</p>
	{/if}
	{#if children}
		<div class="mt-4">
			{@render children()}
		</div>
	{/if}
</div>
