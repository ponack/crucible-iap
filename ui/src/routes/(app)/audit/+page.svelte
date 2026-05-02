<script lang="ts">
	import { onMount } from 'svelte';
	import { audit, type AuditEvent, type PageMeta } from '$lib/api/client';
	import EmptyState from '$lib/components/EmptyState.svelte';

	let events = $state<AuditEvent[]>([]);
	let pagination = $state<PageMeta | null>(null);
	let offset = $state(0);
	let loading = $state(true);
	let error = $state<string | null>(null);
	let expandedID = $state<number | null>(null);

	let filterAction = $state('');
	let filterResourceType = $state('');

	async function load() {
		loading = true;
		error = null;
		try {
			const res = await audit.list(offset, 50, {
				action: filterAction || undefined,
				resource_type: filterResourceType || undefined
			});
			events = res.data;
			pagination = res.pagination;
			expandedID = null;
		} catch (e) {
			error = (e as Error).message;
		} finally {
			loading = false;
		}
	}

	onMount(load);

	function prev() { offset = Math.max(0, offset - (pagination?.limit ?? 50)); load(); }
	function next() { offset += pagination?.limit ?? 50; load(); }

	function applyFilters() { offset = 0; load(); }
	function clearFilters() { filterAction = ''; filterResourceType = ''; offset = 0; load(); }

	const hasFilters = $derived(filterAction !== '' || filterResourceType !== '');

	async function exportCSV() {
		const blob = await audit.exportCSV({
			action: filterAction || undefined,
			resource_type: filterResourceType || undefined
		});
		const url = URL.createObjectURL(blob);
		const a = document.createElement('a');
		a.href = url;
		a.download = 'audit-export.csv';
		a.click();
		URL.revokeObjectURL(url);
	}

	function fmtDate(iso: string) {
		return new Date(iso).toLocaleString();
	}

	function toggleExpand(id: number) {
		expandedID = expandedID === id ? null : id;
	}

	function hasContext(ctx: Record<string, unknown>): boolean {
		return ctx != null && Object.keys(ctx).length > 0;
	}

	const actionColour: Record<string, string> = {
		'run.created': 'text-teal-400',
		'run.confirmed': 'text-green-400',
		'run.discarded': 'text-zinc-400',
		'run.canceled': 'text-zinc-400',
		'run.preparing': 'text-teal-400',
		'run.planning': 'text-teal-400',
		'run.unconfirmed': 'text-yellow-400',
		'run.applying': 'text-teal-400',
		'run.finished': 'text-green-400',
		'run.failed': 'text-red-400',
		'stack.created': 'text-teal-400',
		'stack.deleted': 'text-red-400'
	};
</script>

<div class="p-6 space-y-4">
	<div class="flex items-center justify-between flex-wrap gap-2">
		<h1 class="text-lg font-semibold text-white">Audit log</h1>
		<div class="flex items-center gap-2">
			<input
				type="search" placeholder="Filter by action prefix…"
				bind:value={filterAction}
				onkeydown={(e) => e.key === 'Enter' && applyFilters()}
				class="bg-zinc-900 border border-zinc-700 rounded-lg px-3 py-1.5 text-sm text-white placeholder-zinc-600 focus:outline-none focus:border-teal-500 w-52"
			/>
			<select bind:value={filterResourceType} onchange={applyFilters}
				class="bg-zinc-900 border border-zinc-700 rounded-lg px-3 py-1.5 text-sm text-zinc-300 focus:outline-none focus:border-teal-500">
				<option value="">All resources</option>
				<option value="stack">Stack</option>
				<option value="run">Run</option>
				<option value="policy">Policy</option>
				<option value="org">Org</option>
			</select>
			<button onclick={applyFilters}
				class="bg-zinc-800 hover:bg-zinc-700 text-zinc-300 text-sm px-3 py-1.5 rounded-lg transition-colors">
				Filter
			</button>
			{#if hasFilters}
				<button onclick={clearFilters} class="text-sm text-zinc-500 hover:text-zinc-300 transition-colors">
					Clear
				</button>
			{/if}
			<button onclick={exportCSV}
				class="border border-zinc-700 hover:border-zinc-500 text-zinc-400 hover:text-zinc-200 text-sm px-3 py-1.5 rounded-lg transition-colors ml-2">
				Export CSV
			</button>
		</div>
	</div>

	{#if loading}
		<p class="text-zinc-500 text-sm">Loading…</p>
	{:else if error}
		<p class="text-red-400 text-sm">{error}</p>
	{:else if events.length === 0}
		<EmptyState
			icon="M9 12h3.75M9 15h3.75M9 18h3.75m3 .75H18a2.25 2.25 0 0 0 2.25-2.25V6.108c0-1.135-.845-2.098-1.976-2.192a48.424 48.424 0 0 0-1.123-.08m-5.801 0c-.065.21-.1.433-.1.664 0 .414.336.75.75.75h4.5a.75.75 0 0 0 .75-.75 2.25 2.25 0 0 0-.1-.664m-5.8 0A2.251 2.251 0 0 1 13.5 2.25H15c1.012 0 1.867.668 2.15 1.586m-5.8 0c-.376.023-.75.05-1.124.08C9.095 4.01 8.25 4.973 8.25 6.108V8.25m0 0H4.875c-.621 0-1.125.504-1.125 1.125v11.25c0 .621.504 1.125 1.125 1.125h9.75c.621 0 1.125-.504 1.125-1.125V9.375c0-.621-.504-1.125-1.125-1.125H8.25ZM6.75 12h.008v.008H6.75V12Zm0 3h.008v.008H6.75V15Zm0 3h.008v.008H6.75V18Z"
			heading="No audit events yet"
			sub="Actions taken by members — stack creates, run triggers, policy changes, setting updates — will appear here."
		/>
	{:else}
		<div class="border border-zinc-800 rounded-xl overflow-hidden">
			<table class="w-full text-sm">
				<thead class="bg-zinc-900 text-zinc-400 text-xs uppercase tracking-wide">
					<tr>
						<th class="text-left px-4 py-3">Time</th>
						<th class="text-left px-4 py-3">Action</th>
						<th class="text-left px-4 py-3">Resource</th>
						<th class="text-left px-4 py-3">Actor</th>
						<th class="px-4 py-3"></th>
					</tr>
				</thead>
				<tbody class="divide-y divide-zinc-700">
					{#each events as event (event.id)}
						<tr
							class="hover:bg-zinc-900/50 transition-colors {hasContext(event.context) ? 'cursor-pointer' : ''}"
							onclick={() => hasContext(event.context) && toggleExpand(event.id)}
						>
							<td class="px-4 py-3 text-zinc-500 text-xs whitespace-nowrap">{fmtDate(event.occurred_at)}</td>
							<td class="px-4 py-3">
								<span class="font-mono text-xs {actionColour[event.action] ?? 'text-zinc-300'}">
									{event.action}
								</span>
							</td>
							<td class="px-4 py-3 text-zinc-400 text-xs">
								{#if event.resource_type}
									<span class="text-zinc-500">{event.resource_type}/</span>
								{/if}
								{#if event.resource_id}
									<span class="font-mono">{event.resource_id.slice(0, 8)}</span>
								{:else}
									<span class="text-zinc-600">—</span>
								{/if}
							</td>
							<td class="px-4 py-3 text-zinc-500 text-xs font-mono">
								{event.actor_type}{event.actor_id ? '/' + event.actor_id.slice(0, 8) : ''}
							</td>
							<td class="px-4 py-3 text-right text-zinc-600 text-xs">
								{#if hasContext(event.context)}
									{expandedID === event.id ? '▲' : '▼'}
								{/if}
							</td>
						</tr>
						{#if expandedID === event.id}
							<tr class="bg-zinc-950">
								<td colspan="5" class="px-4 py-3">
									<pre class="text-xs text-zinc-300 font-mono whitespace-pre-wrap break-all">{JSON.stringify(event.context, null, 2)}</pre>
								</td>
							</tr>
						{/if}
					{/each}
				</tbody>
			</table>
		</div>

		{#if pagination && pagination.total > pagination.limit}
			<div class="flex items-center justify-between text-xs text-zinc-500">
				<span>{offset + 1}–{Math.min(offset + events.length, pagination.total)} of {pagination.total}</span>
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
