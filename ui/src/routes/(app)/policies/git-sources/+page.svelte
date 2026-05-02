<script lang="ts">
	import { onMount } from 'svelte';
	import { policyGit, type PolicyGitSource, type Integration } from '$lib/api/client';
	import { auth } from '$lib/stores/auth.svelte';
	import { integrations } from '$lib/api/client';

	let items = $state<PolicyGitSource[]>([]);
	let intgs = $state<Integration[]>([]);
	let loading = $state(true);
	let error = $state<string | null>(null);

	let creating = $state(false);
	let saving = $state(false);
	let formError = $state<string | null>(null);
	let syncing = $state<string | null>(null);
	let syncMsg = $state<string | null>(null);

	let form = $state({
		name: '',
		repo_url: '',
		branch: 'main',
		path: '.',
		vcs_integration_id: '',
		mirror_mode: false
	});

	onMount(async () => {
		try {
			[items, intgs] = await Promise.all([policyGit.list(), integrations.list()]);
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
			const body = {
				name: form.name,
				repo_url: form.repo_url,
				branch: form.branch || 'main',
				path: form.path || '.',
				mirror_mode: form.mirror_mode,
				...(form.vcs_integration_id ? { vcs_integration_id: form.vcs_integration_id } : {})
			};
			const s = await policyGit.create(body);
			items = [...items, s];
			creating = false;
			form = { name: '', repo_url: '', branch: 'main', path: '.', vcs_integration_id: '', mirror_mode: false };
		} catch (e) {
			formError = (e as Error).message;
		} finally {
			saving = false;
		}
	}

	async function triggerSync(id: string) {
		syncing = id;
		syncMsg = null;
		try {
			await policyGit.sync(id);
			syncMsg = 'Sync queued.';
			setTimeout(() => (syncMsg = null), 3000);
		} catch (e) {
			syncMsg = (e as Error).message;
		} finally {
			syncing = null;
		}
	}

	async function remove(id: string) {
		if (!confirm('Delete this git source? Synced policies will not be removed automatically.')) return;
		try {
			await policyGit.delete(id);
			items = items.filter((s) => s.id !== id);
		} catch (e) {
			error = (e as Error).message;
		}
	}

	function webhookURL(id: string) {
		return window.location.origin + '/api/v1/policy-git-webhooks/' + id;
	}

	function copyWebhook(id: string) {
		navigator.clipboard.writeText(webhookURL(id));
	}
</script>

<div class="max-w-3xl space-y-6 p-6">
	<div class="flex items-center justify-between">
		<div>
			<h1 class="text-lg font-semibold text-white">Policy Git Sources</h1>
			<p class="mt-0.5 text-xs text-zinc-500">
				Sync <code class="font-mono">.rego</code> files from a git repository into policies automatically.
			</p>
		</div>
		<div class="flex items-center gap-3">
			<a
				href="/policies"
				class="rounded-lg border border-zinc-700 px-3 py-1.5 text-sm text-zinc-300 transition-colors hover:border-zinc-500 hover:text-white"
			>
				All policies
			</a>
			{#if auth.isAdmin}
				<button
					onclick={() => (creating = !creating)}
					class="rounded-lg bg-indigo-600 px-3 py-1.5 text-sm text-white transition-colors hover:bg-indigo-500"
				>
					{creating ? 'Cancel' : 'Add source'}
				</button>
			{/if}
		</div>
	</div>

	{#if syncMsg}
		<div class="rounded-lg border border-zinc-700 bg-zinc-900 px-4 py-2 text-sm text-zinc-300">
			{syncMsg}
		</div>
	{/if}

	{#if creating}
		<div class="space-y-4 rounded-xl border border-zinc-800 p-5">
			<h2 class="text-sm font-medium text-zinc-300">New git source</h2>
			{#if formError}
				<div class="rounded-lg border border-red-800 bg-red-950 px-4 py-3 text-sm text-red-300">
					{formError}
				</div>
			{/if}
			<form onsubmit={create} class="space-y-4">
				<div class="grid grid-cols-2 gap-4">
					<div class="space-y-1.5">
						<label class="field-label" for="gs-name">Name</label>
						<input
							id="gs-name"
							class="field-input"
							bind:value={form.name}
							required
							placeholder="e.g. org-policies"
						/>
					</div>
					<div class="space-y-1.5">
						<label class="field-label" for="gs-branch">Branch</label>
						<input id="gs-branch" class="field-input" bind:value={form.branch} placeholder="main" />
					</div>
				</div>

				<div class="space-y-1.5">
					<label class="field-label" for="gs-url">Repository URL</label>
					<input
						id="gs-url"
						class="field-input"
						bind:value={form.repo_url}
						required
						placeholder="https://github.com/org/policies.git"
					/>
				</div>

				<div class="grid grid-cols-2 gap-4">
					<div class="space-y-1.5">
						<label class="field-label" for="gs-path">Path (within repo)</label>
						<input id="gs-path" class="field-input" bind:value={form.path} placeholder="." />
					</div>
					<div class="space-y-1.5">
						<label class="field-label" for="gs-intg">VCS Integration (optional)</label>
						<select id="gs-intg" class="field-input" bind:value={form.vcs_integration_id}>
							<option value="">None (public repo)</option>
							{#each intgs as intg}
								<option value={intg.id}>{intg.name}</option>
							{/each}
						</select>
					</div>
				</div>

				<label class="flex cursor-pointer items-center gap-2 text-sm text-zinc-300">
					<input type="checkbox" bind:checked={form.mirror_mode} />
					Mirror mode — delete policies removed from the repo
				</label>

				<div class="flex justify-end">
					<button
						type="submit"
						disabled={saving}
						class="rounded-lg bg-indigo-600 px-4 py-1.5 text-sm text-white transition-colors hover:bg-indigo-500 disabled:opacity-50"
					>
						{saving ? 'Creating…' : 'Create'}
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
			<p class="text-sm text-zinc-400">No git sources yet.</p>
			<p class="text-xs text-zinc-600">
				Add a source to sync <code class="font-mono">.rego</code> files from a git repository.
			</p>
		</div>
	{:else}
		<div class="space-y-3">
			{#each items as src (src.id)}
				<div class="rounded-xl border border-zinc-800 p-4 space-y-3">
					<div class="flex items-start justify-between gap-3">
						<div class="min-w-0">
							<p class="font-medium text-zinc-200">{src.name}</p>
							<p class="mt-0.5 truncate font-mono text-xs text-zinc-500">{src.repo_url}</p>
							<p class="mt-1 text-xs text-zinc-500">
								Branch: <span class="text-zinc-400">{src.branch}</span>
								{#if src.path && src.path !== '.'}
									· Path: <span class="font-mono text-zinc-400">{src.path}</span>
								{/if}
								{#if src.mirror_mode}
									· <span class="text-amber-400">mirror</span>
								{/if}
							</p>
						</div>
						<div class="flex shrink-0 items-center gap-2">
							<button
								onclick={() => triggerSync(src.id)}
								disabled={syncing === src.id}
								class="rounded border border-zinc-700 px-2.5 py-1 text-xs text-zinc-300 transition-colors hover:border-zinc-500 hover:text-white disabled:opacity-40"
							>
								{syncing === src.id ? 'Queuing…' : 'Sync now'}
							</button>
							{#if auth.isAdmin}
								<button
									onclick={() => remove(src.id)}
									class="rounded border border-zinc-700 px-2.5 py-1 text-xs text-red-400 transition-colors hover:border-red-700 hover:text-red-300"
								>
									Delete
								</button>
							{/if}
						</div>
					</div>

					<div class="flex items-center gap-2 rounded-lg border border-zinc-800 bg-zinc-900/50 px-3 py-2">
						<span class="text-xs text-zinc-500">Webhook URL:</span>
						<code class="flex-1 truncate font-mono text-xs text-zinc-400">{webhookURL(src.id)}</code>
						<button
							onclick={() => copyWebhook(src.id)}
							class="text-xs text-zinc-500 transition-colors hover:text-zinc-300"
						>
							Copy
						</button>
						{#if src.webhook_secret}
							<span class="ml-2 text-xs text-zinc-600">
								Secret: <code class="font-mono text-zinc-500">{src.webhook_secret}</code>
							</span>
						{/if}
					</div>

					{#if src.last_sync_error}
						<p class="rounded-lg border border-red-900 bg-red-950/50 px-3 py-2 text-xs text-red-400">
							Last error: {src.last_sync_error}
						</p>
					{:else if src.last_synced_at}
						<p class="text-xs text-zinc-600">
							Last synced: {new Date(src.last_synced_at).toLocaleString()}
							{#if src.last_sync_sha && src.last_sync_sha !== 'HEAD'}
								· <code class="font-mono">{src.last_sync_sha.slice(0, 8)}</code>
							{/if}
						</p>
					{/if}
				</div>
			{/each}
		</div>
	{/if}
</div>
