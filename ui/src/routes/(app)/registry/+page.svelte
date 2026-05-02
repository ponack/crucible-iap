<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { registry, type RegistryModule } from '$lib/api/client';
	import { auth } from '$lib/stores/auth.svelte';
	import EmptyState from '$lib/components/EmptyState.svelte';

	let modules = $state<RegistryModule[]>([]);
	let loading = $state(true);
	let error = $state<string | null>(null);
	let searchQ = $state('');
	let searchTimer: ReturnType<typeof setTimeout>;

	// Publish form
	let publishing = $state(false);
	let showForm = $state(false);
	let saving = $state(false);
	let formError = $state<string | null>(null);
	let fileInput = $state<HTMLInputElement | null>(null);

	let form = $state({
		namespace: '',
		name: '',
		provider: 'aws',
		version: '',
		readme: ''
	});

	// Group modules by namespace/name/provider for display
	const grouped = $derived(() => {
		const map = new Map<string, { latest: RegistryModule; versions: RegistryModule[] }>();
		for (const m of modules) {
			const key = `${m.namespace}/${m.name}/${m.provider}`;
			const entry = map.get(key);
			if (!entry) {
				map.set(key, { latest: m, versions: [m] });
			} else {
				entry.versions.push(m);
			}
		}
		return [...map.values()];
	});

	onMount(() => load());

	async function load(q = '') {
		loading = true;
		error = null;
		try {
			modules = await registry.list(q || undefined);
		} catch (e) {
			error = (e as Error).message;
		} finally {
			loading = false;
		}
	}

	function onSearch() {
		clearTimeout(searchTimer);
		searchTimer = setTimeout(() => load(searchQ), 300);
	}

	async function publish(e: SubmitEvent) {
		e.preventDefault();
		if (!fileInput?.files?.length) {
			formError = 'Select a .tar.gz module archive';
			return;
		}
		saving = true;
		formError = null;
		try {
			const fd = new FormData();
			fd.append('namespace', form.namespace);
			fd.append('name', form.name);
			fd.append('provider', form.provider);
			fd.append('version', form.version);
			fd.append('readme', form.readme);
			fd.append('module', fileInput.files[0]);
			const m = await registry.publish(fd);
			showForm = false;
			goto(`/registry/${m.id}`);
		} catch (e) {
			formError = (e as Error).message;
		} finally {
			saving = false;
		}
	}
</script>

<div class="p-6 max-w-5xl mx-auto space-y-6">
	<div class="flex items-center justify-between">
		<div>
			<h1 class="text-xl font-semibold text-white">Module Registry</h1>
			<p class="text-sm text-zinc-400 mt-0.5">Private Terraform/OpenTofu modules backed by MinIO</p>
		</div>
		{#if auth.isAdmin}
			<button
				onclick={() => { showForm = !showForm; formError = null; }}
				class="px-3 py-1.5 bg-emerald-700 hover:bg-emerald-600 text-white text-sm rounded"
			>
				{showForm ? 'Cancel' : 'Publish module'}
			</button>
		{/if}
	</div>

	{#if showForm}
		<div class="bg-zinc-900 border border-zinc-700 rounded-lg p-5 space-y-4">
			<h2 class="text-sm font-medium text-white">Publish a new version</h2>
			<form onsubmit={publish} class="space-y-4">
				<div class="grid grid-cols-2 gap-4">
					<div>
						<label for="ns" class="block text-xs text-zinc-400 mb-1">Namespace</label>
						<input id="ns" bind:value={form.namespace} required placeholder="myorg"
							class="w-full bg-zinc-800 border border-zinc-700 rounded px-3 py-1.5 text-sm text-white" />
					</div>
					<div>
						<label for="name" class="block text-xs text-zinc-400 mb-1">Name</label>
						<input id="name" bind:value={form.name} required placeholder="vpc"
							class="w-full bg-zinc-800 border border-zinc-700 rounded px-3 py-1.5 text-sm text-white" />
					</div>
					<div>
						<label for="provider" class="block text-xs text-zinc-400 mb-1">Provider</label>
						<input id="provider" bind:value={form.provider} required placeholder="aws"
							class="w-full bg-zinc-800 border border-zinc-700 rounded px-3 py-1.5 text-sm text-white" />
					</div>
					<div>
						<label for="version" class="block text-xs text-zinc-400 mb-1">Version</label>
						<input id="version" bind:value={form.version} required placeholder="1.0.0"
							class="w-full bg-zinc-800 border border-zinc-700 rounded px-3 py-1.5 text-sm text-white" />
					</div>
				</div>
				<div>
					<label for="readme" class="block text-xs text-zinc-400 mb-1">README (optional)</label>
					<textarea id="readme" bind:value={form.readme} rows={4} placeholder="Markdown description…"
						class="w-full bg-zinc-800 border border-zinc-700 rounded px-3 py-1.5 text-sm text-white font-mono"></textarea>
				</div>
				<div>
					<label for="archive" class="block text-xs text-zinc-400 mb-1">Module archive (.tar.gz)</label>
					<input id="archive" type="file" accept=".tar.gz,.tgz" bind:this={fileInput} required
						class="text-sm text-zinc-300" />
				</div>
				{#if formError}
					<p class="text-red-400 text-sm">{formError}</p>
				{/if}
				<div class="flex gap-3">
					<button type="submit" disabled={saving}
						class="px-4 py-1.5 bg-emerald-700 hover:bg-emerald-600 disabled:opacity-50 text-white text-sm rounded">
						{saving ? 'Publishing…' : 'Publish'}
					</button>
				</div>
			</form>
		</div>
	{/if}

	<!-- Search -->
	<input
		bind:value={searchQ}
		oninput={onSearch}
		placeholder="Search modules…"
		class="w-full bg-zinc-800 border border-zinc-700 rounded px-3 py-2 text-sm text-white placeholder:text-zinc-500"
	/>

	<!-- Usage snippet -->
	<details class="bg-zinc-900 border border-zinc-700 rounded-lg">
		<summary class="px-4 py-3 text-sm text-zinc-300 cursor-pointer select-none">Terraform credentials setup</summary>
		<div class="px-4 pb-4 space-y-2">
			<p class="text-xs text-zinc-400">Add to <code class="text-zinc-300">~/.terraformrc</code> or <code class="text-zinc-300">terraform.rc</code> on Windows:</p>
			<pre class="bg-zinc-800 rounded p-3 text-xs text-zinc-200 overflow-x-auto">credentials "{window?.location?.hostname ?? 'crucible.example.com'}" &#123;
  token = "ciap_your_service_account_token"
&#125;</pre>
			<p class="text-xs text-zinc-500">Create a service account token in <a href="/settings/api-tokens" class="text-emerald-400 hover:underline">Settings → API Tokens</a>.</p>
		</div>
	</details>

	{#if loading}
		<p class="text-zinc-500 text-sm">Loading…</p>
	{:else if error}
		<p class="text-red-400 text-sm">{error}</p>
	{:else if grouped().length === 0}
		<EmptyState
			icon="M21 7.5l-9-5.25L3 7.5m18 0-9 5.25m9-5.25v9l-9 5.25M3 7.5l9 5.25M3 7.5v9l9 5.25m0-9v9"
			heading="No modules yet"
			sub="Publish Terraform modules to make them available for all stacks via the built-in registry."
		/>
	{:else}
		<div class="space-y-3">
			{#each grouped() as group}
				{@const m = group.latest}
				<a href={`/registry/${m.id}`}
					class="block bg-zinc-900 border border-zinc-800 hover:border-zinc-600 rounded-lg p-4 transition-colors">
					<div class="flex items-start justify-between gap-4">
						<div class="min-w-0">
							<div class="flex items-center gap-2 flex-wrap">
								<span class="text-white font-medium">{m.namespace}/{m.name}</span>
								<span class="px-1.5 py-0.5 bg-zinc-700 text-zinc-300 text-xs rounded">{m.provider}</span>
								<span class="px-1.5 py-0.5 bg-emerald-900/50 text-emerald-400 text-xs rounded font-mono">v{m.version}</span>
								{#if group.versions.length > 1}
									<span class="text-zinc-500 text-xs">{group.versions.length} versions</span>
								{/if}
							</div>
							{#if m.readme}
								<p class="text-sm text-zinc-400 mt-1 line-clamp-2">{m.readme.slice(0, 120)}</p>
							{/if}
						</div>
						<div class="text-right text-xs text-zinc-500 flex-shrink-0">
							<div>Published {new Date(m.published_at).toLocaleDateString()}</div>
							{#if m.published_by}
								<div class="mt-0.5 truncate max-w-32">{m.published_by}</div>
							{/if}
						</div>
					</div>
					<div class="mt-3 font-mono text-xs text-zinc-400 bg-zinc-800 rounded px-3 py-1.5">
						source = "{window?.location?.hostname ?? 'crucible.example.com'}/{m.namespace}/{m.name}/{m.provider}"
					</div>
				</a>
			{/each}
		</div>
	{/if}
</div>
