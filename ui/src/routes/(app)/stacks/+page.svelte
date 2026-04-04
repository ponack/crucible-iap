<script lang="ts">
	import { stacks, type Stack, type PageMeta } from '$lib/api/client';
	import { onMount } from 'svelte';

	let items = $state<Stack[]>([]);
	let pagination = $state<PageMeta | null>(null);
	let offset = $state(0);
	let loading = $state(true);
	let error = $state<string | null>(null);

	async function load() {
		loading = true;
		error = null;
		try {
			const res = await stacks.list(offset);
			items = res.data;
			pagination = res.pagination;
		} catch (e) {
			error = (e as Error).message;
		} finally {
			loading = false;
		}
	}

	onMount(load);

	function prev() { offset = Math.max(0, offset - (pagination?.limit ?? 50)); load(); }
	function next() { offset += pagination?.limit ?? 50; load(); }

	const toolBadge: Record<string, string> = {
		opentofu: 'bg-violet-900 text-violet-300',
		terraform: 'bg-purple-900 text-purple-300',
		ansible: 'bg-red-900 text-red-300',
		pulumi: 'bg-sky-900 text-sky-300'
	};

	const runStatusColour: Record<string, string> = {
		finished: 'text-green-400',
		failed: 'text-red-400',
		unconfirmed: 'text-yellow-400',
		applying: 'text-blue-400',
		planning: 'text-blue-400',
		preparing: 'text-blue-400',
		queued: 'text-zinc-400',
		canceled: 'text-zinc-500',
		discarded: 'text-zinc-500'
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
						<th class="text-left px-4 py-3">Last run</th>
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
							<td class="px-4 py-3 text-xs">
								{#if stack.last_run_status}
									<span class="font-medium {runStatusColour[stack.last_run_status] ?? 'text-zinc-400'}">
										{stack.last_run_status}
									</span>
								{:else}
									<span class="text-zinc-600">—</span>
								{/if}
							</td>
							<td class="px-4 py-3 text-zinc-400">
								{stack.auto_apply ? '✓' : '—'}
							</td>
						</tr>
					{/each}
				</tbody>
			</table>
		</div>

		{#if pagination && pagination.total > pagination.limit}
			<div class="flex items-center justify-between text-xs text-zinc-500">
				<span>{offset + 1}–{Math.min(offset + items.length, pagination.total)} of {pagination.total}</span>
				<div class="flex gap-2">
					<button onclick={prev} disabled={offset === 0} class="px-3 py-1.5 rounded-lg bg-zinc-800 hover:bg-zinc-700 disabled:opacity-40 transition-colors">
						Previous
					</button>
					<button onclick={next} disabled={!pagination.has_more} class="px-3 py-1.5 rounded-lg bg-zinc-800 hover:bg-zinc-700 disabled:opacity-40 transition-colors">
						Next
					</button>
				</div>
			</div>
		{/if}
	{/if}
</div>
