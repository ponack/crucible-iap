<script lang="ts">
	import { onMount } from 'svelte';
	import { analyticsApi, type RunAnalytics } from '$lib/api/client';

	let data = $state<RunAnalytics | null>(null);
	let loading = $state(true);
	let error = $state<string | null>(null);
	let days = $state(30);

	async function load() {
		loading = true;
		error = null;
		try {
			data = await analyticsApi.getRuns(days);
		} catch (e) {
			error = (e as Error).message;
		} finally {
			loading = false;
		}
	}

	onMount(load);

	function pct(n: number, total: number) {
		return total === 0 ? 0 : Math.round((n / total) * 100);
	}

	const maxDailyTotal = $derived(
		data ? Math.max(...data.daily.map((d) => d.total), 1) : 1
	);
</script>

<div class="p-6 space-y-6 max-w-5xl">
	<div class="flex items-center justify-between">
		<div>
			<h1 class="text-lg font-semibold text-white">Analytics</h1>
			<p class="text-sm text-zinc-500 mt-0.5">Run activity and plan change stats across all stacks.</p>
		</div>
		<div class="flex items-center gap-2">
			<label class="text-xs text-zinc-500" for="window">Window</label>
			<select id="window" class="field-input max-w-[120px]" bind:value={days} onchange={load}>
				<option value={7}>7 days</option>
				<option value={30}>30 days</option>
				<option value={60}>60 days</option>
				<option value={90}>90 days</option>
			</select>
		</div>
	</div>

	{#if loading}
		<p class="text-zinc-500 text-sm">Loading…</p>
	{:else if error}
		<div class="rounded-lg border border-red-800 bg-red-950 px-4 py-3 text-sm text-red-300">{error}</div>
	{:else if data}
		<!-- Overview cards -->
		<div class="grid grid-cols-2 gap-4 sm:grid-cols-4">
			<div class="rounded-xl border border-zinc-800 bg-zinc-900/50 p-4">
				<p class="text-xs text-zinc-500 uppercase tracking-wide">Total runs</p>
				<p class="text-2xl font-semibold text-white mt-1">{data.overview.total_runs}</p>
			</div>
			<div class="rounded-xl border border-zinc-800 bg-zinc-900/50 p-4">
				<p class="text-xs text-zinc-500 uppercase tracking-wide">Success rate</p>
				<p class="text-2xl font-semibold mt-1 {data.overview.success_rate >= 90 ? 'text-emerald-400' : data.overview.success_rate >= 70 ? 'text-yellow-400' : 'text-red-400'}">
					{data.overview.success_rate.toFixed(1)}%
				</p>
			</div>
			<div class="rounded-xl border border-zinc-800 bg-zinc-900/50 p-4">
				<p class="text-xs text-zinc-500 uppercase tracking-wide">Failed</p>
				<p class="text-2xl font-semibold text-red-400 mt-1">{data.overview.failed}</p>
			</div>
			<div class="rounded-xl border border-zinc-800 bg-zinc-900/50 p-4">
				<p class="text-xs text-zinc-500 uppercase tracking-wide">Plan changes</p>
				<p class="text-2xl font-semibold text-white mt-1">
					+{data.overview.total_add} ~{data.overview.total_change} -{data.overview.total_destroy}
				</p>
			</div>
		</div>

		<!-- Daily chart -->
		{#if data.daily.length > 0}
			<div class="rounded-xl border border-zinc-800 p-5 space-y-3">
				<h2 class="text-sm font-medium text-zinc-300">Runs per day</h2>
				<div class="flex items-end gap-1 h-24">
					{#each data.daily as bucket (bucket.date)}
						{@const totalH = pct(bucket.total, maxDailyTotal)}
						{@const finH = pct(bucket.finished, maxDailyTotal)}
						{@const failH = pct(bucket.failed, maxDailyTotal)}
						<div class="flex-1 flex flex-col justify-end gap-px group relative" title="{bucket.date}: {bucket.total} total, {bucket.finished} ok, {bucket.failed} failed">
							{#if failH > 0}
								<div class="bg-red-600 rounded-sm" style="height:{failH}%"></div>
							{/if}
							{#if finH > 0}
								<div class="bg-teal-600 rounded-sm" style="height:{finH}%"></div>
							{/if}
							{#if totalH - finH - failH > 0}
								<div class="bg-zinc-600 rounded-sm" style="height:{totalH - finH - failH}%"></div>
							{/if}
						</div>
					{/each}
				</div>
				<div class="flex items-center gap-4 text-xs text-zinc-500">
					<span class="flex items-center gap-1.5"><span class="w-2 h-2 rounded-sm bg-teal-600 inline-block"></span>Finished</span>
					<span class="flex items-center gap-1.5"><span class="w-2 h-2 rounded-sm bg-red-600 inline-block"></span>Failed</span>
					<span class="flex items-center gap-1.5"><span class="w-2 h-2 rounded-sm bg-zinc-600 inline-block"></span>Other</span>
				</div>
			</div>
		{/if}

		<!-- Per-stack table -->
		{#if data.by_stack.length > 0}
			<div class="rounded-xl border border-zinc-800 overflow-hidden">
				<div class="px-4 py-3 bg-zinc-900 border-b border-zinc-800">
					<h2 class="text-sm font-medium text-zinc-300">By stack</h2>
				</div>
				<table class="w-full text-sm">
					<thead class="bg-zinc-900/50 text-zinc-500 text-xs uppercase tracking-wide">
						<tr>
							<th class="text-left px-4 py-2">Stack</th>
							<th class="text-right px-4 py-2">Runs</th>
							<th class="text-right px-4 py-2">Success</th>
							<th class="text-right px-4 py-2">Failed</th>
							<th class="text-right px-4 py-2 text-emerald-500">+Add</th>
							<th class="text-right px-4 py-2 text-yellow-500">~Change</th>
							<th class="text-right px-4 py-2 text-red-500">-Destroy</th>
						</tr>
					</thead>
					<tbody class="divide-y divide-zinc-800">
						{#each data.by_stack as s (s.stack_id)}
							<tr class="hover:bg-zinc-900/30 transition-colors">
								<td class="px-4 py-2.5 text-zinc-200 font-medium">
									<a href="/stacks/{s.stack_id}" class="hover:text-teal-400 transition-colors">{s.stack_name}</a>
								</td>
								<td class="px-4 py-2.5 text-right text-zinc-300">{s.total}</td>
								<td class="px-4 py-2.5 text-right text-emerald-400">{pct(s.finished, s.total)}%</td>
								<td class="px-4 py-2.5 text-right {s.failed > 0 ? 'text-red-400' : 'text-zinc-500'}">{s.failed}</td>
								<td class="px-4 py-2.5 text-right text-zinc-300">{s.plan_add}</td>
								<td class="px-4 py-2.5 text-right text-zinc-300">{s.plan_change}</td>
								<td class="px-4 py-2.5 text-right {s.plan_destroy > 0 ? 'text-red-400' : 'text-zinc-300'}">{s.plan_destroy}</td>
							</tr>
						{/each}
					</tbody>
				</table>
			</div>
		{:else}
			<p class="text-zinc-500 text-sm">No completed runs in this window.</p>
		{/if}
	{/if}
</div>

<style>
	:global(.field-input) {
		display: block;
		width: 100%;
		padding: 0.375rem 0.625rem;
		background: var(--color-zinc-900);
		border: 1px solid var(--color-zinc-700);
		border-radius: 0.5rem;
		color: #fff;
		font-size: 0.875rem;
		outline: none;
		transition: border-color 0.1s;
	}
	:global(.field-input:focus) {
		border-color: var(--color-teal-500, #6366f1);
	}
</style>
