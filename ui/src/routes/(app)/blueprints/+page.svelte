<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { blueprints, type Blueprint } from '$lib/api/client';
	import { auth } from '$lib/stores/auth.svelte';

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
</script>

<div class="max-w-4xl space-y-6 p-6">
	<div class="flex items-center justify-between">
		<div>
			<h1 class="text-lg font-semibold text-white">Blueprints</h1>
			<p class="text-sm text-zinc-500 mt-0.5">Parameterized stack templates that app teams can self-serve deploy.</p>
		</div>
		{#if auth.isAdmin}
			<button
				onclick={() => (creating = !creating)}
				class="rounded-lg bg-indigo-600 px-3 py-1.5 text-sm text-white transition-colors hover:bg-indigo-500">
				{creating ? 'Cancel' : 'New blueprint'}
			</button>
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
						class="rounded-lg bg-indigo-600 px-4 py-1.5 text-sm text-white transition-colors hover:bg-indigo-500 disabled:opacity-50">
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
			<p class="text-zinc-400 text-sm font-medium">No blueprints yet</p>
			<p class="text-zinc-600 text-xs">Create a blueprint to let app teams deploy pre-configured stacks without touching IaC.</p>
		</div>
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
