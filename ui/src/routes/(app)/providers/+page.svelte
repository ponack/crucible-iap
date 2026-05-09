<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { providers, type RegistryProvider, type ProviderGPGKey } from '$lib/api/client';
	import { auth } from '$lib/stores/auth.svelte';
	import EmptyState from '$lib/components/EmptyState.svelte';

	let providerList = $state<RegistryProvider[]>([]);
	let gpgKeys = $state<ProviderGPGKey[]>([]);
	let loading = $state(true);
	let error = $state<string | null>(null);
	let searchQ = $state('');
	let searchTimer: ReturnType<typeof setTimeout>;

	let showPublishForm = $state(false);
	let showGPGForm = $state(false);
	let saving = $state(false);
	let formError = $state<string | null>(null);
	let fileInput = $state<HTMLInputElement | null>(null);

	let form = $state({ namespace: '', type: '', version: '', os: 'linux', arch: 'amd64', readme: '' });
	let gpgForm = $state({ namespace: '', key_id: '', ascii_armor: '' });

	const grouped = $derived(() => {
		const map = new Map<string, { latest: RegistryProvider; versions: RegistryProvider[] }>();
		for (const p of providerList) {
			const key = `${p.namespace}/${p.type}`;
			const entry = map.get(key);
			if (!entry) {
				map.set(key, { latest: p, versions: [p] });
			} else {
				entry.versions.push(p);
			}
		}
		return [...map.values()];
	});

	onMount(() => {
		load();
		if (auth.isAdmin) providers.listGPGKeys().then((k) => (gpgKeys = k)).catch((e) => console.error('listGPGKeys', e));
	});

	async function load(q = '') {
		loading = true;
		error = null;
		try {
			providerList = await providers.list(q || undefined);
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
			formError = 'Select a provider .zip binary';
			return;
		}
		saving = true;
		formError = null;
		try {
			const fd = new FormData();
			fd.append('namespace', form.namespace);
			fd.append('type', form.type);
			fd.append('version', form.version);
			fd.append('os', form.os);
			fd.append('arch', form.arch);
			fd.append('readme', form.readme);
			fd.append('provider', fileInput.files[0]);
			const p = await providers.publish(fd);
			showPublishForm = false;
			goto(`/providers/${p.id}`);
		} catch (e) {
			formError = (e as Error).message;
		} finally {
			saving = false;
		}
	}

	async function addGPGKey(e: SubmitEvent) {
		e.preventDefault();
		saving = true;
		formError = null;
		try {
			const k = await providers.addGPGKey(gpgForm);
			gpgKeys = [...gpgKeys, k];
			showGPGForm = false;
			gpgForm = { namespace: '', key_id: '', ascii_armor: '' };
		} catch (e) {
			formError = (e as Error).message;
		} finally {
			saving = false;
		}
	}

	async function deleteGPGKey(id: string) {
		if (!confirm('Remove this GPG key?')) return;
		await providers.deleteGPGKey(id);
		gpgKeys = gpgKeys.filter((k) => k.id !== id);
	}
</script>

<div class="p-6 space-y-6">
	<div class="flex items-center justify-between">
		<div>
			<h1 class="text-xl font-semibold text-white">Provider Registry</h1>
			<p class="text-sm text-zinc-400 mt-0.5">Private Terraform/OpenTofu providers backed by MinIO</p>
		</div>
		{#if auth.isAdmin}
			<div class="flex gap-2">
				<button
					onclick={() => { showGPGForm = !showGPGForm; showPublishForm = false; formError = null; }}
					class="px-3 py-1.5 border border-zinc-600 hover:border-zinc-400 text-zinc-300 text-sm rounded">
					{showGPGForm ? 'Cancel' : 'GPG keys'}
				</button>
				<button
					onclick={() => { showPublishForm = !showPublishForm; showGPGForm = false; formError = null; }}
					class="px-3 py-1.5 bg-emerald-700 hover:bg-emerald-600 text-white text-sm rounded">
					{showPublishForm ? 'Cancel' : 'Publish provider'}
				</button>
			</div>
		{/if}
	</div>

	{#if showPublishForm}
		<div class="bg-zinc-900 border border-zinc-700 rounded-lg p-5 space-y-4">
			<h2 class="text-sm font-medium text-white">Publish a provider binary</h2>
			<form onsubmit={publish} class="space-y-4">
				<div class="grid grid-cols-2 gap-4">
					<div>
						<label for="ns" class="block text-xs text-zinc-400 mb-1">Namespace</label>
						<input id="ns" bind:value={form.namespace} required placeholder="myorg"
							class="w-full bg-zinc-800 border border-zinc-700 rounded px-3 py-1.5 text-sm text-white" />
					</div>
					<div>
						<label for="ptype" class="block text-xs text-zinc-400 mb-1">Type</label>
						<input id="ptype" bind:value={form.type} required placeholder="myprovider"
							class="w-full bg-zinc-800 border border-zinc-700 rounded px-3 py-1.5 text-sm text-white" />
					</div>
					<div>
						<label for="version" class="block text-xs text-zinc-400 mb-1">Version</label>
						<input id="version" bind:value={form.version} required placeholder="1.0.0"
							class="w-full bg-zinc-800 border border-zinc-700 rounded px-3 py-1.5 text-sm text-white" />
					</div>
					<div class="grid grid-cols-2 gap-2">
						<div>
							<label for="os" class="block text-xs text-zinc-400 mb-1">OS</label>
							<input id="os" bind:value={form.os} required placeholder="linux"
								class="w-full bg-zinc-800 border border-zinc-700 rounded px-3 py-1.5 text-sm text-white" />
						</div>
						<div>
							<label for="arch" class="block text-xs text-zinc-400 mb-1">Arch</label>
							<input id="arch" bind:value={form.arch} required placeholder="amd64"
								class="w-full bg-zinc-800 border border-zinc-700 rounded px-3 py-1.5 text-sm text-white" />
						</div>
					</div>
				</div>
				<div>
					<label for="readme" class="block text-xs text-zinc-400 mb-1">README (optional)</label>
					<textarea id="readme" bind:value={form.readme} rows={3} placeholder="Markdown description…"
						class="w-full bg-zinc-800 border border-zinc-700 rounded px-3 py-1.5 text-sm text-white font-mono"></textarea>
				</div>
				<div>
					<label for="archive" class="block text-xs text-zinc-400 mb-1">Provider binary (.zip)</label>
					<input id="archive" type="file" accept=".zip" bind:this={fileInput} required
						class="text-sm text-zinc-300" />
				</div>
				{#if formError}
					<p class="text-red-400 text-sm">{formError}</p>
				{/if}
				<button type="submit" disabled={saving}
					class="px-4 py-1.5 bg-emerald-700 hover:bg-emerald-600 disabled:opacity-50 text-white text-sm rounded">
					{saving ? 'Publishing…' : 'Publish'}
				</button>
			</form>
		</div>
	{/if}

	{#if showGPGForm}
		<div class="bg-zinc-900 border border-zinc-700 rounded-lg p-5 space-y-4">
			<h2 class="text-sm font-medium text-white">GPG signing keys</h2>
			<p class="text-xs text-zinc-500">
				Public keys are served in the <code class="text-zinc-300">signing_keys</code> field of the
				provider download response. Required for <code class="text-zinc-300">terraform providers lock</code>.
			</p>

			{#if gpgKeys.length > 0}
				<div class="rounded-lg border border-zinc-800 overflow-hidden">
					<table class="w-full text-sm">
						<thead class="bg-zinc-900 text-zinc-500 text-xs uppercase">
							<tr>
								<th class="text-left px-4 py-2">Namespace</th>
								<th class="text-left px-4 py-2">Key ID</th>
								<th class="text-left px-4 py-2">Added by</th>
								<th class="px-4 py-2"></th>
							</tr>
						</thead>
						<tbody class="divide-y divide-zinc-800">
							{#each gpgKeys as key}
								<tr>
									<td class="px-4 py-2.5 text-zinc-300 font-mono text-xs">{key.namespace}</td>
									<td class="px-4 py-2.5 text-zinc-300 font-mono text-xs">{key.key_id}</td>
									<td class="px-4 py-2.5 text-zinc-500 text-xs">{key.created_by ?? '—'}</td>
									<td class="px-4 py-2.5 text-right">
										<button onclick={() => deleteGPGKey(key.id)}
											class="text-xs text-red-400 hover:text-red-300">Remove</button>
									</td>
								</tr>
							{/each}
						</tbody>
					</table>
				</div>
			{/if}

			<form onsubmit={addGPGKey} class="space-y-3">
				<div class="grid grid-cols-2 gap-4">
					<div>
						<label for="gpg-ns" class="block text-xs text-zinc-400 mb-1">Namespace</label>
						<input id="gpg-ns" bind:value={gpgForm.namespace} required placeholder="myorg"
							class="w-full bg-zinc-800 border border-zinc-700 rounded px-3 py-1.5 text-sm text-white" />
					</div>
					<div>
						<label for="gpg-kid" class="block text-xs text-zinc-400 mb-1">Key ID</label>
						<input id="gpg-kid" bind:value={gpgForm.key_id} required placeholder="ABC12345"
							class="w-full bg-zinc-800 border border-zinc-700 rounded px-3 py-1.5 text-sm text-white font-mono" />
					</div>
				</div>
				<div>
					<label for="gpg-armor" class="block text-xs text-zinc-400 mb-1">ASCII-armored public key</label>
					<textarea id="gpg-armor" bind:value={gpgForm.ascii_armor} rows={6} required
						placeholder="-----BEGIN PGP PUBLIC KEY BLOCK-----&#10;…"
						class="w-full bg-zinc-800 border border-zinc-700 rounded px-3 py-1.5 text-xs text-zinc-200 font-mono"></textarea>
				</div>
				{#if formError}
					<p class="text-red-400 text-sm">{formError}</p>
				{/if}
				<button type="submit" disabled={saving}
					class="px-4 py-1.5 bg-teal-600 hover:bg-teal-500 disabled:opacity-50 text-white text-sm rounded">
					{saving ? 'Saving…' : 'Add key'}
				</button>
			</form>
		</div>
	{/if}

	<!-- Search -->
	<input
		bind:value={searchQ}
		oninput={onSearch}
		placeholder="Search providers…"
		class="w-full bg-zinc-800 border border-zinc-700 rounded px-3 py-2 text-sm text-white placeholder:text-zinc-500"
	/>

	<!-- Usage snippet -->
	<details class="bg-zinc-900 border border-zinc-700 rounded-lg">
		<summary class="px-4 py-3 text-sm text-zinc-300 cursor-pointer select-none">Terraform configuration</summary>
		<div class="px-4 pb-4 space-y-3">
			<div>
				<p class="text-xs text-zinc-400 mb-1">Add to <code class="text-zinc-300">~/.terraformrc</code>:</p>
				<pre class="bg-zinc-800 rounded p-3 text-xs text-zinc-200 overflow-x-auto">credentials "{window?.location?.hostname ?? 'crucible.example.com'}" &#123;
  token = "ciap_your_service_account_token"
&#125;</pre>
			</div>
			<div>
				<p class="text-xs text-zinc-400 mb-1">Reference in your Terraform configuration:</p>
				<pre class="bg-zinc-800 rounded p-3 text-xs text-zinc-200 overflow-x-auto">terraform &#123;
  required_providers &#123;
    myprovider = &#123;
      source  = "{window?.location?.hostname ?? 'crucible.example.com'}/myorg/myprovider"
      version = "~> 1.0"
    &#125;
  &#125;
&#125;</pre>
			</div>
			<p class="text-xs text-zinc-500">Create a service account token in <a href="/settings/api-tokens" class="text-emerald-400 hover:underline">Settings → API Tokens</a>.</p>
		</div>
	</details>

	{#if loading}
		<p class="text-zinc-500 text-sm">Loading…</p>
	{:else if error}
		<p class="text-red-400 text-sm">{error}</p>
	{:else if grouped().length === 0}
		<EmptyState
			icon="M14.25 6.087c0-.355.186-.676.401-.959.221-.29.349-.634.349-1.003 0-1.036-1.007-1.875-2.25-1.875s-2.25.84-2.25 1.875c0 .369.128.713.349 1.003.215.283.401.604.401.959v0a.64.64 0 0 1-.657.643 48.39 48.39 0 0 1-4.163-.3c.186 1.613.293 3.25.315 4.907a.656.656 0 0 1-.658.663v0c-.355 0-.676-.186-.959-.401a1.647 1.647 0 0 0-1.003-.349c-1.035 0-1.875 1.007-1.875 2.25s.84 2.25 1.875 2.25c.369 0 .713-.128 1.003-.349.283-.215.604-.401.959-.401v0c.31 0 .555.26.532.57a48.039 48.039 0 0 1-.642 5.056c1.518.19 3.058.309 4.616.354a.64.64 0 0 0 .657-.643v0c0-.355-.186-.676-.401-.959a1.647 1.647 0 0 1-.349-1.003c0-1.035 1.007-1.875 2.25-1.875 1.243 0 2.25.84 2.25 1.875 0 .369-.128.713-.349 1.003-.215.283-.401.604-.401.959v0c0 .333.277.599.61.58a48.1 48.1 0 0 0 5.427-.63 48.05 48.05 0 0 0 .582-4.717.532.532 0 0 0-.533-.57v0c-.355 0-.676.186-.959.401-.29.221-.634.349-1.003.349-1.035 0-1.875-1.007-1.875-2.25s.84-2.25 1.875-2.25c.37 0 .713.128 1.003.349.283.215.604.401.959.401v0a.656.656 0 0 0 .658-.663 48.422 48.422 0 0 0-.37-5.36c-1.886.342-3.81.574-5.766.689a.578.578 0 0 1-.61-.58v0Z"
			heading="No providers yet"
			sub="Host custom or internal Terraform providers for air-gapped deployments. Binaries are stored in MinIO and served via the registry protocol."
		/>
	{:else}
		<div class="space-y-3">
			{#each grouped() as group}
				{@const p = group.latest}
				<a href={`/providers/${p.id}`}
					class="block bg-zinc-900 border border-zinc-800 hover:border-zinc-600 rounded-lg p-4 transition-colors">
					<div class="flex items-start justify-between gap-4">
						<div class="min-w-0">
							<div class="flex items-center gap-2 flex-wrap">
								<span class="text-white font-medium">{p.namespace}/{p.type}</span>
								<span class="px-1.5 py-0.5 bg-emerald-900/50 text-emerald-400 text-xs rounded font-mono">v{p.version}</span>
								{#if group.versions.length > 1}
									<span class="text-zinc-500 text-xs">{group.versions.length} platform binaries</span>
								{/if}
							</div>
							{#if p.readme}
								<p class="text-sm text-zinc-400 mt-1 line-clamp-2">{p.readme.slice(0, 120)}</p>
							{/if}
						</div>
						<div class="text-right text-xs text-zinc-500 flex-shrink-0">
							<div>Published {new Date(p.published_at).toLocaleDateString()}</div>
							{#if p.published_by}
								<div class="mt-0.5 truncate max-w-32">{p.published_by}</div>
							{/if}
						</div>
					</div>
					<div class="mt-3 font-mono text-xs text-zinc-400 bg-zinc-800 rounded px-3 py-1.5">
						source = "{window?.location?.hostname ?? 'crucible.example.com'}/{p.namespace}/{p.type}"
					</div>
				</a>
			{/each}
		</div>
	{/if}
</div>
