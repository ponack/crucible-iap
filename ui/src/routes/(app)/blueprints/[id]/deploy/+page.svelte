<script lang="ts">
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { onMount } from 'svelte';
	import { blueprints, type Blueprint } from '$lib/api/client';

	const id = $derived(page.params.id!);

	let bp = $state<Blueprint | null>(null);
	let loading = $state(true);
	let error = $state<string | null>(null);

	let stackName = $state('');
	let values = $state<Record<string, string>>({});
	let deploying = $state(false);
	let deployError = $state<string | null>(null);

	onMount(async () => {
		try {
			bp = await blueprints.get(id);
			if (!bp.is_published) {
				goto(`/blueprints/${id}`);
				return;
			}
			// Seed defaults
			const init: Record<string, string> = {};
			for (const p of bp.params) {
				init[p.name] = p.default_value;
			}
			values = init;
		} catch (e) {
			error = (e as Error).message;
		} finally {
			loading = false;
		}
	});

	async function deploy(e: SubmitEvent) {
		e.preventDefault();
		deploying = true;
		deployError = null;
		try {
			const { stack_id } = await blueprints.deploy(id, stackName, values);
			goto(`/stacks/${stack_id}`);
		} catch (e) {
			deployError = (e as Error).message;
			deploying = false;
		}
	}
</script>

{#if loading}
	<div class="p-6"><p class="text-zinc-500 text-sm">Loading…</p></div>
{:else if error}
	<div class="p-6">
		<div class="rounded-lg border border-red-800 bg-red-950 px-4 py-3 text-sm text-red-300">{error}</div>
	</div>
{:else if bp}
<div class="max-w-xl space-y-6 p-6">

	<div>
		<nav class="text-xs text-zinc-600 mb-1">
			<a href="/blueprints" class="hover:text-zinc-400 transition-colors">Blueprints</a>
			<span class="mx-1">/</span>
			<a href="/blueprints/{id}" class="hover:text-zinc-400 transition-colors">{bp.name}</a>
			<span class="mx-1">/</span>
			<span class="text-zinc-400">Deploy</span>
		</nav>
		<h1 class="text-lg font-semibold text-white">Deploy {bp.name}</h1>
		{#if bp.description}
			<p class="text-sm text-zinc-500 mt-0.5">{bp.description}</p>
		{/if}
	</div>

	{#if deployError}
		<div class="rounded-lg border border-red-800 bg-red-950 px-4 py-3 text-sm text-red-300">{deployError}</div>
	{/if}

	<form onsubmit={deploy} class="space-y-5">
		<div class="space-y-1.5">
			<label class="field-label" for="stack-name">Stack name <span class="text-red-400">*</span></label>
			<input id="stack-name" class="field-input" bind:value={stackName} required
				placeholder="e.g. team-payments-prod" />
			<p class="text-xs text-zinc-600">Unique name for the new stack that will be created.</p>
		</div>

		{#if (bp.params?.length ?? 0) > 0}
			<div class="space-y-4 rounded-xl border border-zinc-800 p-4">
				<h2 class="text-sm font-medium text-zinc-300">Parameters</h2>
				{#each [...(bp.params ?? [])].sort((a, b) => a.sort_order - b.sort_order) as p (p.id)}
					<div class="space-y-1.5">
						<label class="field-label" for="param-{p.name}">
							{p.label || p.name}
							{#if p.required}<span class="text-red-400 ml-0.5">*</span>{/if}
							<span class="ml-1 font-mono text-zinc-600">{p.env_prefix}{p.name}</span>
						</label>
						{#if p.description}
							<p class="text-xs text-zinc-600 -mt-0.5">{p.description}</p>
						{/if}
						{#if p.type === 'bool'}
							<select id="param-{p.name}" class="field-input" bind:value={values[p.name]}>
								<option value="true">true</option>
								<option value="false">false</option>
							</select>
						{:else if p.type === 'select' && p.options.length > 0}
							<select id="param-{p.name}" class="field-input" bind:value={values[p.name]}>
								{#if !p.required}<option value="">— select —</option>{/if}
								{#each p.options as opt}
									<option value={opt}>{opt}</option>
								{/each}
							</select>
						{:else}
							<input id="param-{p.name}" class="field-input"
								type={p.type === 'number' ? 'number' : 'text'}
								bind:value={values[p.name]}
								required={p.required}
								placeholder={p.default_value || ''} />
						{/if}
					</div>
				{/each}
			</div>
		{/if}

		<div class="flex items-center justify-between">
			<a href="/blueprints/{id}" class="text-sm text-zinc-500 hover:text-zinc-300 transition-colors">Cancel</a>
			<button type="submit" disabled={deploying}
				class="rounded-lg bg-teal-600 px-5 py-2 text-sm text-white transition-colors hover:bg-teal-500 disabled:opacity-50">
				{deploying ? 'Deploying…' : 'Deploy stack'}
			</button>
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
