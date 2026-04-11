<script lang="ts">
	import { onMount } from 'svelte';
	import {
		integrations,
		type Integration,
		type IntegrationType,
		type VCSIntegrationConfig,
		type AWSSecretStoreConfig,
		type HCVaultSecretStoreConfig,
		type BitwardenSecretStoreConfig,
		type VaultwardenSecretStoreConfig
	} from '$lib/api/client';

	let items = $state<Integration[]>([]);
	let loading = $state(true);
	let error = $state<string | null>(null);

	// Modal state
	let showModal = $state(false);
	let editingID = $state<string | null>(null);
	let saving = $state(false);
	let modalError = $state<string | null>(null);

	// Form fields
	let formName = $state('');
	let formType = $state<IntegrationType>('github');
	// VCS
	let vcsCfg = $state<VCSIntegrationConfig>({ token: '' });
	// AWS SM
	let awsCfg = $state<AWSSecretStoreConfig>({ region: '', secret_names: [] });
	let awsSecretNamesRaw = $state('');
	// HC Vault
	let vaultCfg = $state<HCVaultSecretStoreConfig>({ address: '', mount: 'secret', path: '' });
	// Bitwarden SM
	let bwCfg = $state<BitwardenSecretStoreConfig>({ access_token: '' });
	// Vaultwarden
	let vwCfg = $state<VaultwardenSecretStoreConfig>({ url: '', client_id: '', client_secret: '', email: '', master_password: '' });

	onMount(load);

	async function load() {
		loading = true;
		error = null;
		try {
			items = await integrations.list();
		} catch (e) {
			error = (e as Error).message;
		} finally {
			loading = false;
		}
	}

	function openCreate() {
		editingID = null;
		formName = '';
		formType = 'github';
		resetConfigs();
		modalError = null;
		showModal = true;
	}

	function openEdit(item: Integration) {
		editingID = item.id;
		formName = item.name;
		formType = item.type;
		resetConfigs();
		modalError = null;
		showModal = true;
	}

	function resetConfigs() {
		vcsCfg = { token: '' };
		awsCfg = { region: '', secret_names: [] };
		awsSecretNamesRaw = '';
		vaultCfg = { address: '', mount: 'secret', path: '' };
		bwCfg = { access_token: '' };
		vwCfg = { url: '', client_id: '', client_secret: '', email: '', master_password: '' };
	}

	function currentConfig() {
		if (formType === 'github' || formType === 'gitlab' || formType === 'gitea') return vcsCfg;
		if (formType === 'aws_sm') {
			return { ...awsCfg, secret_names: awsSecretNamesRaw.split('\n').map(s => s.trim()).filter(Boolean) };
		}
		if (formType === 'hc_vault') return vaultCfg;
		if (formType === 'bitwarden_sm') return bwCfg;
		return vwCfg;
	}

	async function save() {
		saving = true;
		modalError = null;
		try {
			const cfg = currentConfig();
			if (editingID) {
				const updated = await integrations.update(editingID, {
					name: formName || undefined,
					config: Object.values(cfg).some(v => v !== '') ? cfg : undefined
				});
				items = items.map(i => i.id === updated.id ? updated : i);
			} else {
				const created = await integrations.create(formName, formType, cfg);
				items = [...items, created];
			}
			showModal = false;
		} catch (e) {
			modalError = (e as Error).message;
		} finally {
			saving = false;
		}
	}

	async function remove(id: string, name: string) {
		if (!confirm(`Delete integration "${name}"? Any stacks using it will lose access.`)) return;
		try {
			await integrations.delete(id);
			items = items.filter(i => i.id !== id);
		} catch (e) {
			alert((e as Error).message);
		}
	}

	const typeLabels: Record<IntegrationType, string> = {
		github: 'GitHub',
		gitlab: 'GitLab',
		gitea: 'Gitea',
		aws_sm: 'AWS Secrets Manager',
		hc_vault: 'HashiCorp Vault',
		bitwarden_sm: 'Bitwarden Secrets Manager',
		vaultwarden: 'Vaultwarden'
	};

	const typeGroups = [
		{ label: 'VCS / Git credentials', types: ['github', 'gitlab', 'gitea'] as IntegrationType[] },
		{ label: 'Secret stores', types: ['aws_sm', 'hc_vault', 'bitwarden_sm', 'vaultwarden'] as IntegrationType[] }
	];

	function groupItems(type: 'vcs' | 'secret') {
		const vcsTypes = new Set(['github', 'gitlab', 'gitea']);
		return items.filter(i => type === 'vcs' ? vcsTypes.has(i.type) : !vcsTypes.has(i.type));
	}

	function isVCS(t: IntegrationType) {
		return t === 'github' || t === 'gitlab' || t === 'gitea';
	}
</script>

<div class="max-w-2xl">
	<div class="flex items-center justify-between mb-6">
		<div>
			<h1 class="text-xl font-semibold text-white">Integrations</h1>
			<p class="text-sm text-zinc-400 mt-0.5">Org-level credentials for VCS and external secret stores. Stacks select which integration to use.</p>
		</div>
		<button onclick={openCreate}
			class="px-3 py-1.5 text-sm bg-indigo-600 hover:bg-indigo-500 text-white rounded-lg transition-colors">
			Add integration
		</button>
	</div>

	{#if loading}
		<p class="text-zinc-500 text-sm">Loading…</p>
	{:else if error}
		<p class="text-red-400 text-sm">{error}</p>
	{:else}
		<!-- VCS integrations -->
		<section class="mb-8">
			<h2 class="text-xs font-medium text-zinc-500 uppercase tracking-wide mb-3">VCS / Git credentials</h2>
			{#if groupItems('vcs').length === 0}
				<div class="border border-zinc-800 rounded-lg px-4 py-6 text-center">
					<p class="text-zinc-500 text-sm">No VCS integrations yet.</p>
					<p class="text-zinc-600 text-xs mt-1">Add a GitHub, GitLab, or Gitea token to clone private repositories.</p>
				</div>
			{:else}
				<div class="border border-zinc-800 rounded-lg divide-y divide-zinc-800 overflow-hidden">
					{#each groupItems('vcs') as item}
						<div class="flex items-center justify-between px-4 py-3">
							<div class="flex items-center gap-3">
								<span class="text-xs px-2 py-0.5 rounded bg-zinc-800 text-zinc-300 font-mono">{typeLabels[item.type]}</span>
								<span class="text-sm text-white">{item.name}</span>
							</div>
							<div class="flex items-center gap-2">
								<button onclick={() => openEdit(item)}
									class="text-xs text-zinc-400 hover:text-white transition-colors px-2 py-1">
									Update token
								</button>
								<button onclick={() => remove(item.id, item.name)}
									class="text-xs text-red-500 hover:text-red-400 transition-colors px-2 py-1">
									Delete
								</button>
							</div>
						</div>
					{/each}
				</div>
			{/if}
		</section>

		<!-- Secret store integrations -->
		<section>
			<h2 class="text-xs font-medium text-zinc-500 uppercase tracking-wide mb-3">Secret stores</h2>
			{#if groupItems('secret').length === 0}
				<div class="border border-zinc-800 rounded-lg px-4 py-6 text-center">
					<p class="text-zinc-500 text-sm">No secret store integrations yet.</p>
					<p class="text-zinc-600 text-xs mt-1">Connect AWS Secrets Manager, HashiCorp Vault, Bitwarden, or Vaultwarden to inject secrets into runs.</p>
				</div>
			{:else}
				<div class="border border-zinc-800 rounded-lg divide-y divide-zinc-800 overflow-hidden">
					{#each groupItems('secret') as item}
						<div class="flex items-center justify-between px-4 py-3">
							<div class="flex items-center gap-3">
								<span class="text-xs px-2 py-0.5 rounded bg-zinc-800 text-zinc-300 font-mono">{typeLabels[item.type]}</span>
								<span class="text-sm text-white">{item.name}</span>
							</div>
							<div class="flex items-center gap-2">
								<button onclick={() => openEdit(item)}
									class="text-xs text-zinc-400 hover:text-white transition-colors px-2 py-1">
									Update config
								</button>
								<button onclick={() => remove(item.id, item.name)}
									class="text-xs text-red-500 hover:text-red-400 transition-colors px-2 py-1">
									Delete
								</button>
							</div>
						</div>
					{/each}
				</div>
			{/if}
		</section>
	{/if}
</div>

<!-- Add / edit modal -->
{#if showModal}
	<div class="fixed inset-0 bg-black/60 flex items-center justify-center z-50 p-4">
		<div class="bg-zinc-900 border border-zinc-700 rounded-xl w-full max-w-lg shadow-2xl">
			<div class="px-6 py-4 border-b border-zinc-800">
				<h2 class="text-white font-semibold">{editingID ? 'Update integration' : 'Add integration'}</h2>
			</div>
			<div class="px-6 py-4 space-y-4">
				{#if modalError}
					<p class="text-red-400 text-sm bg-red-950 border border-red-800 rounded px-3 py-2">{modalError}</p>
				{/if}

				<div>
					<label class="block text-xs text-zinc-400 mb-1" for="int-name">Name</label>
					<input id="int-name" type="text" bind:value={formName} placeholder="e.g. GitHub (ponack)"
						class="w-full bg-zinc-800 border border-zinc-700 rounded-lg px-3 py-2 text-sm text-white placeholder-zinc-500 focus:outline-none focus:border-indigo-500" />
				</div>

				{#if !editingID}
				<div>
					<label class="block text-xs text-zinc-400 mb-1" for="int-type">Type</label>
					<select id="int-type" bind:value={formType}
						class="w-full bg-zinc-800 border border-zinc-700 rounded-lg px-3 py-2 text-sm text-white focus:outline-none focus:border-indigo-500">
						{#each typeGroups as group}
							<optgroup label={group.label}>
								{#each group.types as t}
									<option value={t}>{typeLabels[t]}</option>
								{/each}
							</optgroup>
						{/each}
					</select>
				</div>
				{/if}

				<!-- VCS config -->
				{#if isVCS(formType)}
					<div>
						<label class="block text-xs text-zinc-400 mb-1" for="int-token">
							Personal access token {editingID ? '(leave blank to keep existing)' : ''}
						</label>
						<input id="int-token" type="password" bind:value={vcsCfg.token}
							placeholder={editingID ? '••••••••' : 'ghp_…'}
							class="w-full bg-zinc-800 border border-zinc-700 rounded-lg px-3 py-2 text-sm text-white placeholder-zinc-500 focus:outline-none focus:border-indigo-500" />
						<p class="text-xs text-zinc-500 mt-1">
							{#if formType === 'github'}Requires <code>repo</code> scope (or <code>contents:read</code> for fine-grained tokens).
							{:else if formType === 'gitlab'}Requires <code>read_repository</code> scope.
							{:else}Requires repository read access.{/if}
						</p>
					</div>

				<!-- AWS Secrets Manager -->
				{:else if formType === 'aws_sm'}
					<div class="grid grid-cols-2 gap-3">
						<div>
							<label class="block text-xs text-zinc-400 mb-1" for="aws-region">Region</label>
							<input id="aws-region" type="text" bind:value={awsCfg.region} placeholder="us-east-1"
								class="w-full bg-zinc-800 border border-zinc-700 rounded-lg px-3 py-2 text-sm text-white placeholder-zinc-500 focus:outline-none focus:border-indigo-500" />
						</div>
						<div>
							<label class="block text-xs text-zinc-400 mb-1" for="aws-key">Access key ID <span class="text-zinc-600">(optional)</span></label>
							<input id="aws-key" type="text" bind:value={awsCfg.access_key_id} placeholder="AKIA…"
								class="w-full bg-zinc-800 border border-zinc-700 rounded-lg px-3 py-2 text-sm text-white placeholder-zinc-500 focus:outline-none focus:border-indigo-500" />
						</div>
					</div>
					<div>
						<label class="block text-xs text-zinc-400 mb-1" for="aws-secret">Secret access key <span class="text-zinc-600">(optional)</span></label>
						<input id="aws-secret" type="password" bind:value={awsCfg.secret_access_key}
							class="w-full bg-zinc-800 border border-zinc-700 rounded-lg px-3 py-2 text-sm text-white placeholder-zinc-500 focus:outline-none focus:border-indigo-500" />
					</div>
					<div>
						<label class="block text-xs text-zinc-400 mb-1" for="aws-names">Secret names <span class="text-zinc-500">(one per line)</span></label>
						<textarea id="aws-names" bind:value={awsSecretNamesRaw} rows="3" placeholder="my-app/db-password&#10;my-app/api-key"
							class="w-full bg-zinc-800 border border-zinc-700 rounded-lg px-3 py-2 text-sm text-white placeholder-zinc-500 focus:outline-none focus:border-indigo-500 resize-none font-mono"></textarea>
					</div>

				<!-- HashiCorp Vault -->
				{:else if formType === 'hc_vault'}
					<div>
						<label class="block text-xs text-zinc-400 mb-1" for="hv-addr">Vault address</label>
						<input id="hv-addr" type="text" bind:value={vaultCfg.address} placeholder="https://vault.example.com"
							class="w-full bg-zinc-800 border border-zinc-700 rounded-lg px-3 py-2 text-sm text-white placeholder-zinc-500 focus:outline-none focus:border-indigo-500" />
					</div>
					<div class="grid grid-cols-2 gap-3">
						<div>
							<label class="block text-xs text-zinc-400 mb-1" for="hv-mount">Mount</label>
							<input id="hv-mount" type="text" bind:value={vaultCfg.mount} placeholder="secret"
								class="w-full bg-zinc-800 border border-zinc-700 rounded-lg px-3 py-2 text-sm text-white placeholder-zinc-500 focus:outline-none focus:border-indigo-500" />
						</div>
						<div>
							<label class="block text-xs text-zinc-400 mb-1" for="hv-path">Path</label>
							<input id="hv-path" type="text" bind:value={vaultCfg.path} placeholder="myapp/config"
								class="w-full bg-zinc-800 border border-zinc-700 rounded-lg px-3 py-2 text-sm text-white placeholder-zinc-500 focus:outline-none focus:border-indigo-500" />
						</div>
					</div>
					<div>
						<label class="block text-xs text-zinc-400 mb-1" for="hv-token">Token <span class="text-zinc-600">(or use AppRole below)</span></label>
						<input id="hv-token" type="password" bind:value={vaultCfg.token}
							class="w-full bg-zinc-800 border border-zinc-700 rounded-lg px-3 py-2 text-sm text-white placeholder-zinc-500 focus:outline-none focus:border-indigo-500" />
					</div>
					<div class="grid grid-cols-2 gap-3">
						<div>
							<label class="block text-xs text-zinc-400 mb-1" for="hv-role">Role ID</label>
							<input id="hv-role" type="text" bind:value={vaultCfg.role_id}
								class="w-full bg-zinc-800 border border-zinc-700 rounded-lg px-3 py-2 text-sm text-white placeholder-zinc-500 focus:outline-none focus:border-indigo-500" />
						</div>
						<div>
							<label class="block text-xs text-zinc-400 mb-1" for="hv-secret-id">Secret ID</label>
							<input id="hv-secret-id" type="password" bind:value={vaultCfg.secret_id}
								class="w-full bg-zinc-800 border border-zinc-700 rounded-lg px-3 py-2 text-sm text-white placeholder-zinc-500 focus:outline-none focus:border-indigo-500" />
						</div>
					</div>

				<!-- Bitwarden SM -->
				{:else if formType === 'bitwarden_sm'}
					<div>
						<label class="block text-xs text-zinc-400 mb-1" for="bw-token">Machine account access token</label>
						<input id="bw-token" type="password" bind:value={bwCfg.access_token}
							class="w-full bg-zinc-800 border border-zinc-700 rounded-lg px-3 py-2 text-sm text-white placeholder-zinc-500 focus:outline-none focus:border-indigo-500" />
					</div>
					<div class="grid grid-cols-2 gap-3">
						<div>
							<label class="block text-xs text-zinc-400 mb-1" for="bw-proj">Project ID <span class="text-zinc-600">(optional)</span></label>
							<input id="bw-proj" type="text" bind:value={bwCfg.project_id}
								class="w-full bg-zinc-800 border border-zinc-700 rounded-lg px-3 py-2 text-sm text-white placeholder-zinc-500 focus:outline-none focus:border-indigo-500" />
						</div>
						<div>
							<label class="block text-xs text-zinc-400 mb-1" for="bw-org">Org ID <span class="text-zinc-600">(optional)</span></label>
							<input id="bw-org" type="text" bind:value={bwCfg.org_id}
								class="w-full bg-zinc-800 border border-zinc-700 rounded-lg px-3 py-2 text-sm text-white placeholder-zinc-500 focus:outline-none focus:border-indigo-500" />
						</div>
					</div>

				<!-- Vaultwarden -->
				{:else if formType === 'vaultwarden'}
					<div>
						<label class="block text-xs text-zinc-400 mb-1" for="vw-url">Vaultwarden URL</label>
						<input id="vw-url" type="text" bind:value={vwCfg.url} placeholder="https://vaultwarden.example.com"
							class="w-full bg-zinc-800 border border-zinc-700 rounded-lg px-3 py-2 text-sm text-white placeholder-zinc-500 focus:outline-none focus:border-indigo-500" />
					</div>
					<div class="grid grid-cols-2 gap-3">
						<div>
							<label class="block text-xs text-zinc-400 mb-1" for="vw-cid">Client ID</label>
							<input id="vw-cid" type="text" bind:value={vwCfg.client_id}
								class="w-full bg-zinc-800 border border-zinc-700 rounded-lg px-3 py-2 text-sm text-white placeholder-zinc-500 focus:outline-none focus:border-indigo-500" />
						</div>
						<div>
							<label class="block text-xs text-zinc-400 mb-1" for="vw-csecret">Client secret</label>
							<input id="vw-csecret" type="password" bind:value={vwCfg.client_secret}
								class="w-full bg-zinc-800 border border-zinc-700 rounded-lg px-3 py-2 text-sm text-white placeholder-zinc-500 focus:outline-none focus:border-indigo-500" />
						</div>
					</div>
					<div class="grid grid-cols-2 gap-3">
						<div>
							<label class="block text-xs text-zinc-400 mb-1" for="vw-email">Email</label>
							<input id="vw-email" type="email" bind:value={vwCfg.email}
								class="w-full bg-zinc-800 border border-zinc-700 rounded-lg px-3 py-2 text-sm text-white placeholder-zinc-500 focus:outline-none focus:border-indigo-500" />
						</div>
						<div>
							<label class="block text-xs text-zinc-400 mb-1" for="vw-mp">Master password</label>
							<input id="vw-mp" type="password" bind:value={vwCfg.master_password}
								class="w-full bg-zinc-800 border border-zinc-700 rounded-lg px-3 py-2 text-sm text-white placeholder-zinc-500 focus:outline-none focus:border-indigo-500" />
						</div>
					</div>
				{/if}
			</div>
			<div class="px-6 py-4 border-t border-zinc-800 flex justify-end gap-2">
				<button onclick={() => { showModal = false; }} disabled={saving}
					class="px-3 py-1.5 text-sm text-zinc-400 hover:text-white transition-colors">
					Cancel
				</button>
				<button onclick={save} disabled={saving || !formName}
					class="px-4 py-1.5 text-sm bg-indigo-600 hover:bg-indigo-500 disabled:opacity-50 text-white rounded-lg transition-colors">
					{saving ? 'Saving…' : editingID ? 'Update' : 'Add'}
				</button>
			</div>
		</div>
	</div>
{/if}
