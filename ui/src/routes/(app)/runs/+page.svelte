<script lang="ts">
	import { onMount } from 'svelte';
	import { stacks, runs, type Stack, type Run } from '$lib/api/client';

	let loading = $state(true);
	let error = $state<string | null>(null);
	// Collect runs across all stacks
	let allRuns = $state<(Run & { stackName?: string })[]>([]);

	onMount(async () => {
		try {
			const stackList = await stacks.list();
			const stackMap = new Map<string, string>(stackList.map((s) => [s.id, s.name]));

			const runLists = await Promise.all(stackList.map((s) => runs.list(s.id)));
			allRuns = runLists
				.flat()
				.map((r) => ({ ...r, stackName: stackMap.get(r.stack_id) }))
				.sort((a, b) => new Date(b.queued_at).getTime() - new Date(a.queued_at).getTime());
		} catch (e) {
			error = (e as Error).message;
		} finally {
			loading = false;
		}
	});

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
									{run.stackName ?? run.stack_id.slice(0, 8)}
								</a>
							</td>
							<td class="px-4 py-3 text-zinc-400">{run.type}</td>
							<td class="px-4 py-3 text-zinc-500">{run.trigger}</td>
							<td class="px-4 py-3 text-zinc-500 text-xs">{fmtDate(run.queued_at)}</td>
						</tr>
					{/each}
				</tbody>
			</table>
		</div>
	{/if}
</div>
