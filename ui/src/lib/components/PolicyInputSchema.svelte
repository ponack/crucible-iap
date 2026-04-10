<script lang="ts">
	import { ChevronDown, ChevronRight } from 'lucide-svelte';
	import { inputSchemas, type PolicyType } from '$lib/policy-data';

	let { type }: { type: PolicyType } = $props();

	let expanded = $state(false);

	const schema = $derived(inputSchemas[type]);
</script>

<div class="rounded-lg border border-zinc-800">
	<button
		type="button"
		onclick={() => (expanded = !expanded)}
		class="flex w-full items-center justify-between px-3 py-2 text-xs text-zinc-400 transition-colors hover:text-zinc-200"
	>
		<span class="font-medium">Input reference</span>
		{#if expanded}
			<ChevronDown size={14} />
		{:else}
			<ChevronRight size={14} />
		{/if}
	</button>

	{#if expanded}
		<div class="space-y-4 border-t border-zinc-800 px-3 py-3">
			<p class="text-xs text-zinc-400">{schema.summary}</p>

			<!-- Sample snippet -->
			<div>
				<p class="mb-1 text-xs font-medium text-zinc-500 uppercase tracking-wide">Shape</p>
				<pre
					class="overflow-x-auto rounded-md bg-zinc-950 px-3 py-2.5 font-mono text-[11px] text-zinc-300 leading-relaxed">{schema.sample}</pre>
			</div>

			<!-- Field reference table -->
			<div>
				<p class="mb-2 text-xs font-medium text-zinc-500 uppercase tracking-wide">Key fields</p>
				<div class="space-y-1.5">
					{#each schema.fields as field}
						<div class="grid grid-cols-[1fr_auto] gap-x-3 gap-y-0.5">
							<code class="font-mono text-[11px] text-indigo-300 break-all">{field.path}</code>
							<span
								class="shrink-0 rounded bg-zinc-800 px-1.5 py-0.5 font-mono text-[10px] text-zinc-400"
								>{field.type}</span
							>
							<p class="col-span-2 text-[11px] text-zinc-500">{field.description}</p>
						</div>
					{/each}
				</div>
			</div>
		</div>
	{/if}
</div>
