<script lang="ts">
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { onMount } from 'svelte';
	import { blueprints, type Blueprint, type BlueprintParam } from '$lib/api/client';
	import { auth } from '$lib/stores/auth.svelte';

	const id = $derived(page.params.id!);

	let bp = $state<Blueprint | null>(null);
	let loading = $state(true);
	let error = $state<string | null>(null);

	let editing = $state(false);
	let saving = $state(false);
	let editError = $state<string | null>(null);
	let publishing = $state(false);

	let form = $state({
		name: '',
		description: '',
		tool: 'opentofu',
		tool_version: '',
		repo_url: '',
		repo_branch: 'main',
		project_root: '.',
		runner_image: '',
		auto_apply: false,
		drift_detection: false,
		drift_schedule: '',
		auto_remediate_drift: false,
		vcs_provider: 'github'
	});

	// Param editing
	let addingParam = $state(false);
	let paramSaving = $state(false);
	let paramError = $state<string | null>(null);
	let paramForm = $state({
		name: '',
		label: '',
		description: '',
		type: 'string' as BlueprintParam['type'],
		options: '',
		default_value: '',
		required: false,
		env_prefix: 'TF_VAR_',
		sort_order: 0
	});

	onMount(async () => {
		try {
			bp = await blueprints.get(id);
			resetForm();
		} catch (e) {
			error = (e as Error).message;
		} finally {
			loading = false;
		}
	});

	function resetForm() {
		if (!bp) return;
		form = {
			name: bp.name,
			description: bp.description,
			tool: bp.tool,
			tool_version: bp.tool_version,
			repo_url: bp.repo_url,
			repo_branch: bp.repo_branch,
			project_root: bp.project_root,
			runner_image: bp.runner_image,
			auto_apply: bp.auto_apply,
			drift_detection: bp.drift_detection,
			drift_schedule: bp.drift_schedule,
			auto_remediate_drift: bp.auto_remediate_drift,
			vcs_provider: bp.vcs_provider
		};
	}

	function resetParamForm() {
		paramForm = {
			name: '', label: '', description: '',
			type: 'string', options: '', default_value: '',
			required: false, env_prefix: 'TF_VAR_', sort_order: bp?.params?.length ?? 0
		};
	}

	async function saveEdit(e: SubmitEvent) {
		e.preventDefault();
		saving = true;
		editError = null;
		try {
			bp = await blueprints.update(id, form);
			editing = false;
		} catch (e) {
			editError = (e as Error).message;
		} finally {
			saving = false;
		}
	}

	async function togglePublish() {
		if (!bp) return;
		publishing = true;
		try {
			await blueprints.publish(id, !bp.is_published);
			bp = { ...bp, is_published: !bp.is_published };
		} catch (e) {
			alert((e as Error).message);
		} finally {
			publishing = false;
		}
	}

	async function deleteBlueprint() {
		if (!bp || !confirm(`Delete blueprint "${bp.name}"? This cannot be undone.`)) return;
		try {
			await blueprints.delete(id);
			goto('/blueprints');
		} catch (e) {
			alert((e as Error).message);
		}
	}

	async function saveParam(e: SubmitEvent) {
		e.preventDefault();
		paramSaving = true;
		paramError = null;
		try {
			const options = paramForm.options
				? paramForm.options.split(',').map((s) => s.trim()).filter(Boolean)
				: [];
			const saved = await blueprints.upsertParam(id, { ...paramForm, options });
			if (!bp) return;
			const idx = bp.params.findIndex((p) => p.name === saved.name);
			if (idx >= 0) {
				bp.params[idx] = saved;
			} else {
				bp.params = [...bp.params, saved];
			}
			addingParam = false;
			resetParamForm();
		} catch (e) {
			paramError = (e as Error).message;
		} finally {
			paramSaving = false;
		}
	}

	async function deleteParam(name: string) {
		if (!confirm(`Remove param "${name}"?`)) return;
		try {
			await blueprints.deleteParam(id, name);
			if (!bp) return;
			bp.params = bp.params.filter((p) => p.name !== name);
		} catch (e) {
			alert((e as Error).message);
		}
	}

	function editParam(p: BlueprintParam) {
		paramForm = {
			name: p.name,
			label: p.label,
			description: p.description,
			type: p.type,
			options: p.options.join(', '),
			default_value: p.default_value,
			required: p.required,
			env_prefix: p.env_prefix,
			sort_order: p.sort_order
		};
		addingParam = true;
	}
</script>

{#if loading}
	<div class="p-6"><p class="text-zinc-500 text-sm">Loading…</p></div>
{:else if error}
	<div class="p-6">
		<div class="rounded-lg border border-red-800 bg-red-950 px-4 py-3 text-sm text-red-300">{error}</div>
	</div>
{:else if bp}
<div class="max-w-3xl space-y-8 p-6">

	<!-- Header -->
	<div class="flex items-start justify-between gap-4">
		<div>
			<nav class="text-xs text-zinc-600 mb-1">
				<a href="/blueprints" class="hover:text-zinc-400 transition-colors">Blueprints</a>
				<span class="mx-1">/</span>
				<span class="text-zinc-400">{bp.name}</span>
			</nav>
			<h1 class="text-lg font-semibold text-white flex items-center gap-2">
				{bp.name}
				{#if bp.is_published}
					<span class="text-xs font-normal rounded-full bg-emerald-950 text-emerald-400 px-2 py-0.5">Published</span>
				{:else}
					<span class="text-xs font-normal rounded-full bg-zinc-800 text-zinc-500 px-2 py-0.5">Draft</span>
				{/if}
			</h1>
			{#if bp.description}
				<p class="text-sm text-zinc-500 mt-0.5">{bp.description}</p>
			{/if}
		</div>
		<div class="flex items-center gap-2 shrink-0">
			{#if bp.is_published && auth.isMemberOrAbove}
				<a href="/blueprints/{id}/deploy"
					class="rounded-lg bg-indigo-600 px-3 py-1.5 text-sm text-white transition-colors hover:bg-indigo-500">
					Deploy
				</a>
			{/if}
			{#if auth.isAdmin}
				<button onclick={togglePublish} disabled={publishing}
					class="border border-zinc-700 hover:border-zinc-500 text-zinc-300 text-sm px-3 py-1.5 rounded-lg transition-colors disabled:opacity-50">
					{publishing ? '…' : bp.is_published ? 'Unpublish' : 'Publish'}
				</button>
				<button onclick={() => { editing = !editing; if (!editing) resetForm(); }}
					class="border border-zinc-700 hover:border-zinc-500 text-zinc-300 text-sm px-3 py-1.5 rounded-lg transition-colors">
					{editing ? 'Cancel' : 'Edit'}
				</button>
				<button onclick={deleteBlueprint}
					class="border border-red-900 hover:border-red-700 text-red-400 text-sm px-3 py-1.5 rounded-lg transition-colors">
					Delete
				</button>
			{/if}
		</div>
	</div>

	{#if editing}
		<section class="space-y-4 rounded-xl border border-zinc-800 p-5">
			<h2 class="text-sm font-medium text-zinc-300">Edit blueprint</h2>
			{#if editError}
				<div class="rounded-lg border border-red-800 bg-red-950 px-4 py-3 text-sm text-red-300">{editError}</div>
			{/if}
			<form onsubmit={saveEdit} class="space-y-4">
				<div class="grid grid-cols-2 gap-4">
					<div class="space-y-1.5">
						<label class="field-label" for="b-name">Name</label>
						<input id="b-name" class="field-input" bind:value={form.name} required />
					</div>
					<div class="space-y-1.5">
						<label class="field-label" for="b-desc">Description</label>
						<input id="b-desc" class="field-input" bind:value={form.description} placeholder="Optional" />
					</div>
					<div class="space-y-1.5">
						<label class="field-label" for="b-tool">Tool</label>
						<select id="b-tool" class="field-input" bind:value={form.tool}>
							<option value="opentofu">OpenTofu</option>
							<option value="terraform">Terraform</option>
							<option value="ansible">Ansible</option>
							<option value="pulumi">Pulumi</option>
						</select>
					</div>
					<div class="space-y-1.5">
						<label class="field-label" for="b-toolv">Tool version</label>
						<input id="b-toolv" class="field-input" bind:value={form.tool_version} placeholder="e.g. 1.7.0" />
					</div>
					<div class="space-y-1.5">
						<label class="field-label" for="b-vcs">VCS provider</label>
						<select id="b-vcs" class="field-input" bind:value={form.vcs_provider}>
							<option value="github">GitHub</option>
							<option value="gitlab">GitLab</option>
							<option value="gitea">Gitea</option>
							<option value="gogs">Gogs</option>
						</select>
					</div>
					<div class="space-y-1.5">
						<label class="field-label" for="b-runner">Runner image</label>
						<input id="b-runner" class="field-input" bind:value={form.runner_image} placeholder="ghcr.io/org/runner:latest" />
					</div>
					<div class="space-y-1.5 col-span-2">
						<label class="field-label" for="b-repo">Repository URL</label>
						<input id="b-repo" class="field-input" bind:value={form.repo_url} placeholder="https://github.com/org/repo" />
					</div>
					<div class="space-y-1.5">
						<label class="field-label" for="b-branch">Branch</label>
						<input id="b-branch" class="field-input" bind:value={form.repo_branch} placeholder="main" />
					</div>
					<div class="space-y-1.5">
						<label class="field-label" for="b-root">Project root</label>
						<input id="b-root" class="field-input" bind:value={form.project_root} placeholder="." />
					</div>
					<div class="space-y-1.5 col-span-2">
						<div class="flex gap-6">
							<label class="flex items-center gap-2 cursor-pointer">
								<input type="checkbox" bind:checked={form.auto_apply} class="accent-indigo-500" />
								<span class="text-xs text-zinc-400">Auto-apply</span>
							</label>
							<label class="flex items-center gap-2 cursor-pointer">
								<input type="checkbox" bind:checked={form.drift_detection} class="accent-indigo-500" />
								<span class="text-xs text-zinc-400">Drift detection</span>
							</label>
							<label class="flex items-center gap-2 cursor-pointer">
								<input type="checkbox" bind:checked={form.auto_remediate_drift} class="accent-indigo-500" />
								<span class="text-xs text-zinc-400">Auto-remediate drift</span>
							</label>
						</div>
					</div>
					{#if form.drift_detection}
						<div class="space-y-1.5 col-span-2">
							<label class="field-label" for="b-sched">Drift schedule (cron)</label>
							<input id="b-sched" class="field-input" bind:value={form.drift_schedule} placeholder="0 */6 * * *" />
						</div>
					{/if}
				</div>
				<div class="flex justify-end">
					<button type="submit" disabled={saving}
						class="rounded-lg bg-indigo-600 px-4 py-1.5 text-sm text-white transition-colors hover:bg-indigo-500 disabled:opacity-50">
						{saving ? 'Saving…' : 'Save'}
					</button>
				</div>
			</form>
		</section>
	{:else}
		<!-- Read-only summary -->
		<section class="rounded-xl border border-zinc-800 overflow-hidden">
			<table class="w-full text-sm">
				<tbody class="divide-y divide-zinc-700">
					<tr>
						<td class="px-4 py-2.5 text-zinc-500 w-40">Tool</td>
						<td class="px-4 py-2.5 text-zinc-200">{bp.tool}{bp.tool_version ? ` ${bp.tool_version}` : ''}</td>
					</tr>
					<tr>
						<td class="px-4 py-2.5 text-zinc-500">VCS provider</td>
						<td class="px-4 py-2.5 text-zinc-200">{bp.vcs_provider}</td>
					</tr>
					<tr>
						<td class="px-4 py-2.5 text-zinc-500">Repository</td>
						<td class="px-4 py-2.5 text-zinc-200 font-mono text-xs">{bp.repo_url || '—'}</td>
					</tr>
					<tr>
						<td class="px-4 py-2.5 text-zinc-500">Branch</td>
						<td class="px-4 py-2.5 text-zinc-200">{bp.repo_branch}</td>
					</tr>
					<tr>
						<td class="px-4 py-2.5 text-zinc-500">Project root</td>
						<td class="px-4 py-2.5 text-zinc-200 font-mono text-xs">{bp.project_root}</td>
					</tr>
					{#if bp.runner_image}
						<tr>
							<td class="px-4 py-2.5 text-zinc-500">Runner image</td>
							<td class="px-4 py-2.5 text-zinc-200 font-mono text-xs">{bp.runner_image}</td>
						</tr>
					{/if}
					<tr>
						<td class="px-4 py-2.5 text-zinc-500">Auto-apply</td>
						<td class="px-4 py-2.5 text-zinc-200">{bp.auto_apply ? 'Yes' : 'No'}</td>
					</tr>
					<tr>
						<td class="px-4 py-2.5 text-zinc-500">Drift detection</td>
						<td class="px-4 py-2.5 text-zinc-200">
							{bp.drift_detection ? 'Yes' : 'No'}
							{#if bp.drift_detection && bp.drift_schedule}
								<span class="text-zinc-500 font-mono text-xs ml-2">{bp.drift_schedule}</span>
							{/if}
						</td>
					</tr>
					<tr>
						<td class="px-4 py-2.5 text-zinc-500">Auto-remediate</td>
						<td class="px-4 py-2.5 text-zinc-200">{bp.auto_remediate_drift ? 'Yes' : 'No'}</td>
					</tr>
					<tr>
						<td class="px-4 py-2.5 text-zinc-500">Updated</td>
						<td class="px-4 py-2.5 text-zinc-500 text-xs">{new Date(bp.updated_at).toLocaleString()}</td>
					</tr>
				</tbody>
			</table>
		</section>
	{/if}

	<!-- Params section -->
	<section class="space-y-4">
		<div class="flex items-center justify-between">
			<div>
				<h2 class="text-sm font-semibold text-zinc-200">Parameters</h2>
				<p class="text-xs text-zinc-500 mt-0.5">Values the deployer fills in — injected as stack env vars on deploy.</p>
			</div>
			{#if auth.isAdmin}
				<button onclick={() => { addingParam = !addingParam; if (!addingParam) resetParamForm(); }}
					class="border border-zinc-700 hover:border-zinc-500 text-zinc-300 text-xs px-3 py-1.5 rounded-lg transition-colors">
					{addingParam ? 'Cancel' : 'Add param'}
				</button>
			{/if}
		</div>

		{#if addingParam}
			<div class="rounded-xl border border-zinc-800 p-4 space-y-3">
				{#if paramError}
					<div class="rounded-lg border border-red-800 bg-red-950 px-4 py-3 text-sm text-red-300">{paramError}</div>
				{/if}
				<form onsubmit={saveParam} class="space-y-3">
					<div class="grid grid-cols-2 gap-3">
						<div class="space-y-1">
							<label class="field-label" for="p-name">Key name <span class="text-red-400">*</span></label>
							<input id="p-name" class="field-input" bind:value={paramForm.name} required placeholder="aws_region" />
						</div>
						<div class="space-y-1">
							<label class="field-label" for="p-label">Display label</label>
							<input id="p-label" class="field-input" bind:value={paramForm.label} placeholder="AWS Region" />
						</div>
						<div class="space-y-1">
							<label class="field-label" for="p-type">Type</label>
							<select id="p-type" class="field-input" bind:value={paramForm.type}>
								<option value="string">String</option>
								<option value="number">Number</option>
								<option value="bool">Boolean</option>
								<option value="select">Select</option>
							</select>
						</div>
						<div class="space-y-1">
							<label class="field-label" for="p-prefix">Env prefix</label>
							<input id="p-prefix" class="field-input" bind:value={paramForm.env_prefix} placeholder="TF_VAR_" />
						</div>
						{#if paramForm.type === 'select'}
							<div class="space-y-1 col-span-2">
								<label class="field-label" for="p-opts">Options (comma-separated)</label>
								<input id="p-opts" class="field-input" bind:value={paramForm.options} placeholder="us-east-1, us-west-2, eu-west-1" />
							</div>
						{/if}
						<div class="space-y-1 col-span-2">
							<label class="field-label" for="p-desc">Description</label>
							<input id="p-desc" class="field-input" bind:value={paramForm.description} placeholder="Help text shown to the deployer" />
						</div>
						<div class="space-y-1">
							<label class="field-label" for="p-default">Default value</label>
							<input id="p-default" class="field-input" bind:value={paramForm.default_value} placeholder="Optional" />
						</div>
						<div class="space-y-1">
							<label class="field-label" for="p-order">Sort order</label>
							<input id="p-order" type="number" class="field-input" bind:value={paramForm.sort_order} />
						</div>
						<div class="col-span-2">
							<label class="flex items-center gap-2 cursor-pointer">
								<input type="checkbox" bind:checked={paramForm.required} class="accent-indigo-500" />
								<span class="text-xs text-zinc-400">Required</span>
							</label>
						</div>
					</div>
					<div class="flex justify-end">
						<button type="submit" disabled={paramSaving}
							class="rounded-lg bg-indigo-600 px-4 py-1.5 text-sm text-white transition-colors hover:bg-indigo-500 disabled:opacity-50">
							{paramSaving ? 'Saving…' : 'Save param'}
						</button>
					</div>
				</form>
			</div>
		{/if}

		{#if bp.params.length === 0 && !addingParam}
			<div class="rounded-xl border border-zinc-800 p-6 text-center">
				<p class="text-zinc-600 text-xs">No parameters defined. Add params to let deployers customise this blueprint.</p>
			</div>
		{:else if bp.params.length > 0}
			<div class="rounded-xl border border-zinc-800 overflow-hidden">
				<table class="w-full text-sm">
					<thead class="bg-zinc-900 text-zinc-500 text-xs uppercase tracking-wide">
						<tr>
							<th class="text-left px-4 py-2">Key</th>
							<th class="text-left px-4 py-2">Label</th>
							<th class="text-left px-4 py-2">Type</th>
							<th class="text-left px-4 py-2">Default</th>
							<th class="text-left px-4 py-2">Required</th>
							{#if auth.isAdmin}<th class="px-4 py-2"></th>{/if}
						</tr>
					</thead>
					<tbody class="divide-y divide-zinc-700">
						{#each bp.params.sort((a, b) => a.sort_order - b.sort_order) as p (p.id)}
							<tr class="hover:bg-zinc-900/50 transition-colors">
								<td class="px-4 py-2.5 text-zinc-200 font-mono text-xs">
									{p.env_prefix}{p.name}
								</td>
								<td class="px-4 py-2.5 text-zinc-400 text-xs">
									{p.label || p.name}
									{#if p.description}<span class="block text-zinc-600">{p.description}</span>{/if}
								</td>
								<td class="px-4 py-2.5 text-zinc-500 text-xs">
									{p.type}
									{#if p.options.length > 0}
										<span class="block text-zinc-600">{p.options.join(', ')}</span>
									{/if}
								</td>
								<td class="px-4 py-2.5 text-zinc-500 text-xs font-mono">{p.default_value || '—'}</td>
								<td class="px-4 py-2.5 text-xs">
									{#if p.required}
										<span class="text-amber-400">Yes</span>
									{:else}
										<span class="text-zinc-600">No</span>
									{/if}
								</td>
								{#if auth.isAdmin}
									<td class="px-4 py-2.5 text-right">
										<button onclick={() => editParam(p)} class="text-xs text-zinc-500 hover:text-zinc-300 mr-3 transition-colors">Edit</button>
										<button onclick={() => deleteParam(p.name)} class="text-xs text-red-600 hover:text-red-400 transition-colors">Remove</button>
									</td>
								{/if}
							</tr>
						{/each}
					</tbody>
				</table>
			</div>
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
