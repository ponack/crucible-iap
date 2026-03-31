<script lang="ts">
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { onMount } from 'svelte';
	import { policies, type Policy } from '$lib/api/client';

	const id = $derived(page.params.id);

	let policy = $state<Policy | null>(null);
	let loading = $state(true);
	let error = $state<string | null>(null);
	let saving = $state(false);
	let saveError = $state<string | null>(null);
	let saved = $state(false);

	let form = $state({ name: '', description: '', body: '', is_active: true });

	onMount(async () => {
		try {
			policy = await policies.get(id);
			resetForm();
		} catch (e) {
			error = (e as Error).message;
		} finally {
			loading = false;
		}
	});

	function resetForm() {
		if (!policy) return;
		form = {
			name: policy.name,
			description: policy.description ?? '',
			body: policy.body,
			is_active: policy.is_active
		};
	}

	async function save(e: SubmitEvent) {
		e.preventDefault();
		saving = true;
		saveError = null;
		saved = false;
		try {
			policy = await policies.update(id, form);
			saved = true;
			setTimeout(() => (saved = false), 3000);
		} catch (e) {
			saveError = (e as Error).message;
		} finally {
			saving = false;
		}
	}

	async function deletePolicy() {
		if (!confirm(`Delete policy "${policy?.name}"? Stacks using it will no longer have it evaluated.`)) return;
		await policies.delete(id);
		goto('/policies');
	}

	const typeLabels: Record<string, string> = {
		post_plan: 'Post-plan',
		pre_plan: 'Pre-plan',
		pre_apply: 'Pre-apply',
		trigger: 'Trigger',
		login: 'Login'
	};
</script>

{#if loading}
	<div class="p-6 text-zinc-500 text-sm">Loading…</div>
{:else if error || !policy}
	<div class="p-6 text-red-400 text-sm">{error ?? 'Policy not found'}</div>
{:else}
<div class="p-6 space-y-6 max-w-3xl">

	<!-- Header -->
	<div class="flex items-start justify-between">
		<div>
			<div class="flex items-center gap-2 text-sm text-zinc-500 mb-1">
				<a href="/policies" class="hover:text-zinc-300">Policies</a>
				<span>/</span>
				<span class="text-white font-medium">{policy.name}</span>
			</div>
			<div class="flex items-center gap-2 text-xs text-zinc-500">
				<span class="px-1.5 py-0.5 rounded bg-zinc-800 text-zinc-400">
					{typeLabels[policy.type] ?? policy.type}
				</span>
				<span class={policy.is_active ? 'text-green-400' : 'text-zinc-500'}>
					{policy.is_active ? 'Active' : 'Inactive'}
				</span>
			</div>
		</div>
		<button onclick={deletePolicy}
			class="border border-red-900 hover:border-red-700 text-red-400 text-sm px-3 py-1.5 rounded-lg transition-colors">
			Delete
		</button>
	</div>

	<!-- Editor -->
	<form onsubmit={save} class="space-y-4">
		{#if saveError}
			<div class="bg-red-950 border border-red-800 rounded-lg px-4 py-3 text-red-300 text-sm">{saveError}</div>
		{/if}
		{#if saved}
			<div class="bg-green-950 border border-green-800 rounded-lg px-4 py-3 text-green-300 text-sm">Policy saved and reloaded into engine.</div>
		{/if}

		<div class="grid grid-cols-2 gap-4">
			<div class="space-y-1.5">
				<label class="field-label" for="p-name">Name</label>
				<input id="p-name" class="field-input" bind:value={form.name} required />
			</div>
			<div class="space-y-1.5">
				<label class="field-label">Type</label>
				<input class="field-input opacity-60 cursor-not-allowed" value={typeLabels[policy.type] ?? policy.type} disabled />
			</div>
		</div>

		<div class="space-y-1.5">
			<label class="field-label" for="p-desc">Description</label>
			<input id="p-desc" class="field-input" bind:value={form.description} placeholder="Optional" />
		</div>

		<div class="space-y-1.5">
			<label class="field-label" for="p-body">Rego source</label>
			<textarea id="p-body" class="field-input font-mono text-xs" rows="20"
				bind:value={form.body} required spellcheck="false"></textarea>
			<p class="text-xs text-zinc-600">
				Policies must export <code class="text-zinc-400">deny_msgs</code> (blocking) and optionally
				<code class="text-zinc-400">warn_msgs</code>. The input is the Terraform/OpenTofu plan JSON.
			</p>
		</div>

		<div class="flex items-center justify-between pt-1">
			<label class="flex items-center gap-2 cursor-pointer text-sm text-zinc-300">
				<input type="checkbox" bind:checked={form.is_active} />
				Active
			</label>
			<div class="flex gap-3">
				<button type="button" onclick={resetForm}
					class="border border-zinc-700 hover:border-zinc-500 text-zinc-400 text-sm px-3 py-1.5 rounded-lg transition-colors">
					Reset
				</button>
				<button type="submit" disabled={saving}
					class="bg-indigo-600 hover:bg-indigo-500 disabled:opacity-50 text-white text-sm px-4 py-1.5 rounded-lg transition-colors">
					{saving ? 'Saving…' : 'Save policy'}
				</button>
			</div>
		</div>
	</form>
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
