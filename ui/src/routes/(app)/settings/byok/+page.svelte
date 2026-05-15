<script lang="ts">
	import { onMount } from 'svelte';
	import { byok, type BYOKStatus, type KMSProvider } from '$lib/api/client';
	import { toast } from '$lib/stores/toasts.svelte';

	let status = $state<BYOKStatus | null>(null);
	let loading = $state(true);
	let error = $state<string | null>(null);

	// Enable modal
	let showEnableModal = $state(false);
	let formProvider = $state<KMSProvider>('aws_kms');
	let formKeyID = $state('');
	let testing = $state(false);
	let testOK = $state(false);
	let saving = $state(false);
	let modalError = $state<string | null>(null);

	// Confirmation modals
	let showRotateConfirm = $state(false);
	let showDisableConfirm = $state(false);
	let rotating = $state(false);
	let disabling = $state(false);

	const providerLabels: Record<KMSProvider, string> = {
		aws_kms: 'AWS KMS',
		hc_vault_transit: 'HashiCorp Vault Transit',
		azure_kv: 'Azure Key Vault'
	};

	const keyIDHints: Record<KMSProvider, string> = {
		aws_kms: 'KMS key ARN or alias (arn:aws:kms:us-east-1:...:key/...)',
		hc_vault_transit: 'Transit key name (e.g. crucible-master)',
		azure_kv: 'Full key URL (https://{vault}.vault.azure.net/keys/{name}[/{version}])'
	};

	const envHints: Record<KMSProvider, string> = {
		aws_kms: 'Set CRUCIBLE_KMS_AWS_REGION + CRUCIBLE_KMS_AWS_ACCESS_KEY_ID + CRUCIBLE_KMS_AWS_SECRET_ACCESS_KEY in the server env before enabling.',
		hc_vault_transit: 'Set CRUCIBLE_KMS_VAULT_ADDR plus either CRUCIBLE_KMS_VAULT_TOKEN, or CRUCIBLE_KMS_VAULT_ROLE_ID + CRUCIBLE_KMS_VAULT_SECRET_ID for AppRole.',
		azure_kv: 'Set CRUCIBLE_KMS_AZURE_TENANT_ID + CRUCIBLE_KMS_AZURE_CLIENT_ID + CRUCIBLE_KMS_AZURE_CLIENT_SECRET for the service principal that has wrapKey/unwrapKey on the key.'
	};

	onMount(load);

	async function load() {
		loading = true;
		error = null;
		try {
			status = await byok.status();
		} catch (e) {
			error = (e as Error).message;
		} finally {
			loading = false;
		}
	}

	function openEnable() {
		formProvider = 'aws_kms';
		formKeyID = '';
		testOK = false;
		modalError = null;
		showEnableModal = true;
	}

	async function testAccess() {
		testing = true;
		testOK = false;
		modalError = null;
		try {
			await byok.test(formProvider, formKeyID);
			testOK = true;
		} catch (e) {
			modalError = (e as Error).message;
		} finally {
			testing = false;
		}
	}

	async function enable() {
		saving = true;
		modalError = null;
		try {
			await byok.enable(formProvider, formKeyID);
			showEnableModal = false;
			toast.success('BYOK enabled — vault re-encrypted under the new master key.');
			await load();
		} catch (e) {
			modalError = (e as Error).message;
		} finally {
			saving = false;
		}
	}

	async function rotate() {
		rotating = true;
		try {
			await byok.rotate();
			showRotateConfirm = false;
			toast.success('Master key rotated — vault re-encrypted.');
			await load();
		} catch (e) {
			toast.error((e as Error).message);
		} finally {
			rotating = false;
		}
	}

	async function disable() {
		disabling = true;
		try {
			await byok.disable();
			showDisableConfirm = false;
			toast.success('BYOK disabled — reverted to CRUCIBLE_SECRET_KEY.');
			await load();
		} catch (e) {
			toast.error((e as Error).message);
		} finally {
			disabling = false;
		}
	}
</script>

<div class="max-w-2xl">
	<div class="mb-6">
		<h1 class="text-xl font-semibold text-white">BYOK — Customer-Managed Encryption Keys</h1>
		<p class="text-sm text-zinc-400 mt-0.5">
			Wrap the vault master key with a key from your own KMS. Every vault-protected
			row (stack env vars, integration configs, webhook secrets, SIEM destinations, etc.)
			is re-encrypted under the new master in a single transaction. The wrapped blob is
			unwrapped via KMS once at server boot.
		</p>
	</div>

	{#if loading}
		<p class="text-zinc-500 text-sm">Loading…</p>
	{:else if error}
		<p class="text-red-400 text-sm">{error}</p>
	{:else if status}
		<!-- Status card -->
		<div class="border border-zinc-800 rounded-xl p-5 mb-6">
			<div class="flex items-center justify-between">
				<div>
					<div class="flex items-center gap-2 mb-1">
						<span class="inline-block w-2 h-2 rounded-full {status.enabled ? 'bg-teal-400' : 'bg-zinc-600'}"></span>
						<span class="text-sm font-medium text-white">
							{status.enabled ? 'BYOK enabled' : 'BYOK disabled'}
						</span>
					</div>
					{#if status.enabled && status.provider}
						<p class="text-xs text-zinc-500">
							Provider: <span class="text-zinc-300">{providerLabels[status.provider]}</span>
						</p>
						<p class="text-xs text-zinc-500 font-mono mt-0.5 break-all">{status.key_id}</p>
					{:else}
						<p class="text-xs text-zinc-500">Master derived from CRUCIBLE_SECRET_KEY (default).</p>
					{/if}
				</div>
				<div class="flex items-center gap-2">
					{#if status.enabled}
						<button onclick={() => (showRotateConfirm = true)}
							class="px-3 py-1.5 text-sm bg-zinc-800 hover:bg-zinc-700 text-zinc-100 rounded-lg transition-colors">
							Rotate master
						</button>
						<button onclick={() => (showDisableConfirm = true)}
							class="px-3 py-1.5 text-sm bg-zinc-800 hover:bg-zinc-700 text-red-400 rounded-lg transition-colors">
							Disable
						</button>
					{:else}
						<button onclick={openEnable}
							class="px-3 py-1.5 text-sm bg-teal-600 hover:bg-teal-500 text-white rounded-lg transition-colors">
							Enable BYOK
						</button>
					{/if}
				</div>
			</div>
		</div>

		<div class="text-xs text-zinc-500 space-y-2 border border-zinc-800/60 rounded-lg p-4">
			<p>
				<span class="text-zinc-300 font-medium">How it works:</span>
				A random 32-byte master key is generated and wrapped by your KMS. Crucible stores
				only the wrapped blob; the plaintext master is unwrapped at server boot and held
				in memory.
			</p>
			<p>
				<span class="text-zinc-300 font-medium">Auth credentials</span> live in environment
				variables, never the database — see the per-provider hints in the Enable dialog.
			</p>
			<p>
				<span class="text-zinc-300 font-medium">Rotation</span> generates a new random master,
				re-wraps it via your KMS, and re-encrypts every vault row in a single transaction.
				<span class="text-zinc-300 font-medium">Disable</span> reverts to the
				<code>CRUCIBLE_SECRET_KEY</code>-derived master.
			</p>
		</div>
	{/if}
</div>

<!-- Enable modal -->
{#if showEnableModal}
	<div class="fixed inset-0 bg-black/60 flex items-center justify-center z-50 p-4">
		<div class="bg-zinc-900 border border-zinc-700 rounded-xl w-full max-w-lg shadow-2xl">
			<div class="px-6 py-4 border-b border-zinc-800">
				<h2 class="text-white font-semibold">Enable BYOK</h2>
			</div>
			<div class="px-6 py-4 space-y-4">
				{#if modalError}
					<p class="text-red-400 text-sm bg-red-950 border border-red-800 rounded px-3 py-2">{modalError}</p>
				{/if}

				<div>
					<label class="block text-xs text-zinc-400 mb-1" for="byok-provider">KMS provider</label>
					<select id="byok-provider" bind:value={formProvider}
						class="w-full bg-zinc-800 border border-zinc-700 rounded-lg px-3 py-2 text-sm text-white focus:outline-none focus:border-teal-500">
						<option value="aws_kms">AWS KMS</option>
						<option value="hc_vault_transit">HashiCorp Vault Transit</option>
						<option value="azure_kv">Azure Key Vault</option>
					</select>
				</div>

				<div>
					<label class="block text-xs text-zinc-400 mb-1" for="byok-key">Key identifier</label>
					<input id="byok-key" type="text" bind:value={formKeyID}
						placeholder={keyIDHints[formProvider]}
						class="w-full bg-zinc-800 border border-zinc-700 rounded-lg px-3 py-2 text-sm text-white placeholder-zinc-500 focus:outline-none focus:border-teal-500 font-mono" />
				</div>

				<p class="text-xs text-zinc-500">{envHints[formProvider]}</p>

				{#if testOK}
					<p class="text-xs text-teal-400 bg-teal-950 border border-teal-800 rounded px-3 py-2">
						✓ Test passed — KMS can wrap and unwrap with this key.
					</p>
				{/if}
			</div>
			<div class="px-6 py-4 border-t border-zinc-800 flex justify-end gap-2">
				<button onclick={() => (showEnableModal = false)} disabled={saving || testing}
					class="px-3 py-1.5 text-sm text-zinc-400 hover:text-white transition-colors">
					Cancel
				</button>
				<button onclick={testAccess} disabled={!formKeyID || testing || saving}
					class="px-3 py-1.5 text-sm bg-zinc-800 hover:bg-zinc-700 disabled:opacity-50 text-zinc-100 rounded-lg transition-colors">
					{testing ? 'Testing…' : 'Test access'}
				</button>
				<button onclick={enable} disabled={!testOK || saving}
					class="px-4 py-1.5 text-sm bg-teal-600 hover:bg-teal-500 disabled:opacity-50 text-white rounded-lg transition-colors">
					{saving ? 'Enabling…' : 'Enable BYOK'}
				</button>
			</div>
		</div>
	</div>
{/if}

<!-- Rotate confirmation -->
{#if showRotateConfirm}
	<div class="fixed inset-0 bg-black/60 flex items-center justify-center z-50 p-4">
		<div class="bg-zinc-900 border border-zinc-700 rounded-xl w-full max-w-md shadow-2xl">
			<div class="px-6 py-4 border-b border-zinc-800">
				<h2 class="text-white font-semibold">Rotate master key</h2>
			</div>
			<div class="px-6 py-4">
				<p class="text-sm text-zinc-300">
					This generates a new random master, re-wraps it with your KMS, and re-encrypts
					every vault-protected row in a single transaction. The operation is atomic;
					readers see either the old or new state consistently.
				</p>
			</div>
			<div class="px-6 py-4 border-t border-zinc-800 flex justify-end gap-2">
				<button onclick={() => (showRotateConfirm = false)} disabled={rotating}
					class="px-3 py-1.5 text-sm text-zinc-400 hover:text-white transition-colors">Cancel</button>
				<button onclick={rotate} disabled={rotating}
					class="px-4 py-1.5 text-sm bg-teal-600 hover:bg-teal-500 disabled:opacity-50 text-white rounded-lg transition-colors">
					{rotating ? 'Rotating…' : 'Rotate master'}
				</button>
			</div>
		</div>
	</div>
{/if}

<!-- Disable confirmation -->
{#if showDisableConfirm}
	<div class="fixed inset-0 bg-black/60 flex items-center justify-center z-50 p-4">
		<div class="bg-zinc-900 border border-zinc-700 rounded-xl w-full max-w-md shadow-2xl">
			<div class="px-6 py-4 border-b border-zinc-800">
				<h2 class="text-white font-semibold">Disable BYOK</h2>
			</div>
			<div class="px-6 py-4 space-y-2">
				<p class="text-sm text-zinc-300">
					Reverts the master key to one derived from <code>CRUCIBLE_SECRET_KEY</code>
					and re-encrypts every vault row under it. The KMS-wrapped blob is cleared.
				</p>
				<p class="text-xs text-amber-400">
					CRUCIBLE_SECRET_KEY must remain set in the server environment after this completes.
				</p>
			</div>
			<div class="px-6 py-4 border-t border-zinc-800 flex justify-end gap-2">
				<button onclick={() => (showDisableConfirm = false)} disabled={disabling}
					class="px-3 py-1.5 text-sm text-zinc-400 hover:text-white transition-colors">Cancel</button>
				<button onclick={disable} disabled={disabling}
					class="px-4 py-1.5 text-sm bg-red-600 hover:bg-red-500 disabled:opacity-50 text-white rounded-lg transition-colors">
					{disabling ? 'Disabling…' : 'Disable BYOK'}
				</button>
			</div>
		</div>
	</div>
{/if}
