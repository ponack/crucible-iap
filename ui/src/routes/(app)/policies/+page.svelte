<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { policies, type Policy } from '$lib/api/client';

	let items = $state<Policy[]>([]);
	let loading = $state(true);
	let error = $state<string | null>(null);

	// New policy form
	let creating = $state(false);
	let saving = $state(false);
	let formError = $state<string | null>(null);
	let form = $state({
		name: '',
		description: '',
		type: 'post_plan' as Policy['type'],
		body: defaultRego('post_plan'),
		is_active: true
	});

	function defaultRego(type: string): string {
		return `package crucible

# ${type} policy — edit to define your rules
plan := result {
  result := {
    "deny":    deny_msgs,
    "warn":    warn_msgs,
    "trigger": [],
  }
}

deny_msgs[msg] {
  # Example: block destroy operations
  input.resource_changes[_].change.actions[_] == "delete"
  msg := "destroy operations require an explicit destroy run"
}

warn_msgs[msg] {
  input.resource_changes[_].change.actions[_] == "update"
  msg := sprintf("resource %v will be updated", [input.resource_changes[_].address])
}
`;
	}

	onMount(async () => {
		try {
			items = await policies.list();
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
			const p = await policies.create(form);
			goto(`/policies/${p.id}`);
		} catch (e) {
			formError = (e as Error).message;
			saving = false;
		}
	}

	const typeLabels: Record<string, string> = {
		post_plan: 'Post-plan',
		pre_plan: 'Pre-plan',
		pre_apply: 'Pre-apply',
		trigger: 'Trigger',
		login: 'Login'
	};

	const typeBadge: Record<string, string> = {
		post_plan: 'bg-indigo-900 text-indigo-300',
		pre_plan: 'bg-sky-900 text-sky-300',
		pre_apply: 'bg-violet-900 text-violet-300',
		trigger: 'bg-amber-900 text-amber-300',
		login: 'bg-rose-900 text-rose-300'
	};
</script>

<div class="p-6 space-y-6 max-w-3xl">
	<div class="flex items-center justify-between">
		<h1 class="text-lg font-semibold text-white">Policies</h1>
		<button
			onclick={() => (creating = !creating)}
			class="bg-indigo-600 hover:bg-indigo-500 text-white text-sm px-3 py-1.5 rounded-lg transition-colors"
		>
			{creating ? 'Cancel' : 'New policy'}
		</button>
	</div>

	{#if creating}
		<div class="border border-zinc-800 rounded-xl p-5 space-y-4">
			<h2 class="text-sm font-medium text-zinc-300">New policy</h2>
			{#if formError}
				<div class="bg-red-950 border border-red-800 rounded-lg px-4 py-3 text-red-300 text-sm">{formError}</div>
			{/if}
			<form onsubmit={create} class="space-y-4">
				<div class="grid grid-cols-2 gap-4">
					<div class="space-y-1.5">
						<label class="field-label" for="p-name">Name</label>
						<input id="p-name" class="field-input" bind:value={form.name} required placeholder="e.g. no-destroy" />
					</div>
					<div class="space-y-1.5">
						<label class="field-label" for="p-type">Type</label>
						<select id="p-type" class="field-input" bind:value={form.type}
							onchange={() => (form.body = defaultRego(form.type))}>
							{#each Object.entries(typeLabels) as [val, label]}
								<option value={val}>{label}</option>
							{/each}
						</select>
					</div>
				</div>
				<div class="space-y-1.5">
					<label class="field-label" for="p-desc">Description</label>
					<input id="p-desc" class="field-input" bind:value={form.description} placeholder="Optional" />
				</div>
				<div class="space-y-1.5">
					<label class="field-label" for="p-body">Rego source</label>
					<textarea id="p-body" class="field-input font-mono text-xs" rows="14"
						bind:value={form.body} required></textarea>
				</div>
				<div class="flex items-center justify-between">
					<label class="flex items-center gap-2 cursor-pointer text-sm text-zinc-300">
						<input type="checkbox" bind:checked={form.is_active} />
						Active (evaluated on runs immediately)
					</label>
					<button type="submit" disabled={saving}
						class="bg-indigo-600 hover:bg-indigo-500 disabled:opacity-50 text-white text-sm px-4 py-1.5 rounded-lg transition-colors">
						{saving ? 'Creating…' : 'Create policy'}
					</button>
				</div>
			</form>
		</div>
	{/if}

	{#if loading}
		<p class="text-zinc-500 text-sm">Loading…</p>
	{:else if error}
		<p class="text-red-400 text-sm">{error}</p>
	{:else if items.length === 0}
		<div class="border border-zinc-800 rounded-xl p-8 text-center space-y-2">
			<p class="text-zinc-400 text-sm">No policies yet.</p>
			<p class="text-zinc-600 text-xs">Create a policy to enforce guardrails on plan and apply operations.</p>
		</div>
	{:else}
		<div class="border border-zinc-800 rounded-xl overflow-hidden">
			<table class="w-full text-sm">
				<thead class="bg-zinc-900 text-zinc-500 text-xs uppercase tracking-wide">
					<tr>
						<th class="text-left px-4 py-2">Name</th>
						<th class="text-left px-4 py-2">Type</th>
						<th class="text-left px-4 py-2">Status</th>
						<th class="text-left px-4 py-2">Updated</th>
					</tr>
				</thead>
				<tbody class="divide-y divide-zinc-800">
					{#each items as p (p.id)}
						<tr class="hover:bg-zinc-900/50 transition-colors cursor-pointer"
							onclick={() => goto(`/policies/${p.id}`)}>
							<td class="px-4 py-3 text-zinc-200 font-medium">
								{p.name}
								{#if p.description}
									<span class="block text-xs text-zinc-500 font-normal mt-0.5">{p.description}</span>
								{/if}
							</td>
							<td class="px-4 py-3">
								<span class="text-xs px-1.5 py-0.5 rounded font-medium {typeBadge[p.type] ?? 'bg-zinc-800 text-zinc-400'}">
									{typeLabels[p.type] ?? p.type}
								</span>
							</td>
							<td class="px-4 py-3">
								<span class="text-xs {p.is_active ? 'text-green-400' : 'text-zinc-500'}">
									{p.is_active ? 'Active' : 'Inactive'}
								</span>
							</td>
							<td class="px-4 py-3 text-zinc-500 text-xs">
								{new Date(p.updated_at).toLocaleString()}
							</td>
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
