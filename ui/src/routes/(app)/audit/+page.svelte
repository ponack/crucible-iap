<script lang="ts">
	import { onMount } from 'svelte';
	import { audit, type AuditEvent, type PageMeta } from '$lib/api/client';

	let events = $state<AuditEvent[]>([]);
	let pagination = $state<PageMeta | null>(null);
	let offset = $state(0);
	let loading = $state(true);
	let error = $state<string | null>(null);
	let expandedID = $state<number | null>(null);

	async function load() {
		loading = true;
		error = null;
		try {
			const res = await audit.list(offset);
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
		'run.created': 'text-blue-400',
		'run.confirmed': 'text-green-400',
		'run.discarded': 'text-zinc-400',
		'run.canceled': 'text-zinc-400',
		'run.preparing': 'text-blue-400',
		'run.planning': 'text-blue-400',
		'run.unconfirmed': 'text-yellow-400',
		'run.applying': 'text-blue-400',
		'run.finished': 'text-green-400',
		'run.failed': 'text-red-400',
		'stack.created': 'text-indigo-400',
		'stack.deleted': 'text-red-400'
	};
</script>

<div class="p-6 space-y-4">
	<h1 class="text-lg font-semibold text-white">Audit log</h1>

	{#if loading}
		<p class="text-zinc-500 text-sm">Loading…</p>
	{:else if error}
		<p class="text-red-400 text-sm">{error}</p>
	{:else if events.length === 0}
		<div class="border border-zinc-800 rounded-xl p-12 text-center">
			<p class="text-zinc-400 text-sm">No audit events yet.</p>
		</div>
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
				<tbody class="divide-y divide-zinc-800">
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
