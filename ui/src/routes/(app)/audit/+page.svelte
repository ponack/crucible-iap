<script lang="ts">
	import { onMount } from 'svelte';
	import { audit, type AuditEvent } from '$lib/api/client';

	let events = $state<AuditEvent[]>([]);
	let loading = $state(true);
	let error = $state<string | null>(null);

	onMount(async () => {
		try {
			events = await audit.list();
		} catch (e) {
			error = (e as Error).message;
		} finally {
			loading = false;
		}
	});

	function fmtDate(iso: string) {
		return new Date(iso).toLocaleString();
	}

	const actionColour: Record<string, string> = {
		'run.created': 'text-blue-400',
		'run.confirmed': 'text-green-400',
		'run.discarded': 'text-zinc-400',
		'run.canceled': 'text-zinc-400',
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
					</tr>
				</thead>
				<tbody class="divide-y divide-zinc-800">
					{#each events as event (event.id)}
						<tr class="hover:bg-zinc-900/50 transition-colors">
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
						</tr>
					{/each}
				</tbody>
			</table>
		</div>
	{/if}
</div>
