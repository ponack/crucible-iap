<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { varSets, type VarSet } from '$lib/api/client';
	import { auth } from '$lib/stores/auth.svelte';

	let items = $state<VarSet[]>([]);
	let loading = $state(true);
	let error = $state<string | null>(null);

	let creating = $state(false);
	let saving = $state(false);
	let formError = $state<string | null>(null);
	let form = $state({ name: '', description: '' });

	onMount(async () => {
		try {
			items = await varSets.list();
		} catch (e) {
			error = (e as Error).message;
		} finally {
			loading = false;
		}
	});

	async function create(e: SubmitEvent) {
		e.preventDefault();
		saving = true;
		formError = null;
		try {
			const vs = await varSets.create(form);
			goto(`/variable-sets/${vs.id}`);
		} catch (e) {
			formError = (e as Error).message;
			saving = false;
		}
	}
</script>

<div class="max-w-3xl space-y-6 p-6">
	<div class="flex items-center justify-between">
		<div>
			<h1 class="text-lg font-semibold text-white">Variable sets</h1>
			<p class="text-sm text-zinc-500 mt-0.5">Reusable collections of environment variables that can be attached to multiple stacks.</p>
		</div>
		{#if auth.isMemberOrAbove}
			<button
				onclick={() => (creating = !creating)}
				class="rounded-lg bg-teal-600 px-3 py-1.5 text-sm text-white transition-colors hover:bg-teal-500">
				{creating ? 'Cancel' : 'New variable set'}
			</button>
		{/if}
	</div>

	{#if creating}
		<div class="space-y-4 rounded-xl border border-zinc-800 p-5">
			<h2 class="text-sm font-medium text-zinc-300">New variable set</h2>
			{#if formError}
				<div class="rounded-lg border border-red-800 bg-red-950 px-4 py-3 text-sm text-red-300">{formError}</div>
			{/if}
			<form onsubmit={create} class="space-y-4">
				<div class="grid grid-cols-2 gap-4">
					<div class="space-y-1.5">
						<label class="field-label" for="vs-name">Name</label>
						<input id="vs-name" class="field-input" bind:value={form.name} required placeholder="e.g. aws-prod" />
					</div>
					<div class="space-y-1.5">
						<label class="field-label" for="vs-desc">Description</label>
						<input id="vs-desc" class="field-input" bind:value={form.description} placeholder="Optional description" />
					</div>
				</div>
				<div class="flex justify-end">
					<button type="submit" disabled={saving}
						class="rounded-lg bg-teal-600 px-4 py-1.5 text-sm text-white transition-colors hover:bg-teal-500 disabled:opacity-50">
						{saving ? 'Creating…' : 'Create'}
					</button>
				</div>
			</form>
		</div>
	{/if}

	{#if loading}
		<p class="text-zinc-500 text-sm">Loading…</p>
	{:else if error}
		<div class="rounded-lg border border-red-800 bg-red-950 px-4 py-3 text-sm text-red-300">{error}</div>
	{:else if items.length === 0}
		<div class="rounded-xl border border-zinc-800 p-10 text-center space-y-2">
			<p class="text-zinc-400 text-sm font-medium">No variable sets yet</p>
			<p class="text-zinc-600 text-xs">Create a variable set to share environment variables across multiple stacks.</p>
		</div>
	{:else}
		<div class="rounded-xl border border-zinc-800 overflow-hidden">
			<table class="w-full text-sm">
				<thead class="bg-zinc-900 text-zinc-500 text-xs uppercase tracking-wide">
					<tr>
						<th class="text-left px-4 py-2">Name</th>
						<th class="text-left px-4 py-2">Description</th>
						<th class="text-left px-4 py-2">Variables</th>
						<th class="text-left px-4 py-2">Updated</th>
					</tr>
				</thead>
				<tbody class="divide-y divide-zinc-700">
					{#each items as vs (vs.id)}
						<tr class="hover:bg-zinc-900/50 transition-colors cursor-pointer" onclick={() => goto(`/variable-sets/${vs.id}`)}>
							<td class="px-4 py-3 text-zinc-200 font-medium">{vs.name}</td>
							<td class="px-4 py-3 text-zinc-500">{vs.description || '—'}</td>
							<td class="px-4 py-3 text-zinc-400">{vs.var_count}</td>
							<td class="px-4 py-3 text-zinc-500 text-xs">{new Date(vs.updated_at).toLocaleString()}</td>
						</tr>
					{/each}
				</tbody>
			</table>
		</div>
	{/if}
</div>

<style>
	:global(.field-label) {
		display: block;
		font-size: 0.75rem;
		color: var(--color-zinc-400);
	}
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
		border-color: #6366f1;
	}
</style>
