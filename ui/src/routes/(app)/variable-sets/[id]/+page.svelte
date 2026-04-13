<script lang="ts">
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { onMount } from 'svelte';
	import { varSets, type VarSetDetail, type VarMeta } from '$lib/api/client';
	import { auth } from '$lib/stores/auth.svelte';

	const id = $derived(page.params.id!);

	let vs = $state<VarSetDetail | null>(null);
	let loading = $state(true);
	let error = $state<string | null>(null);

	// Edit name/description
	let editing = $state(false);
	let saving = $state(false);
	let editError = $state<string | null>(null);
	let form = $state({ name: '', description: '' });

	// Add variable
	let newVarName = $state('');
	let newVarValue = $state('');
	let newVarSecret = $state(true);
	let savingVar = $state(false);
	let varError = $state<string | null>(null);

	onMount(async () => {
		try {
			vs = await varSets.get(id);
			resetForm();
		} catch (e) {
			error = (e as Error).message;
		} finally {
			loading = false;
		}
	});

	function resetForm() {
		if (!vs) return;
		form = { name: vs.name, description: vs.description };
	}

	async function saveEdit(e: SubmitEvent) {
		e.preventDefault();
		saving = true;
		editError = null;
		try {
			const updated = await varSets.update(id, form);
			vs = { ...vs!, ...updated };
			editing = false;
		} catch (e) {
			editError = (e as Error).message;
		} finally {
			saving = false;
		}
	}

	async function addVar(e: SubmitEvent) {
		e.preventDefault();
		savingVar = true;
		varError = null;
		try {
			const v = await varSets.upsertVar(id, newVarName, newVarValue, newVarSecret);
			// Replace existing or append
			const idx = vs!.vars.findIndex((vr) => vr.name === v.name);
			if (idx >= 0) {
				vs!.vars[idx] = v;
			} else {
				vs!.vars = [...vs!.vars, v].sort((a, b) => a.name.localeCompare(b.name));
				vs!.var_count += 1;
			}
			newVarName = '';
			newVarValue = '';
			newVarSecret = true;
		} catch (e) {
			varError = (e as Error).message;
		} finally {
			savingVar = false;
		}
	}

	async function deleteVar(name: string) {
		if (!confirm(`Delete variable "${name}"?`)) return;
		try {
			await varSets.deleteVar(id, name);
			vs!.vars = vs!.vars.filter((v) => v.name !== name);
			vs!.var_count -= 1;
		} catch (e) {
			alert((e as Error).message);
		}
	}

	async function deleteSet() {
		if (!vs || !confirm(`Delete variable set "${vs.name}"? This cannot be undone.`)) return;
		try {
			await varSets.delete(id);
			goto('/variable-sets');
		} catch (e) {
			alert((e as Error).message);
		}
	}
</script>

{#if loading}
	<div class="p-6"><p class="text-zinc-500 text-sm">Loading…</p></div>
{:else if error}
	<div class="p-6">
		<div class="rounded-lg border border-red-800 bg-red-950 px-4 py-3 text-sm text-red-300">{error}</div>
	</div>
{:else if vs}
<div class="max-w-3xl space-y-8 p-6">

	<!-- Header -->
	<div class="flex items-start justify-between gap-4">
		<div>
			<nav class="text-xs text-zinc-600 mb-1">
				<a href="/variable-sets" class="hover:text-zinc-400 transition-colors">Variable sets</a>
				<span class="mx-1">/</span>
				<span class="text-zinc-400">{vs.name}</span>
			</nav>
			<h1 class="text-lg font-semibold text-white">{vs.name}</h1>
			{#if vs.description}
				<p class="text-sm text-zinc-500 mt-0.5">{vs.description}</p>
			{/if}
		</div>
		<div class="flex items-center gap-2 shrink-0">
			{#if auth.isMemberOrAbove}
				<button onclick={() => { editing = !editing; if (!editing) resetForm(); }}
					class="border border-zinc-700 hover:border-zinc-500 text-zinc-300 text-sm px-3 py-1.5 rounded-lg transition-colors">
					{editing ? 'Cancel' : 'Edit'}
				</button>
			{/if}
			{#if auth.isAdmin}
				<button onclick={deleteSet}
					class="border border-red-900 hover:border-red-700 text-red-400 text-sm px-3 py-1.5 rounded-lg transition-colors">
					Delete
				</button>
			{/if}
		</div>
	</div>

	<!-- Edit form -->
	{#if editing}
		<section class="space-y-4 rounded-xl border border-zinc-800 p-5">
			<h2 class="text-sm font-medium text-zinc-300">Edit variable set</h2>
			{#if editError}
				<div class="rounded-lg border border-red-800 bg-red-950 px-4 py-3 text-sm text-red-300">{editError}</div>
			{/if}
			<form onsubmit={saveEdit} class="space-y-4">
				<div class="grid grid-cols-2 gap-4">
					<div class="space-y-1.5">
						<label class="field-label" for="vs-name">Name</label>
						<input id="vs-name" class="field-input" bind:value={form.name} required />
					</div>
					<div class="space-y-1.5">
						<label class="field-label" for="vs-desc">Description</label>
						<input id="vs-desc" class="field-input" bind:value={form.description} placeholder="Optional" />
					</div>
				</div>
				<div class="flex justify-end">
					<button type="submit" disabled={saving}
						class="rounded-lg bg-indigo-600 px-4 py-1.5 text-sm text-white transition-colors hover:bg-indigo-500 disabled:opacity-50">
						{saving ? 'Saving…' : 'Save'}
					</button>
				</div>
			</form>
		</section>
	{/if}

	<!-- Variables -->
	<section class="space-y-3">
		<h2 class="text-sm font-medium text-zinc-400 uppercase tracking-wide">Variables</h2>
		<p class="text-xs text-zinc-500">
			Values are write-only — they are encrypted at rest and never returned by the API.
			Re-enter a variable to update its value.
		</p>

		{#if vs.vars.length > 0}
			<div class="border border-zinc-800 rounded-xl overflow-hidden">
				<table class="w-full text-sm">
					<thead class="bg-zinc-900 text-zinc-500 text-xs uppercase tracking-wide">
						<tr>
							<th class="text-left px-4 py-2">Name</th>
							<th class="text-left px-4 py-2">Type</th>
							<th class="text-left px-4 py-2">Updated</th>
							<th class="px-4 py-2"></th>
						</tr>
					</thead>
					<tbody class="divide-y divide-zinc-800">
						{#each vs.vars as v (v.id)}
							<tr>
								<td class="px-4 py-2.5 font-mono text-xs text-zinc-300">{v.name}</td>
								<td class="px-4 py-2.5 text-xs text-zinc-500">
									{#if v.is_secret}
										<span class="text-amber-500">secret</span>
									{:else}
										plain
									{/if}
								</td>
								<td class="px-4 py-2.5 text-xs text-zinc-600">{new Date(v.updated_at).toLocaleString()}</td>
								<td class="px-4 py-2.5 text-right">
									{#if auth.isMemberOrAbove}
										<button onclick={() => deleteVar(v.name)} class="text-xs text-red-500 hover:text-red-300">Delete</button>
									{/if}
								</td>
							</tr>
						{/each}
					</tbody>
				</table>
			</div>
		{/if}

		{#if auth.isMemberOrAbove}
			{#if varError}
				<div class="rounded-lg border border-red-800 bg-red-950 px-4 py-3 text-sm text-red-300">{varError}</div>
			{/if}
			<form onsubmit={addVar} class="flex items-end gap-2">
				<div class="space-y-1 flex-1">
					<label class="field-label" for="var-name">Name</label>
					<input id="var-name" class="field-input font-mono" bind:value={newVarName} placeholder="MY_VAR" required />
				</div>
				<div class="space-y-1 flex-1">
					<label class="field-label" for="var-value">Value</label>
					<input id="var-value" class="field-input" type="password" bind:value={newVarValue} placeholder="••••••••" required />
				</div>
				<div class="flex items-center gap-1.5 pb-1.5">
					<input id="var-secret" type="checkbox" bind:checked={newVarSecret} class="accent-indigo-500" />
					<label for="var-secret" class="text-xs text-zinc-400">Secret</label>
				</div>
				<button type="submit" disabled={savingVar}
					class="border border-zinc-700 hover:border-zinc-500 text-zinc-300 text-sm px-3 py-1.5 rounded-lg transition-colors disabled:opacity-50 whitespace-nowrap">
					{savingVar ? 'Saving…' : 'Add variable'}
				</button>
			</form>
		{/if}
	</section>

</div>
{/if}

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
