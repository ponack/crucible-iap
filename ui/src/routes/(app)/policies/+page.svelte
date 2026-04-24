<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { policies, type Policy } from '$lib/api/client';
	import { auth } from '$lib/stores/auth.svelte';
	import RegoEditor from '$lib/components/RegoEditor.svelte';
	import PolicyInputSchema from '$lib/components/PolicyInputSchema.svelte';
	import { policyTemplates, type PolicyType } from '$lib/policy-data';

	let items = $state<Policy[]>([]);
	let loading = $state(true);
	let error = $state<string | null>(null);

	// New policy form
	let creating = $state(false);
	let saving = $state(false);
	let formError = $state<string | null>(null);
	let validating = $state(false);
	let validateResult = $state<{ ok: boolean; error?: string } | null>(null);
	let selectedTemplate = $state(0);

	let form = $state({
		name: '',
		description: '',
		type: 'post_plan' as PolicyType,
		body: policyTemplates['post_plan'][0].body,
		is_active: true
	});

	const templates = $derived(policyTemplates[form.type]);

	function onTypeChange() {
		selectedTemplate = 0;
		form.body = templates[0].body;
		validateResult = null;
	}

	function onTemplateChange() {
		form.body = templates[selectedTemplate].body;
		validateResult = null;
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

	async function validateRego() {
		validating = true;
		validateResult = null;
		try {
			validateResult = await policies.validate(form.type, form.body);
		} catch (e) {
			validateResult = { ok: false, error: (e as Error).message };
		} finally {
			validating = false;
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

<div class="max-w-3xl space-y-6 p-6">
	<div class="flex items-center justify-between">
		<h1 class="text-lg font-semibold text-white">Policies</h1>
		<div class="flex items-center gap-2">
			<a
				href="/policies/test"
				class="rounded-lg border border-zinc-700 px-3 py-1.5 text-sm text-zinc-300 transition-colors hover:border-zinc-500 hover:text-white"
			>
				Test playground
			</a>
			<button
				onclick={() => (creating = !creating)}
				class="rounded-lg bg-indigo-600 px-3 py-1.5 text-sm text-white transition-colors hover:bg-indigo-500"
			>
				{creating ? 'Cancel' : 'New policy'}
			</button>
		</div>
	</div>

	{#if creating}
		<div class="space-y-4 rounded-xl border border-zinc-800 p-5">
			<h2 class="text-sm font-medium text-zinc-300">New policy</h2>
			{#if formError}
				<div class="rounded-lg border border-red-800 bg-red-950 px-4 py-3 text-sm text-red-300">
					{formError}
				</div>
			{/if}
			<form onsubmit={create} class="space-y-4">
				<div class="grid grid-cols-2 gap-4">
					<div class="space-y-1.5">
						<label class="field-label" for="p-name">Name</label>
						<input
							id="p-name"
							class="field-input"
							bind:value={form.name}
							required
							placeholder="e.g. no-destroy"
						/>
					</div>
					<div class="space-y-1.5">
						<label class="field-label" for="p-type">Type</label>
						<select id="p-type" class="field-input" bind:value={form.type} onchange={onTypeChange}>
							{#each Object.entries(typeLabels) as [val, label]}
								<option value={val}>{label}</option>
							{/each}
						</select>
					</div>
				</div>

				<div class="space-y-1.5">
					<label class="field-label" for="p-desc">Description</label>
					<input
						id="p-desc"
						class="field-input"
						bind:value={form.description}
						placeholder="Optional"
					/>
				</div>

				<!-- Template selector -->
				<div class="space-y-1.5">
					<label class="field-label" for="p-template">Starter template</label>
					<select
						id="p-template"
						class="field-input"
						bind:value={selectedTemplate}
						onchange={onTemplateChange}
					>
						{#each templates as t, i}
							<option value={i}>{t.name} — {t.description}</option>
						{/each}
					</select>
				</div>

				<!-- Rego editor -->
				<div class="space-y-1.5">
					<div class="flex items-center justify-between">
						<label class="field-label" for="p-body">Rego source</label>
						<button
							type="button"
							onclick={validateRego}
							disabled={validating || !form.body}
							class="text-xs text-zinc-400 transition-colors hover:text-zinc-200 disabled:opacity-40"
						>
							{validating ? 'Validating…' : 'Validate syntax'}
						</button>
					</div>
					<RegoEditor bind:value={form.body} minLines={14} />
					{#if validateResult}
						{#if validateResult.ok}
							<p class="text-xs text-green-400">Syntax valid — no compile errors.</p>
						{:else}
							<p class="whitespace-pre-wrap font-mono text-xs text-red-400">
								{validateResult.error}
							</p>
						{/if}
					{/if}
				</div>

				<!-- Input reference -->
				<PolicyInputSchema type={form.type} />

				<div class="flex items-center justify-between">
					<label class="flex cursor-pointer items-center gap-2 text-sm text-zinc-300">
						<input type="checkbox" bind:checked={form.is_active} />
						Active (evaluated on runs immediately)
					</label>
					<button
						type="submit"
						disabled={saving}
						class="rounded-lg bg-indigo-600 px-4 py-1.5 text-sm text-white transition-colors hover:bg-indigo-500 disabled:opacity-50"
					>
						{saving ? 'Creating…' : 'Create policy'}
					</button>
				</div>
			</form>
		</div>
	{/if}

	{#if loading}
		<p class="text-sm text-zinc-500">Loading…</p>
	{:else if error}
		<p class="text-sm text-red-400">{error}</p>
	{:else if items.length === 0}
		<div class="rounded-xl border border-zinc-800 p-8 text-center space-y-2">
			<p class="text-sm text-zinc-400">No policies yet.</p>
			<p class="text-xs text-zinc-600">
				Create a policy to enforce guardrails on plan and apply operations.
			</p>
		</div>
	{:else}
		<div class="overflow-hidden rounded-xl border border-zinc-800">
			<table class="w-full text-sm">
				<thead class="bg-zinc-900 text-xs uppercase tracking-wide text-zinc-500">
					<tr>
						<th class="px-4 py-2 text-left">Name</th>
						<th class="px-4 py-2 text-left">Type</th>
						<th class="px-4 py-2 text-left">Status</th>
						<th class="px-4 py-2 text-left">Updated</th>
					</tr>
				</thead>
				<tbody class="divide-y divide-zinc-800">
					{#each items as p (p.id)}
						<tr
							class="cursor-pointer transition-colors hover:bg-zinc-900/50"
							onclick={() => goto(`/policies/${p.id}`)}
						>
							<td class="px-4 py-3 font-medium text-zinc-200">
								{p.name}
								{#if p.description}
									<span class="mt-0.5 block text-xs font-normal text-zinc-500"
										>{p.description}</span
									>
								{/if}
							</td>
							<td class="px-4 py-3">
								<span
									class="rounded px-1.5 py-0.5 text-xs font-medium {typeBadge[p.type] ??
										'bg-zinc-800 text-zinc-400'}"
								>
									{typeLabels[p.type] ?? p.type}
								</span>
							</td>
							<td class="px-4 py-3">
								<span class="text-xs {p.is_active ? 'text-green-400' : 'text-zinc-500'}">
									{p.is_active ? 'Active' : 'Inactive'}
								</span>
							</td>
							<td class="px-4 py-3 text-xs text-zinc-500">
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
