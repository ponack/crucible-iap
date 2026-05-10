<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { blueprints, type Blueprint, type BlueprintExport } from '$lib/api/client';
	import { auth } from '$lib/stores/auth.svelte';
	import EmptyState from '$lib/components/EmptyState.svelte';

	let items = $state<Blueprint[]>([]);
	let loading = $state(true);
	let error = $state<string | null>(null);

	let creating = $state(false);
	let saving = $state(false);
	let formError = $state<string | null>(null);
	let form = $state({
		name: '',
		description: '',
		tool: 'opentofu',
		repo_url: '',
		repo_branch: 'main',
		project_root: '.',
		vcs_provider: 'github'
	});

	let importing = $state(false);
	let importSaving = $state(false);
	let importError = $state<string | null>(null);
	let importJSON = $state('');

	onMount(async () => {
		try {
			items = await blueprints.list();
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
			const b = await blueprints.create(form);
			goto(`/blueprints/${b.id}`);
		} catch (e) {
			formError = (e as Error).message;
			saving = false;
		}
	}

	function handleImportFile(e: Event) {
		const file = (e.target as HTMLInputElement).files?.[0];
		if (!file) return;
		const reader = new FileReader();
		reader.onload = (ev) => { importJSON = (ev.target?.result as string) ?? ''; };
		reader.readAsText(file);
	}

	async function importBlueprint(e: SubmitEvent) {
		e.preventDefault();
		importSaving = true;
		importError = null;
		try {
			const data: BlueprintExport = JSON.parse(importJSON);
			const b = await blueprints.importBlueprint(data);
			goto(`/blueprints/${b.id}`);
		} catch (e) {
			importError = e instanceof SyntaxError ? 'Invalid JSON' : (e as Error).message;
			importSaving = false;
		}
	}
</script>

<div class="p-6 space-y-6">
	<div class="flex items-center justify-between">
		<div>
			<h1 class="text-lg font-semibold text-white">Blueprints</h1>
			<p class="text-sm text-zinc-500 mt-0.5">Parameterized stack templates that app teams can self-serve deploy.</p>
		</div>
		{#if auth.isAdmin}
			<div class="flex gap-2">
				<button
					onclick={() => { importing = !importing; creating = false; }}
					class="rounded-lg border border-zinc-700 px-3 py-1.5 text-sm text-zinc-300 transition-colors hover:bg-zinc-800">
					{importing ? 'Cancel' : 'Import'}
				</button>
				<button
					onclick={() => { creating = !creating; importing = false; }}
					class="rounded-lg bg-teal-600 px-3 py-1.5 text-sm text-white transition-colors hover:bg-teal-500">
					{creating ? 'Cancel' : 'New blueprint'}
				</button>
			</div>
		{/if}
	</div>

	{#if creating}
		<div class="space-y-4 rounded-xl border border-zinc-800 p-5">
			<h2 class="text-sm font-medium text-zinc-300">New blueprint</h2>
			{#if formError}
				<div class="rounded-lg border border-red-800 bg-red-950 px-4 py-3 text-sm text-red-300">{formError}</div>
			{/if}
			<form onsubmit={create} class="space-y-4">
				<div class="grid grid-cols-2 gap-4">
					<div class="space-y-1.5">
						<label class="field-label" for="b-name">Name <span class="text-red-400">*</span></label>
						<input id="b-name" class="field-input" bind:value={form.name} required placeholder="e.g. aws-app-stack" />
					</div>
					<div class="space-y-1.5">
						<label class="field-label" for="b-desc">Description</label>
						<input id="b-desc" class="field-input" bind:value={form.description} placeholder="Optional description" />
					</div>
					<div class="space-y-1.5">
						<label class="field-label" for="b-tool">Tool</label>
						<select id="b-tool" class="field-input" bind:value={form.tool}>
							<option value="opentofu">OpenTofu</option>
							<option value="terraform">Terraform</option>
							<option value="ansible">Ansible</option>
							<option value="pulumi">Pulumi</option>
							<option value="terragrunt">Terragrunt</option>
						</select>
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
						<label class="field-label" for="b-repo">Repository URL</label>
						<input id="b-repo" class="field-input" bind:value={form.repo_url} placeholder="https://github.com/org/repo" />
					</div>
					<div class="space-y-1.5">
						<label class="field-label" for="b-branch">Branch</label>
						<input id="b-branch" class="field-input" bind:value={form.repo_branch} placeholder="main" />
					</div>
					<div class="space-y-1.5 col-span-2">
						<label class="field-label" for="b-root">Project root</label>
						<input id="b-root" class="field-input" bind:value={form.project_root} placeholder="." />
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

	{#if importing}
		<div class="space-y-4 rounded-xl border border-zinc-800 p-5">
			<h2 class="text-sm font-medium text-zinc-300">Import blueprint</h2>
			{#if importError}
				<div class="rounded-lg border border-red-800 bg-red-950 px-4 py-3 text-sm text-red-300">{importError}</div>
			{/if}
			<form onsubmit={importBlueprint} class="space-y-4">
				<div class="space-y-1.5">
					<label class="field-label" for="imp-file">Upload JSON file</label>
					<input id="imp-file" type="file" accept=".json,application/json" onchange={handleImportFile}
						class="block w-full text-sm text-zinc-400 file:mr-3 file:rounded-lg file:border-0 file:bg-zinc-800 file:px-3 file:py-1.5 file:text-sm file:text-zinc-300 hover:file:bg-zinc-700" />
				</div>
				<div class="space-y-1.5">
					<label class="field-label" for="imp-json">Or paste JSON</label>
					<textarea id="imp-json" class="field-input font-mono text-xs" rows="6" bind:value={importJSON} placeholder="Paste exported blueprint JSON here…"></textarea>
				</div>
				<div class="flex justify-end">
					<button type="submit" disabled={importSaving || !importJSON.trim()}
						class="rounded-lg bg-teal-600 px-4 py-1.5 text-sm text-white transition-colors hover:bg-teal-500 disabled:opacity-50">
						{importSaving ? 'Importing…' : 'Import'}
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
		<EmptyState
			icon="M9 6.75V15m6-6v8.25m.503 3.498 4.875-2.437c.381-.19.622-.58.622-1.006V4.82c0-.836-.88-1.38-1.628-1.006l-3.869 1.934c-.317.159-.69.159-1.006 0L9.503 3.252a1.125 1.125 0 0 0-1.006 0L3.622 5.689C3.24 5.88 3 6.695V19.18c0 .836.88 1.38 1.628 1.006l3.869-1.934c.317-.159.69-.159 1.006 0l4.994 2.497c.317.158.69.158 1.006 0Z"
			heading="No blueprints yet"
			sub="Blueprints let app teams self-serve new stacks by filling in a form — no Terraform knowledge required."
		/>
	{:else}
		<div class="rounded-xl border border-zinc-800 overflow-hidden">
			<table class="w-full text-sm">
				<thead class="bg-zinc-900 text-zinc-500 text-xs uppercase tracking-wide">
					<tr>
						<th class="text-left px-4 py-2">Name</th>
						<th class="text-left px-4 py-2">Tool</th>
						<th class="text-left px-4 py-2">Params</th>
						<th class="text-left px-4 py-2">Status</th>
						<th class="text-left px-4 py-2">Updated</th>
					</tr>
				</thead>
				<tbody class="divide-y divide-zinc-700">
					{#each items as b (b.id)}
						<tr class="hover:bg-zinc-900/50 transition-colors cursor-pointer" onclick={() => goto(`/blueprints/${b.id}`)}>
							<td class="px-4 py-3 text-zinc-200 font-medium">
								{b.name}
								{#if b.description}
									<span class="block text-xs text-zinc-500 font-normal">{b.description}</span>
								{/if}
							</td>
							<td class="px-4 py-3 text-zinc-400">{b.tool}</td>
							<td class="px-4 py-3 text-zinc-400">{b.params?.length ?? 0}</td>
							<td class="px-4 py-3">
								{#if b.is_published}
									<span class="inline-flex items-center rounded-full bg-emerald-950 px-2 py-0.5 text-xs text-emerald-400">Published</span>
								{:else}
									<span class="inline-flex items-center rounded-full bg-zinc-800 px-2 py-0.5 text-xs text-zinc-500">Draft</span>
								{/if}
							</td>
							<td class="px-4 py-3 text-zinc-500 text-xs">{new Date(b.updated_at).toLocaleString()}</td>
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
