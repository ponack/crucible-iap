<script lang="ts">
	import { stacks, type Stack } from '$lib/api/client';
	import { onMount } from 'svelte';

	let items = $state<Stack[]>([]);
	let loading = $state(true);
	let error = $state<string | null>(null);

	onMount(async () => {
		try {
			items = await stacks.list();
		} catch (e) {
			error = (e as Error).message;
		} finally {
			loading = false;
		}
	});

	const toolBadge: Record<string, string> = {
		opentofu: 'bg-violet-900 text-violet-300',
		terraform: 'bg-purple-900 text-purple-300',
		ansible: 'bg-red-900 text-red-300',
		pulumi: 'bg-sky-900 text-sky-300'
	};
</script>

<div class="p-6 space-y-4">
	<div class="flex items-center justify-between">
		<h1 class="text-lg font-semibold text-white">Stacks</h1>
		<a href="/stacks/new" class="bg-indigo-600 hover:bg-indigo-500 text-white text-sm px-3 py-1.5 rounded-lg transition-colors">
			New stack
		</a>
	</div>

	{#if loading}
		<p class="text-zinc-500 text-sm">Loading…</p>
	{:else if error}
		<p class="text-red-400 text-sm">{error}</p>
	{:else if items.length === 0}
		<div class="border border-zinc-800 rounded-xl p-12 text-center">
			<p class="text-zinc-400 text-sm">No stacks yet.</p>
			<a href="/stacks/new" class="mt-3 inline-block text-indigo-400 text-sm hover:underline">
				Create your first stack →
			</a>
		</div>
	{:else}
		<div class="border border-zinc-800 rounded-xl overflow-hidden">
			<table class="w-full text-sm">
				<thead class="bg-zinc-900 text-zinc-400 text-xs uppercase tracking-wide">
					<tr>
						<th class="text-left px-4 py-3">Name</th>
						<th class="text-left px-4 py-3">Tool</th>
						<th class="text-left px-4 py-3">Branch</th>
						<th class="text-left px-4 py-3">Auto-apply</th>
					</tr>
				</thead>
				<tbody class="divide-y divide-zinc-800">
					{#each items as stack (stack.id)}
						<tr class="hover:bg-zinc-900/50 transition-colors">
							<td class="px-4 py-3">
								<a href="/stacks/{stack.id}" class="text-white hover:text-indigo-400 font-medium">
									{stack.name}
								</a>
								{#if stack.description}
									<p class="text-zinc-500 text-xs mt-0.5">{stack.description}</p>
								{/if}
							</td>
							<td class="px-4 py-3">
								<span class="px-1.5 py-0.5 rounded text-xs font-medium {toolBadge[stack.tool] ?? 'bg-zinc-800 text-zinc-400'}">
									{stack.tool}
								</span>
							</td>
							<td class="px-4 py-3 text-zinc-400 font-mono text-xs">{stack.repo_branch}</td>
							<td class="px-4 py-3 text-zinc-400">
								{stack.auto_apply ? '✓' : '—'}
							</td>
						</tr>
					{/each}
				</tbody>
			</table>
		</div>
	{/if}
</div>
