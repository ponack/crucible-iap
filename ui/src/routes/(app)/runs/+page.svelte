<script lang="ts">
	import { onMount } from 'svelte';
	import { runs, type Run, type PageMeta } from '$lib/api/client';

	let loading = $state(true);
	let error = $state<string | null>(null);
	let allRuns = $state<Run[]>([]);
	let pagination = $state<PageMeta | null>(null);
	let offset = $state(0);

	async function load() {
		loading = true;
		error = null;
		try {
			const res = await runs.listAll(offset);
			allRuns = res.data;
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

	function fmtDate(iso: string) {
		return new Date(iso).toLocaleString();
	}

	const statusColour: Record<string, string> = {
		queued: 'text-zinc-400',
		preparing: 'text-blue-400',
		planning: 'text-blue-400',
		unconfirmed: 'text-yellow-400',
		confirmed: 'text-blue-400',
		applying: 'text-blue-400',
		finished: 'text-green-400',
		failed: 'text-red-400',
		canceled: 'text-zinc-500',
		discarded: 'text-zinc-500'
	};
</script>

<div class="p-6 space-y-4">
	<h1 class="text-lg font-semibold text-white">Runs</h1>

	{#if loading}
		<p class="text-zinc-500 text-sm">Loading…</p>
	{:else if error}
		<p class="text-red-400 text-sm">{error}</p>
	{:else if allRuns.length === 0}
		<div class="border border-zinc-800 rounded-xl p-12 text-center">
			<p class="text-zinc-400 text-sm">No runs yet. Trigger a run from a stack.</p>
		</div>
	{:else}
		<div class="border border-zinc-800 rounded-xl overflow-hidden">
			<table class="w-full text-sm">
				<thead class="bg-zinc-900 text-zinc-400 text-xs uppercase tracking-wide">
					<tr>
						<th class="text-left px-4 py-3">Status</th>
						<th class="text-left px-4 py-3">Stack</th>
						<th class="text-left px-4 py-3">Type</th>
						<th class="text-left px-4 py-3">Trigger</th>
						<th class="text-left px-4 py-3">Queued</th>
					</tr>
				</thead>
				<tbody class="divide-y divide-zinc-800">
					{#each allRuns as run (run.id)}
						<tr class="hover:bg-zinc-900/50 transition-colors">
							<td class="px-4 py-3">
								<a href="/runs/{run.id}" class="font-medium {statusColour[run.status] ?? 'text-zinc-400'}">
									{run.status}
								</a>
							</td>
							<td class="px-4 py-3">
								<a href="/stacks/{run.stack_id}" class="text-zinc-300 hover:text-white">
									{run.stack_name ?? run.stack_id.slice(0, 8)}
								</a>
							</td>
							<td class="px-4 py-3 text-zinc-400">
								{run.type}{#if run.is_drift} <span class="text-xs text-amber-500">drift</span>{/if}
								{#if run.pr_number}
									<a href={run.pr_url} target="_blank" rel="noopener"
										class="ml-1 text-xs text-blue-400 hover:text-blue-300">#{run.pr_number}</a>
								{/if}
							</td>
							<td class="px-4 py-3 text-zinc-500">{run.trigger}</td>
							<td class="px-4 py-3 text-zinc-500 text-xs">{fmtDate(run.queued_at)}</td>
						</tr>
					{/each}
				</tbody>
			</table>
		</div>

		{#if pagination && pagination.total > pagination.limit}
			<div class="flex items-center justify-between text-xs text-zinc-500">
				<span>{offset + 1}–{Math.min(offset + allRuns.length, pagination.total)} of {pagination.total}</span>
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
