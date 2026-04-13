<script lang="ts">
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { onMount } from 'svelte';
	import { stackTemplates, type StackTemplate } from '$lib/api/client';
	import { auth } from '$lib/stores/auth.svelte';

	const id = $derived(page.params.id!);

	let tmpl = $state<StackTemplate | null>(null);
	let loading = $state(true);
	let error = $state<string | null>(null);

	let editing = $state(false);
	let saving = $state(false);
	let editError = $state<string | null>(null);
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

	onMount(async () => {
		try {
			tmpl = await stackTemplates.get(id);
			resetForm();
		} catch (e) {
			error = (e as Error).message;
		} finally {
			loading = false;
		}
	});

	function resetForm() {
		if (!tmpl) return;
		form = {
			name: tmpl.name,
			description: tmpl.description,
			tool: tmpl.tool,
			tool_version: tmpl.tool_version,
			repo_url: tmpl.repo_url,
			repo_branch: tmpl.repo_branch,
			project_root: tmpl.project_root,
			runner_image: tmpl.runner_image,
			auto_apply: tmpl.auto_apply,
			drift_detection: tmpl.drift_detection,
			drift_schedule: tmpl.drift_schedule,
			auto_remediate_drift: tmpl.auto_remediate_drift,
			vcs_provider: tmpl.vcs_provider
		};
	}

	async function saveEdit(e: SubmitEvent) {
		e.preventDefault();
		saving = true;
		editError = null;
		try {
			tmpl = await stackTemplates.update(id, form);
			editing = false;
		} catch (e) {
			editError = (e as Error).message;
		} finally {
			saving = false;
		}
	}

	async function deleteTemplate() {
		if (!tmpl || !confirm(`Delete template "${tmpl.name}"? This cannot be undone.`)) return;
		try {
			await stackTemplates.delete(id);
			goto('/stack-templates');
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
{:else if tmpl}
<div class="max-w-3xl space-y-8 p-6">

	<!-- Header -->
	<div class="flex items-start justify-between gap-4">
		<div>
			<nav class="text-xs text-zinc-600 mb-1">
				<a href="/stack-templates" class="hover:text-zinc-400 transition-colors">Stack templates</a>
				<span class="mx-1">/</span>
				<span class="text-zinc-400">{tmpl.name}</span>
			</nav>
			<h1 class="text-lg font-semibold text-white">{tmpl.name}</h1>
			{#if tmpl.description}
				<p class="text-sm text-zinc-500 mt-0.5">{tmpl.description}</p>
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
				<button onclick={deleteTemplate}
					class="border border-red-900 hover:border-red-700 text-red-400 text-sm px-3 py-1.5 rounded-lg transition-colors">
					Delete
				</button>
			{/if}
		</div>
	</div>

	{#if editing}
		<section class="space-y-4 rounded-xl border border-zinc-800 p-5">
			<h2 class="text-sm font-medium text-zinc-300">Edit template</h2>
			{#if editError}
				<div class="rounded-lg border border-red-800 bg-red-950 px-4 py-3 text-sm text-red-300">{editError}</div>
			{/if}
			<form onsubmit={saveEdit} class="space-y-4">
				<div class="grid grid-cols-2 gap-4">
					<div class="space-y-1.5">
						<label class="field-label" for="t-name">Name</label>
						<input id="t-name" class="field-input" bind:value={form.name} required />
					</div>
					<div class="space-y-1.5">
						<label class="field-label" for="t-desc">Description</label>
						<input id="t-desc" class="field-input" bind:value={form.description} placeholder="Optional" />
					</div>
					<div class="space-y-1.5">
						<label class="field-label" for="t-tool">Tool</label>
						<select id="t-tool" class="field-input" bind:value={form.tool}>
							<option value="opentofu">OpenTofu</option>
							<option value="terraform">Terraform</option>
							<option value="ansible">Ansible</option>
							<option value="pulumi">Pulumi</option>
						</select>
					</div>
					<div class="space-y-1.5">
						<label class="field-label" for="t-toolv">Tool version</label>
						<input id="t-toolv" class="field-input" bind:value={form.tool_version} placeholder="e.g. 1.7.0" />
					</div>
					<div class="space-y-1.5">
						<label class="field-label" for="t-vcs">VCS provider</label>
						<select id="t-vcs" class="field-input" bind:value={form.vcs_provider}>
							<option value="github">GitHub</option>
							<option value="gitlab">GitLab</option>
							<option value="gitea">Gitea</option>
							<option value="gogs">Gogs</option>
						</select>
					</div>
					<div class="space-y-1.5">
						<label class="field-label" for="t-runner">Runner image</label>
						<input id="t-runner" class="field-input" bind:value={form.runner_image} placeholder="ghcr.io/org/runner:latest" />
					</div>
					<div class="space-y-1.5 col-span-2">
						<label class="field-label" for="t-repo">Repository URL</label>
						<input id="t-repo" class="field-input" bind:value={form.repo_url} placeholder="https://github.com/org/repo" />
					</div>
					<div class="space-y-1.5">
						<label class="field-label" for="t-branch">Branch</label>
						<input id="t-branch" class="field-input" bind:value={form.repo_branch} placeholder="main" />
					</div>
					<div class="space-y-1.5">
						<label class="field-label" for="t-root">Project root</label>
						<input id="t-root" class="field-input" bind:value={form.project_root} placeholder="." />
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
							<label class="field-label" for="t-sched">Drift schedule (cron)</label>
							<input id="t-sched" class="field-input" bind:value={form.drift_schedule} placeholder="0 */6 * * *" />
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
				<tbody class="divide-y divide-zinc-800">
					<tr>
						<td class="px-4 py-2.5 text-zinc-500 w-40">Tool</td>
						<td class="px-4 py-2.5 text-zinc-200">{tmpl.tool}{tmpl.tool_version ? ` ${tmpl.tool_version}` : ''}</td>
					</tr>
					<tr>
						<td class="px-4 py-2.5 text-zinc-500">VCS provider</td>
						<td class="px-4 py-2.5 text-zinc-200">{tmpl.vcs_provider}</td>
					</tr>
					<tr>
						<td class="px-4 py-2.5 text-zinc-500">Repository</td>
						<td class="px-4 py-2.5 text-zinc-200 font-mono text-xs">{tmpl.repo_url || '—'}</td>
					</tr>
					<tr>
						<td class="px-4 py-2.5 text-zinc-500">Branch</td>
						<td class="px-4 py-2.5 text-zinc-200">{tmpl.repo_branch}</td>
					</tr>
					<tr>
						<td class="px-4 py-2.5 text-zinc-500">Project root</td>
						<td class="px-4 py-2.5 text-zinc-200 font-mono text-xs">{tmpl.project_root}</td>
					</tr>
					{#if tmpl.runner_image}
						<tr>
							<td class="px-4 py-2.5 text-zinc-500">Runner image</td>
							<td class="px-4 py-2.5 text-zinc-200 font-mono text-xs">{tmpl.runner_image}</td>
						</tr>
					{/if}
					<tr>
						<td class="px-4 py-2.5 text-zinc-500">Auto-apply</td>
						<td class="px-4 py-2.5 text-zinc-200">{tmpl.auto_apply ? 'Yes' : 'No'}</td>
					</tr>
					<tr>
						<td class="px-4 py-2.5 text-zinc-500">Drift detection</td>
						<td class="px-4 py-2.5 text-zinc-200">
							{tmpl.drift_detection ? 'Yes' : 'No'}
							{#if tmpl.drift_detection && tmpl.drift_schedule}
								<span class="text-zinc-500 font-mono text-xs ml-2">{tmpl.drift_schedule}</span>
							{/if}
						</td>
					</tr>
					<tr>
						<td class="px-4 py-2.5 text-zinc-500">Auto-remediate</td>
						<td class="px-4 py-2.5 text-zinc-200">{tmpl.auto_remediate_drift ? 'Yes' : 'No'}</td>
					</tr>
					<tr>
						<td class="px-4 py-2.5 text-zinc-500">Updated</td>
						<td class="px-4 py-2.5 text-zinc-500 text-xs">{new Date(tmpl.updated_at).toLocaleString()}</td>
					</tr>
				</tbody>
			</table>
		</section>
	{/if}

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
