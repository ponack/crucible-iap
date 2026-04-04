<script lang="ts">
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { onMount } from 'svelte';
	import { stacks, runs, policies, type Stack, type Run, type StackToken, type Policy, type StackPolicyRef, type StackEnvVar, type SecretStoreProvider, type AWSSecretStoreConfig, type HCVaultSecretStoreConfig, type BitwardenSecretStoreConfig, type VaultwardenSecretStoreConfig, type StateBackendProvider, type S3StateBackendConfig, type GCSStateBackendConfig, type AzureStateBackendConfig } from '$lib/api/client';

	const stackID = $derived(page.params.id as string);

	let stack = $state<Stack | null>(null);
	let recentRuns = $state<Run[]>([]);
	let tokens = $state<StackToken[]>([]);
	let stackPolicies = $state<StackPolicyRef[]>([]);
	let allPolicies = $state<Policy[]>([]);
	let loading = $state(true);
	let error = $state<string | null>(null);

	// Edit form state
	let editing = $state(false);
	let saving = $state(false);
	let editError = $state<string | null>(null);
	let form = $state({
		name: '', description: '', repo_branch: '', project_root: '',
		auto_apply: false, drift_detection: false, drift_schedule: ''
	});

	// Token creation
	let newTokenName = $state('');
	let creatingToken = $state(false);
	let newTokenSecret = $state<string | null>(null);

	// Run creation
	let triggeringRun = $state(false);
	let triggeringDrift = $state(false);

	// Destroy modal
	let showDestroyModal = $state(false);
	let destroyConfirmName = $state('');
	let triggeringDestroy = $state(false);

	// Policy attachment
	let attachingPolicy = $state('');

	// Env vars
	let envVars = $state<StackEnvVar[]>([]);
	let newEnvName = $state('');
	let newEnvValue = $state('');
	let savingEnv = $state(false);

	// Notifications
	let notifVCSToken = $state('');
	let notifSlackWebhook = $state('');
	let notifEvents = $state<string[]>([]);
	let savingNotif = $state(false);
	let notifSaved = $state(false);

	// Secret store
	let secretStoreProvider = $state<SecretStoreProvider | ''>('');
	let savingSecretStore = $state(false);
	let secretStoreSaved = $state(false);
	let removingSecretStore = $state(false);
	// AWS SM
	let awsCfg = $state<AWSSecretStoreConfig>({ region: '', secret_names: [] });
	let awsNewSecretName = $state('');
	// HashiCorp Vault
	let vaultCfg = $state<HCVaultSecretStoreConfig>({ address: '', mount: 'secret', path: '' });
	// Bitwarden SM
	let bwCfg = $state<BitwardenSecretStoreConfig>({ access_token: '' });
	// Vaultwarden
	let vwCfg = $state<VaultwardenSecretStoreConfig>({ url: '', client_id: '', client_secret: '', email: '', master_password: '' });

	// Notifications VCS provider
	let notifVCSProvider = $state('');
	let notifVCSBaseURL = $state('');

	// Webhook
	let rotatingWebhook = $state(false);
	let newWebhookSecret = $state<string | null>(null);

	// Disable/enable
	let togglingDisabled = $state(false);

	// State backend
	let stateBackendProvider = $state<StateBackendProvider | ''>('');
	let savingStateBackend = $state(false);
	let stateBackendSaved = $state(false);
	let removingStateBackend = $state(false);
	let s3Cfg = $state<S3StateBackendConfig>({ region: '', bucket: '' });
	let gcsCfg = $state<GCSStateBackendConfig>({ bucket: '', service_account_json: '' });
	let azureCfg = $state<AzureStateBackendConfig>({ account_name: '', account_key: '', container: '' });

	const notifyEventOptions = [
		{ value: 'plan_complete', label: 'Plan complete' },
		{ value: 'run_finished', label: 'Run succeeded' },
		{ value: 'run_failed', label: 'Run failed' }
	];

	const driftScheduleOptions = [
		{ value: '', label: 'Disabled' },
		{ value: '30', label: 'Every 30 minutes' },
		{ value: '60', label: 'Every hour' },
		{ value: '360', label: 'Every 6 hours' },
		{ value: '720', label: 'Every 12 hours' },
		{ value: '1440', label: 'Every 24 hours' }
	];

	onMount(async () => {
		try {
			const [stackRes, runsRes, tokensRes, stackPoliciesRes, allPoliciesRes, envVarsRes] = await Promise.all([
				stacks.get(stackID),
				runs.list(stackID),
				stacks.tokens.list(stackID),
				policies.forStack(stackID),
				policies.list(),
				stacks.env.list(stackID)
			]);
			stack = stackRes;
			recentRuns = runsRes.data;
			tokens = tokensRes;
			stackPolicies = stackPoliciesRes;
			allPolicies = allPoliciesRes;
			envVars = envVarsRes;
			if (stackRes.secret_store_provider) {
				secretStoreProvider = stackRes.secret_store_provider as SecretStoreProvider;
			}
			if (stackRes.state_backend_provider) {
				stateBackendProvider = stackRes.state_backend_provider as StateBackendProvider;
			}
			notifVCSProvider = stackRes.vcs_provider ?? 'github';
			notifVCSBaseURL = stackRes.vcs_base_url ?? '';
			resetForm();
		} catch (e) {
			error = (e as Error).message;
		} finally {
			loading = false;
		}
	});

	function resetForm() {
		if (!stack) return;
		form = {
			name: stack.name,
			description: stack.description ?? '',
			repo_branch: stack.repo_branch,
			project_root: stack.project_root,
			auto_apply: stack.auto_apply,
			drift_detection: stack.drift_detection,
			drift_schedule: stack.drift_schedule ?? ''
		};
		notifEvents = [...(stack.notify_events ?? [])];
	}

	async function saveEdit(e: SubmitEvent) {
		e.preventDefault();
		saving = true;
		editError = null;
		try {
			stack = await stacks.update(stackID, form);
			editing = false;
		} catch (e) {
			editError = (e as Error).message;
		} finally {
			saving = false;
		}
	}

	async function deleteStack() {
		if (!confirm(`Delete stack "${stack?.name}"? This cannot be undone.`)) return;
		await stacks.delete(stackID);
		goto('/stacks');
	}

	async function triggerRun() {
		triggeringRun = true;
		try {
			const run = await runs.create(stackID);
			goto(`/runs/${run.id}`);
		} catch (e) {
			alert((e as Error).message);
			triggeringRun = false;
		}
	}

	async function triggerDrift() {
		triggeringDrift = true;
		try {
			const run = await runs.triggerDrift(stackID);
			goto(`/runs/${run.id}`);
		} catch (e) {
			alert((e as Error).message);
			triggeringDrift = false;
		}
	}

	async function triggerDestroy() {
		if (!stack || destroyConfirmName !== stack.name) return;
		triggeringDestroy = true;
		try {
			const run = await runs.create(stackID, 'destroy');
			goto(`/runs/${run.id}`);
		} catch (e) {
			alert((e as Error).message);
			triggeringDestroy = false;
		}
	}

	async function createToken(e: SubmitEvent) {
		e.preventDefault();
		creatingToken = true;
		newTokenSecret = null;
		try {
			const t = await stacks.tokens.create(stackID, newTokenName || 'default');
			newTokenSecret = t.secret ?? null;
			newTokenName = '';
			tokens = await stacks.tokens.list(stackID);
		} catch (e) {
			alert((e as Error).message);
		} finally {
			creatingToken = false;
		}
	}

	async function revokeToken(tokenID: string) {
		if (!confirm('Revoke this token? Terraform will stop being able to access state.')) return;
		await stacks.tokens.revoke(stackID, tokenID);
		tokens = tokens.filter((t) => t.id !== tokenID);
	}

	async function attachPolicy() {
		if (!attachingPolicy) return;
		await policies.attach(stackID, attachingPolicy);
		stackPolicies = await policies.forStack(stackID);
		attachingPolicy = '';
	}

	async function detachPolicy(policyID: string) {
		await policies.detach(stackID, policyID);
		stackPolicies = stackPolicies.filter((p) => p.policy_id !== policyID);
	}

	async function saveNotifications(e: SubmitEvent) {
		e.preventDefault();
		savingNotif = true;
		notifSaved = false;
		try {
			const data: { vcs_provider?: string; vcs_base_url?: string; vcs_token?: string; slack_webhook?: string; notify_events: string[] } = {
				notify_events: notifEvents
			};
			if (notifVCSProvider) data.vcs_provider = notifVCSProvider;
			data.vcs_base_url = notifVCSBaseURL; // allow clearing
			if (notifVCSToken !== '') data.vcs_token = notifVCSToken;
			if (notifSlackWebhook !== '') data.slack_webhook = notifSlackWebhook;
			await stacks.notifications.update(stackID, data);
			notifVCSToken = '';
			notifSlackWebhook = '';
			notifSaved = true;
			stack = await stacks.get(stackID);
		} catch (err) {
			alert((err as Error).message);
		} finally {
			savingNotif = false;
		}
	}

	async function saveEnvVar(e: SubmitEvent) {
		e.preventDefault();
		if (!newEnvName.trim() || !newEnvValue.trim()) return;
		savingEnv = true;
		try {
			await stacks.env.upsert(stackID, newEnvName.trim(), newEnvValue.trim());
			envVars = await stacks.env.list(stackID);
			newEnvName = '';
			newEnvValue = '';
		} catch (err) {
			alert((err as Error).message);
		} finally {
			savingEnv = false;
		}
	}

	async function deleteEnvVar(name: string) {
		if (!confirm(`Remove env var "${name}"?`)) return;
		await stacks.env.delete(stackID, name);
		envVars = envVars.filter((v) => v.name !== name);
	}

	async function saveSecretStore(e: SubmitEvent) {
		e.preventDefault();
		if (!secretStoreProvider) return;
		savingSecretStore = true;
		secretStoreSaved = false;
		try {
			let cfg: AWSSecretStoreConfig | HCVaultSecretStoreConfig | BitwardenSecretStoreConfig | VaultwardenSecretStoreConfig;
			if (secretStoreProvider === 'aws_sm') cfg = awsCfg;
			else if (secretStoreProvider === 'hc_vault') cfg = vaultCfg;
			else if (secretStoreProvider === 'bitwarden_sm') cfg = bwCfg;
			else cfg = vwCfg;
			await stacks.secretStore.upsert(stackID, secretStoreProvider, cfg);
			secretStoreSaved = true;
			stack = await stacks.get(stackID);
		} catch (err) {
			alert((err as Error).message);
		} finally {
			savingSecretStore = false;
		}
	}

	async function removeSecretStore() {
		if (!confirm('Remove the external secret store? Secrets will no longer be injected into runs.')) return;
		removingSecretStore = true;
		try {
			await stacks.secretStore.delete(stackID);
			secretStoreProvider = '';
			awsCfg = { region: '', secret_names: [] };
			vaultCfg = { address: '', mount: 'secret', path: '' };
			bwCfg = { access_token: '' };
			vwCfg = { url: '', client_id: '', client_secret: '', email: '', master_password: '' };
			stack = await stacks.get(stackID);
		} catch (err) {
			alert((err as Error).message);
		} finally {
			removingSecretStore = false;
		}
	}

	async function saveStateBackend(e: SubmitEvent) {
		e.preventDefault();
		if (!stateBackendProvider) return;
		savingStateBackend = true;
		stateBackendSaved = false;
		try {
			let cfg: S3StateBackendConfig | GCSStateBackendConfig | AzureStateBackendConfig;
			if (stateBackendProvider === 's3') cfg = s3Cfg;
			else if (stateBackendProvider === 'gcs') cfg = gcsCfg;
			else cfg = azureCfg;
			await stacks.stateBackend.upsert(stackID, stateBackendProvider, cfg);
			stateBackendSaved = true;
			stack = await stacks.get(stackID);
		} catch (err) {
			alert((err as Error).message);
		} finally {
			savingStateBackend = false;
		}
	}

	async function toggleDisabled() {
		if (!stack) return;
		const next = !stack.is_disabled;
		const msg = next
			? 'Disable this stack? Webhook triggers and drift checks will be paused.'
			: 'Re-enable this stack?';
		if (!confirm(msg)) return;
		togglingDisabled = true;
		try {
			stack = await stacks.update(stackID, { is_disabled: next });
		} catch (e) {
			alert((e as Error).message);
		} finally {
			togglingDisabled = false;
		}
	}

	async function rotateWebhookSecret() {
		if (!confirm('Rotate the webhook secret? The old secret will stop working immediately — update your repository webhook settings before the next push.')) return;
		rotatingWebhook = true;
		newWebhookSecret = null;
		try {
			const res = await stacks.webhook.rotateSecret(stackID);
			newWebhookSecret = res.webhook_secret;
		} catch (e) {
			alert((e as Error).message);
		} finally {
			rotatingWebhook = false;
		}
	}

	async function removeStateBackend() {
		if (!confirm('Remove the external state backend? Terraform state will fall back to the built-in MinIO store.')) return;
		removingStateBackend = true;
		try {
			await stacks.stateBackend.delete(stackID);
			stateBackendProvider = '';
			s3Cfg = { region: '', bucket: '' };
			gcsCfg = { bucket: '', service_account_json: '' };
			azureCfg = { account_name: '', account_key: '', container: '' };
			stack = await stacks.get(stackID);
		} catch (err) {
			alert((err as Error).message);
		} finally {
			removingStateBackend = false;
		}
	}

	const unattachedPolicies = $derived(
		allPolicies.filter((p) => !stackPolicies.some((sp) => sp.policy_id === p.id))
	);

	const statusColour: Record<string, string> = {
		queued: 'text-zinc-400',
		preparing: 'text-blue-400',
		planning: 'text-blue-400',
		unconfirmed: 'text-yellow-400',
		confirmed: 'text-blue-400',
		applying: 'text-blue-400',
		finished: 'text-green-400',
		failed: 'text-red-400',
		canceled: 'text-zinc-500',
		discarded: 'text-zinc-500'
	};

	function fmtDate(iso: string) {
		return new Date(iso).toLocaleString();
	}

	function driftScheduleLabel(val: string | undefined) {
		return driftScheduleOptions.find((o) => o.value === (val ?? ''))?.label ?? val ?? '—';
	}
</script>

{#if loading}
	<div class="p-6 text-zinc-500 text-sm">Loading…</div>
{:else if error || !stack}
	<div class="p-6 text-red-400 text-sm">{error ?? 'Stack not found'}</div>
{:else}
<div class="p-6 space-y-6 max-w-3xl">

	<!-- Disabled banner -->
	{#if stack.is_disabled}
		<div class="bg-zinc-800 border border-zinc-700 rounded-xl px-5 py-3 flex items-center justify-between gap-4">
			<p class="text-zinc-300 text-sm">This stack is <span class="font-semibold text-white">disabled</span>. Webhook triggers and drift checks are paused. Manual runs can still be triggered.</p>
		</div>
	{/if}

	<!-- Header -->
	<div class="flex items-start justify-between">
		<div>
			<div class="flex items-center gap-2 text-sm text-zinc-500 mb-1">
				<a href="/stacks" class="hover:text-zinc-300">Stacks</a>
				<span>/</span>
				<span class="text-white font-medium">{stack.name}</span>
			</div>
			<div class="flex items-center gap-2">
				<span class="text-xs px-1.5 py-0.5 rounded font-medium
					{stack.tool === 'opentofu' ? 'bg-violet-900 text-violet-300' :
					 stack.tool === 'terraform' ? 'bg-purple-900 text-purple-300' :
					 stack.tool === 'ansible' ? 'bg-red-900 text-red-300' :
					 'bg-sky-900 text-sky-300'}">
					{stack.tool}
				</span>
				{#if stack.description}
					<span class="text-zinc-400 text-sm">{stack.description}</span>
				{/if}
			</div>
		</div>
		<div class="flex items-center gap-2">
			<button onclick={triggerRun} disabled={triggeringRun}
				class="bg-indigo-600 hover:bg-indigo-500 disabled:opacity-50 text-white text-sm px-3 py-1.5 rounded-lg transition-colors">
				{triggeringRun ? 'Queuing…' : 'Trigger run'}
			</button>
			{#if stack.drift_detection}
				<button onclick={triggerDrift} disabled={triggeringDrift}
					class="border border-zinc-700 hover:border-zinc-500 text-zinc-300 text-sm px-3 py-1.5 rounded-lg transition-colors">
					{triggeringDrift ? 'Queuing…' : 'Drift check'}
				</button>
			{/if}
			<button onclick={() => { editing = !editing; resetForm(); }}
				class="border border-zinc-700 hover:border-zinc-500 text-zinc-300 text-sm px-3 py-1.5 rounded-lg transition-colors">
				{editing ? 'Cancel' : 'Edit'}
			</button>
			<button onclick={toggleDisabled} disabled={togglingDisabled}
				class="border border-zinc-700 hover:border-zinc-500 text-zinc-400 text-sm px-3 py-1.5 rounded-lg transition-colors disabled:opacity-50">
				{togglingDisabled ? '…' : stack.is_disabled ? 'Enable' : 'Disable'}
			</button>
			<button onclick={() => { showDestroyModal = true; destroyConfirmName = ''; }}
				class="border border-orange-900 hover:border-orange-700 text-orange-400 text-sm px-3 py-1.5 rounded-lg transition-colors">
				Destroy infra
			</button>
			<button onclick={deleteStack}
				class="border border-red-900 hover:border-red-700 text-red-400 text-sm px-3 py-1.5 rounded-lg transition-colors">
				Delete
			</button>
		</div>
	</div>

	<!-- Edit form -->
	{#if editing}
	<div class="border border-zinc-800 rounded-xl p-5">
		{#if editError}
			<div class="mb-4 bg-red-950 border border-red-800 rounded-lg px-4 py-3 text-red-300 text-sm">{editError}</div>
		{/if}
		<form onsubmit={saveEdit} class="space-y-4">
			<div class="grid grid-cols-2 gap-4">
				<div class="space-y-1.5">
					<label class="field-label" for="edit-name">Name</label>
					<input id="edit-name" class="field-input" bind:value={form.name} required />
				</div>
				<div class="space-y-1.5">
					<label class="field-label" for="edit-branch">Branch</label>
					<input id="edit-branch" class="field-input font-mono text-sm" bind:value={form.repo_branch} />
				</div>
			</div>
			<div class="space-y-1.5">
				<label class="field-label" for="edit-desc">Description</label>
				<input id="edit-desc" class="field-input" bind:value={form.description} />
			</div>
			<div class="space-y-1.5">
				<label class="field-label" for="edit-root">Project root</label>
				<input id="edit-root" class="field-input font-mono text-sm" bind:value={form.project_root} />
			</div>
			<div class="flex gap-6">
				<label class="flex items-center gap-2 cursor-pointer text-sm text-zinc-300">
					<input type="checkbox" bind:checked={form.auto_apply} /> Auto-apply
				</label>
				<label class="flex items-center gap-2 cursor-pointer text-sm text-zinc-300">
					<input type="checkbox" bind:checked={form.drift_detection} /> Drift detection
				</label>
			</div>
			{#if form.drift_detection}
				<div class="space-y-1.5">
					<label class="field-label" for="edit-schedule">Drift check interval</label>
					<select id="edit-schedule" class="field-input" bind:value={form.drift_schedule}>
						{#each driftScheduleOptions as opt}
							<option value={opt.value}>{opt.label}</option>
						{/each}
					</select>
				</div>
			{/if}
			<div class="flex gap-3 pt-1">
				<button type="submit" disabled={saving}
					class="bg-indigo-600 hover:bg-indigo-500 disabled:opacity-50 text-white text-sm px-4 py-1.5 rounded-lg transition-colors">
					{saving ? 'Saving…' : 'Save changes'}
				</button>
			</div>
		</form>
	</div>
	{/if}

	<!-- Stack details -->
	<div class="border border-zinc-800 rounded-xl divide-y divide-zinc-800 text-sm">
		{#each [
			['Repository', stack.repo_url],
			['Branch', stack.repo_branch],
			['Project root', stack.project_root],
			['Auto-apply', stack.auto_apply ? 'Yes' : 'No'],
			['Drift detection', stack.drift_detection ? 'Yes' : 'No'],
			['Drift interval', stack.drift_detection ? driftScheduleLabel(stack.drift_schedule) : '—'],
			['Created', fmtDate(stack.created_at)]
		] as [label, value]}
			<div class="flex px-4 py-3">
				<span class="w-36 flex-shrink-0 text-zinc-500">{label}</span>
				<span class="text-zinc-200 font-mono text-xs break-all">{value}</span>
			</div>
		{/each}
	</div>

	<!-- Policies -->
	<section class="space-y-3">
		<h2 class="text-sm font-medium text-zinc-400 uppercase tracking-wide">Policies</h2>
		{#if stackPolicies.length > 0}
			<div class="border border-zinc-800 rounded-xl overflow-hidden">
				<table class="w-full text-sm">
					<thead class="bg-zinc-900 text-zinc-500 text-xs uppercase tracking-wide">
						<tr>
							<th class="text-left px-4 py-2">Name</th>
							<th class="text-left px-4 py-2">Type</th>
							<th class="text-left px-4 py-2">Status</th>
							<th class="px-4 py-2"></th>
						</tr>
					</thead>
					<tbody class="divide-y divide-zinc-800">
						{#each stackPolicies as sp (sp.policy_id)}
							<tr>
								<td class="px-4 py-2.5">
									<a href="/policies/{sp.policy_id}" class="text-zinc-200 hover:text-white">{sp.name}</a>
								</td>
								<td class="px-4 py-2.5 text-zinc-500 text-xs">{sp.type}</td>
								<td class="px-4 py-2.5">
									<span class="text-xs {sp.is_active ? 'text-green-400' : 'text-zinc-500'}">
										{sp.is_active ? 'Active' : 'Inactive'}
									</span>
								</td>
								<td class="px-4 py-2.5 text-right">
									<button onclick={() => detachPolicy(sp.policy_id)}
										class="text-xs text-zinc-500 hover:text-red-400">Remove</button>
								</td>
							</tr>
						{/each}
					</tbody>
				</table>
			</div>
		{:else}
			<p class="text-zinc-600 text-sm">No policies attached.</p>
		{/if}

		{#if unattachedPolicies.length > 0}
			<div class="flex items-center gap-2">
				<select class="field-input w-64" bind:value={attachingPolicy}>
					<option value="">— attach a policy —</option>
					{#each unattachedPolicies as p (p.id)}
						<option value={p.id}>{p.name} ({p.type})</option>
					{/each}
				</select>
				<button onclick={attachPolicy} disabled={!attachingPolicy}
					class="border border-zinc-700 hover:border-zinc-500 disabled:opacity-40 text-zinc-300 text-sm px-3 py-1.5 rounded-lg transition-colors">
					Attach
				</button>
			</div>
		{/if}
	</section>

	<!-- Environment variables -->
	<section class="space-y-3">
		<h2 class="text-sm font-medium text-zinc-400 uppercase tracking-wide">Environment variables</h2>
		<p class="text-xs text-zinc-500">Values are encrypted at rest and injected into runner containers. They are write-only — existing values cannot be read back.</p>

		{#if envVars.length > 0}
			<div class="border border-zinc-800 rounded-xl overflow-hidden">
				<table class="w-full text-sm">
					<thead class="bg-zinc-900 text-zinc-500 text-xs uppercase tracking-wide">
						<tr>
							<th class="text-left px-4 py-2">Name</th>
							<th class="text-left px-4 py-2">Last updated</th>
							<th class="px-4 py-2"></th>
						</tr>
					</thead>
					<tbody class="divide-y divide-zinc-800">
						{#each envVars as ev (ev.id)}
							<tr>
								<td class="px-4 py-2.5 font-mono text-xs text-zinc-200">{ev.name}</td>
								<td class="px-4 py-2.5 text-zinc-500 text-xs">{fmtDate(ev.updated_at)}</td>
								<td class="px-4 py-2.5 text-right">
									<button onclick={() => deleteEnvVar(ev.name)}
										class="text-xs text-zinc-500 hover:text-red-400">Remove</button>
								</td>
							</tr>
						{/each}
					</tbody>
				</table>
			</div>
		{:else}
			<p class="text-zinc-600 text-sm">No environment variables set.</p>
		{/if}

		<form onsubmit={saveEnvVar} class="flex items-center gap-2">
			<input id="env-name" class="field-input w-40 font-mono text-xs" bind:value={newEnvName} placeholder="NAME" autocomplete="off" />
			<input id="env-value" class="field-input w-56" type="password" bind:value={newEnvValue} placeholder="value" autocomplete="new-password" />
			<button type="submit" disabled={savingEnv || !newEnvName || !newEnvValue}
				class="border border-zinc-700 hover:border-zinc-500 text-zinc-300 text-sm px-3 py-1.5 rounded-lg transition-colors disabled:opacity-50">
				{savingEnv ? 'Saving…' : 'Set'}
			</button>
		</form>
	</section>

	<!-- Notifications -->
	<section class="space-y-3">
		<h2 class="text-sm font-medium text-zinc-400 uppercase tracking-wide">Notifications</h2>
		<form onsubmit={saveNotifications} class="border border-zinc-800 rounded-xl p-5 space-y-4">
			<div class="grid grid-cols-2 gap-4">
				<div class="space-y-1.5">
					<label class="field-label" for="notif-vcs-provider">VCS provider</label>
					<select id="notif-vcs-provider" class="field-input" bind:value={notifVCSProvider}>
						<option value="github">GitHub</option>
						<option value="gitlab">GitLab</option>
						<option value="gitea">Gitea / Gogs</option>
					</select>
				</div>
				{#if notifVCSProvider === 'gitlab' || notifVCSProvider === 'gitea'}
					<div class="space-y-1.5">
						<label class="field-label" for="notif-vcs-base-url">
							Instance base URL
							{#if notifVCSProvider === 'gitea'}
								<span class="text-zinc-600"> (required)</span>
							{:else}
								<span class="text-zinc-600"> (optional — leave blank for gitlab.com)</span>
							{/if}
						</label>
						<input id="notif-vcs-base-url" class="field-input font-mono text-sm"
							bind:value={notifVCSBaseURL}
							placeholder="https://gitea.example.com" />
					</div>
				{:else}
					<div></div>
				{/if}
				<div class="space-y-1.5">
					<label class="field-label" for="notif-vcs-token">
						VCS token
						{#if stack.has_vcs_token}
							<span class="ml-1 text-green-500 text-xs">● set</span>
						{/if}
					</label>
					<input id="notif-vcs-token" class="field-input" type="password"
						bind:value={notifVCSToken}
						placeholder={stack.has_vcs_token ? 'Enter new value to replace' : 'ghp_… / GitLab PAT / Gitea token'}
						autocomplete="new-password" />
					<p class="text-xs text-zinc-600">Used to post PR comments and set commit status checks.</p>
				</div>
				<div class="space-y-1.5">
					<label class="field-label" for="notif-slack">
						Slack webhook URL
						{#if stack.has_slack_webhook}
							<span class="ml-1 text-green-500 text-xs">● set</span>
						{/if}
					</label>
					<input id="notif-slack" class="field-input" type="password"
						bind:value={notifSlackWebhook}
						placeholder={stack.has_slack_webhook ? 'Enter new value to replace' : 'https://hooks.slack.com/…'}
						autocomplete="new-password" />
				</div>
			</div>

			<div class="space-y-1.5">
				<p class="text-xs text-zinc-400">Slack events to notify on</p>
				<div class="flex gap-4 flex-wrap">
					{#each notifyEventOptions as opt}
						<label class="flex items-center gap-2 cursor-pointer text-sm text-zinc-300">
							<input type="checkbox"
								checked={notifEvents.includes(opt.value)}
								onchange={(e) => {
									if ((e.target as HTMLInputElement).checked) {
										notifEvents = [...notifEvents, opt.value];
									} else {
										notifEvents = notifEvents.filter((v) => v !== opt.value);
									}
								}} />
							{opt.label}
						</label>
					{/each}
				</div>
			</div>

			<div class="flex items-center gap-3">
				<button type="submit" disabled={savingNotif}
					class="bg-indigo-600 hover:bg-indigo-500 disabled:opacity-50 text-white text-sm px-4 py-1.5 rounded-lg transition-colors">
					{savingNotif ? 'Saving…' : 'Save notifications'}
				</button>
				{#if notifSaved}
					<span class="text-xs text-green-400">Saved.</span>
				{/if}
			</div>
		</form>
	</section>

	<!-- External secret store -->
	<section class="space-y-3">
		<h2 class="text-sm font-medium text-zinc-400 uppercase tracking-wide">External secret store</h2>
		<p class="text-xs text-zinc-500">Pull secrets from an external store and inject them into runner containers. Built-in env vars override any same-named external secret.</p>

		<form onsubmit={saveSecretStore} class="border border-zinc-800 rounded-xl p-5 space-y-4">
			<div class="space-y-1.5">
				<label class="field-label" for="ss-provider">
					Provider
					{#if stack.has_secret_store}
						<span class="ml-1 text-green-500 text-xs">● {stack.secret_store_provider}</span>
					{/if}
				</label>
				<select id="ss-provider" class="field-input w-64" bind:value={secretStoreProvider}>
					<option value="">— none —</option>
					<option value="aws_sm">AWS Secrets Manager</option>
					<option value="hc_vault">HashiCorp Vault (KV v2)</option>
					<option value="bitwarden_sm">Bitwarden Secrets Manager</option>
					<option value="vaultwarden">Vaultwarden (self-hosted)</option>
				</select>
			</div>

			{#if secretStoreProvider === 'aws_sm'}
				<div class="grid grid-cols-2 gap-4">
					<div class="space-y-1.5">
						<label class="field-label" for="aws-region">Region</label>
						<input id="aws-region" class="field-input font-mono text-sm" bind:value={awsCfg.region} placeholder="us-east-1" required />
					</div>
					<div class="space-y-1.5">
						<label class="field-label" for="aws-key-id">Access key ID <span class="text-zinc-600">(optional — uses env if omitted)</span></label>
						<input id="aws-key-id" class="field-input font-mono text-sm" type="password" bind:value={awsCfg.access_key_id} placeholder="AKIA…" autocomplete="new-password" />
					</div>
					<div class="col-span-2 space-y-1.5">
						<label class="field-label" for="aws-secret-key">Secret access key</label>
						<input id="aws-secret-key" class="field-input font-mono text-sm" type="password" bind:value={awsCfg.secret_access_key} placeholder="…" autocomplete="new-password" />
					</div>
				</div>
				<div class="space-y-2">
					<p class="text-xs text-zinc-400">Secret names / ARNs to fetch</p>
					{#if awsCfg.secret_names.length > 0}
						<ul class="border border-zinc-800 rounded-lg divide-y divide-zinc-800">
							{#each awsCfg.secret_names as name, i}
								<li class="flex items-center justify-between px-3 py-2 text-xs font-mono text-zinc-300">
									{name}
									<button type="button" onclick={() => { awsCfg.secret_names = awsCfg.secret_names.filter((_, j) => j !== i); }}
										class="text-zinc-500 hover:text-red-400 ml-2">✕</button>
								</li>
							{/each}
						</ul>
					{/if}
					<div class="flex gap-2">
						<input class="field-input font-mono text-xs" bind:value={awsNewSecretName} placeholder="myapp/db_password or ARN" />
						<button type="button" onclick={() => { if (awsNewSecretName.trim()) { awsCfg.secret_names = [...awsCfg.secret_names, awsNewSecretName.trim()]; awsNewSecretName = ''; } }}
							class="border border-zinc-700 hover:border-zinc-500 text-zinc-300 text-sm px-3 py-1.5 rounded-lg transition-colors whitespace-nowrap">
							Add
						</button>
					</div>
				</div>
			{:else if secretStoreProvider === 'hc_vault'}
				<div class="grid grid-cols-2 gap-4">
					<div class="col-span-2 space-y-1.5">
						<label class="field-label" for="vault-addr">Vault address</label>
						<input id="vault-addr" class="field-input font-mono text-sm" bind:value={vaultCfg.address} placeholder="https://vault.example.com" required />
					</div>
					<div class="space-y-1.5">
						<label class="field-label" for="vault-mount">KV mount</label>
						<input id="vault-mount" class="field-input font-mono text-sm" bind:value={vaultCfg.mount} placeholder="secret" />
					</div>
					<div class="space-y-1.5">
						<label class="field-label" for="vault-path">Secret path</label>
						<input id="vault-path" class="field-input font-mono text-sm" bind:value={vaultCfg.path} placeholder="myapp/config" required />
					</div>
					<div class="space-y-1.5">
						<label class="field-label" for="vault-ns">Namespace <span class="text-zinc-600">(HCP Vault)</span></label>
						<input id="vault-ns" class="field-input font-mono text-sm" bind:value={vaultCfg.namespace} placeholder="admin" />
					</div>
				</div>
				<div class="space-y-1.5">
					<p class="text-xs text-zinc-400">Authentication — token or AppRole</p>
					<div class="grid grid-cols-2 gap-4">
						<div class="space-y-1.5">
							<label class="field-label" for="vault-token">Token</label>
							<input id="vault-token" class="field-input font-mono text-sm" type="password" bind:value={vaultCfg.token} placeholder="hvs.…" autocomplete="new-password" />
						</div>
						<div class="space-y-1.5"></div>
						<div class="space-y-1.5">
							<label class="field-label" for="vault-role">AppRole — Role ID</label>
							<input id="vault-role" class="field-input font-mono text-sm" bind:value={vaultCfg.role_id} placeholder="role UUID" />
						</div>
						<div class="space-y-1.5">
							<label class="field-label" for="vault-secret">AppRole — Secret ID</label>
							<input id="vault-secret" class="field-input font-mono text-sm" type="password" bind:value={vaultCfg.secret_id} placeholder="secret UUID" autocomplete="new-password" />
						</div>
					</div>
				</div>
			{:else if secretStoreProvider === 'bitwarden_sm'}
				<div class="grid grid-cols-2 gap-4">
					<div class="col-span-2 space-y-1.5">
						<label class="field-label" for="bw-token">Machine account access token</label>
						<input id="bw-token" class="field-input font-mono text-sm" type="password" bind:value={bwCfg.access_token} placeholder="0.…" autocomplete="new-password" required />
						<p class="text-xs text-zinc-600">Format: <code>0.&lt;serviceAccountId&gt;.&lt;clientSecret&gt;.&lt;encryptionKey&gt;</code></p>
					</div>
					<div class="space-y-1.5">
						<label class="field-label" for="bw-project">Project ID <span class="text-zinc-600">(recommended)</span></label>
						<input id="bw-project" class="field-input font-mono text-sm" bind:value={bwCfg.project_id} placeholder="UUID" />
					</div>
					<div class="space-y-1.5">
						<label class="field-label" for="bw-org">Org ID <span class="text-zinc-600">(fallback if no project)</span></label>
						<input id="bw-org" class="field-input font-mono text-sm" bind:value={bwCfg.org_id} placeholder="UUID" />
					</div>
					<div class="space-y-1.5">
						<label class="field-label" for="bw-api">API URL <span class="text-zinc-600">(self-hosted)</span></label>
						<input id="bw-api" class="field-input font-mono text-sm" bind:value={bwCfg.api_url} placeholder="https://api.bitwarden.com" />
					</div>
					<div class="space-y-1.5">
						<label class="field-label" for="bw-id">Identity URL <span class="text-zinc-600">(self-hosted)</span></label>
						<input id="bw-id" class="field-input font-mono text-sm" bind:value={bwCfg.identity_url} placeholder="https://identity.bitwarden.com" />
					</div>
				</div>
			{:else if secretStoreProvider === 'vaultwarden'}
				<div class="grid grid-cols-2 gap-4">
					<div class="col-span-2 space-y-1.5">
						<label class="field-label" for="vw-url">Vaultwarden URL</label>
						<input id="vw-url" class="field-input font-mono text-sm" bind:value={vwCfg.url} placeholder="https://vault.example.com" required />
					</div>
					<div class="space-y-1.5">
						<label class="field-label" for="vw-client-id">Client ID</label>
						<input id="vw-client-id" class="field-input font-mono text-sm" bind:value={vwCfg.client_id} placeholder="user.&#123;uuid&#125;" required />
					</div>
					<div class="space-y-1.5">
						<label class="field-label" for="vw-client-secret">Client secret</label>
						<input id="vw-client-secret" class="field-input font-mono text-sm" type="password" bind:value={vwCfg.client_secret} autocomplete="new-password" required />
					</div>
					<div class="space-y-1.5">
						<label class="field-label" for="vw-email">Account email</label>
						<input id="vw-email" class="field-input" type="email" bind:value={vwCfg.email} required />
					</div>
					<div class="space-y-1.5">
						<label class="field-label" for="vw-master-pw">Master password</label>
						<input id="vw-master-pw" class="field-input" type="password" bind:value={vwCfg.master_password} autocomplete="new-password" required />
					</div>
					<div class="col-span-2 space-y-1.5">
						<label class="field-label" for="vw-folder">Folder name <span class="text-zinc-600">(optional — leave blank for all SecureNote items)</span></label>
						<input id="vw-folder" class="field-input" bind:value={vwCfg.folder_name} placeholder="crucible-secrets" />
					</div>
				</div>
				<p class="text-xs text-zinc-600">Each SecureNote's decrypted name becomes the env var key; its notes field becomes the value. Credentials are encrypted at rest.</p>
			{/if}

			{#if secretStoreProvider}
				<div class="flex items-center gap-3">
					<button type="submit" disabled={savingSecretStore}
						class="bg-indigo-600 hover:bg-indigo-500 disabled:opacity-50 text-white text-sm px-4 py-1.5 rounded-lg transition-colors">
						{savingSecretStore ? 'Saving…' : 'Save secret store'}
					</button>
					{#if stack.has_secret_store}
						<button type="button" onclick={removeSecretStore} disabled={removingSecretStore}
							class="border border-red-900 hover:border-red-700 text-red-400 text-sm px-3 py-1.5 rounded-lg transition-colors disabled:opacity-50">
							{removingSecretStore ? 'Removing…' : 'Remove'}
						</button>
					{/if}
					{#if secretStoreSaved}
						<span class="text-xs text-green-400">Saved.</span>
					{/if}
				</div>
			{/if}
		</form>
	</section>

	<!-- External state backend -->
	<section class="space-y-3">
		<h2 class="text-sm font-medium text-zinc-400 uppercase tracking-wide">External state backend</h2>
		<p class="text-xs text-zinc-500">Override the built-in MinIO state storage with an external backend. Only applies if you are not using the HTTP backend built into Crucible.</p>

		<form onsubmit={saveStateBackend} class="border border-zinc-800 rounded-xl p-5 space-y-4">
			<div class="space-y-1.5">
				<label class="field-label" for="sb-provider">
					Provider
					{#if stack.has_state_backend}
						<span class="ml-1 text-green-500 text-xs">● {stack.state_backend_provider}</span>
					{/if}
				</label>
				<select id="sb-provider" class="field-input w-64" bind:value={stateBackendProvider}>
					<option value="">— use built-in MinIO —</option>
					<option value="s3">Amazon S3 / S3-compatible</option>
					<option value="gcs">Google Cloud Storage</option>
					<option value="azurerm">Azure Blob Storage</option>
				</select>
			</div>

			{#if stateBackendProvider === 's3'}
				<div class="grid grid-cols-2 gap-4">
					<div class="space-y-1.5">
						<label class="field-label" for="s3-region">Region</label>
						<input id="s3-region" class="field-input font-mono text-sm" bind:value={s3Cfg.region} placeholder="us-east-1" required />
					</div>
					<div class="space-y-1.5">
						<label class="field-label" for="s3-bucket">Bucket</label>
						<input id="s3-bucket" class="field-input font-mono text-sm" bind:value={s3Cfg.bucket} placeholder="my-tf-state" required />
					</div>
					<div class="space-y-1.5">
						<label class="field-label" for="s3-prefix">Key prefix <span class="text-zinc-600">(optional)</span></label>
						<input id="s3-prefix" class="field-input font-mono text-sm" bind:value={s3Cfg.key_prefix} placeholder="stacks/" />
					</div>
					<div class="space-y-1.5">
						<label class="field-label" for="s3-endpoint">Endpoint URL <span class="text-zinc-600">(S3-compatible / MinIO)</span></label>
						<input id="s3-endpoint" class="field-input font-mono text-sm" bind:value={s3Cfg.endpoint_url} placeholder="https://minio.example.com" />
					</div>
					<div class="space-y-1.5">
						<label class="field-label" for="s3-key-id">Access key ID <span class="text-zinc-600">(optional)</span></label>
						<input id="s3-key-id" class="field-input font-mono text-sm" type="password" bind:value={s3Cfg.access_key_id} placeholder="AKIA…" autocomplete="new-password" />
					</div>
					<div class="space-y-1.5">
						<label class="field-label" for="s3-secret-key">Secret access key</label>
						<input id="s3-secret-key" class="field-input font-mono text-sm" type="password" bind:value={s3Cfg.secret_access_key} autocomplete="new-password" />
					</div>
				</div>
			{:else if stateBackendProvider === 'gcs'}
				<div class="space-y-4">
					<div class="grid grid-cols-2 gap-4">
						<div class="space-y-1.5">
							<label class="field-label" for="gcs-bucket">Bucket</label>
							<input id="gcs-bucket" class="field-input font-mono text-sm" bind:value={gcsCfg.bucket} placeholder="my-tf-state" required />
						</div>
						<div class="space-y-1.5">
							<label class="field-label" for="gcs-prefix">Key prefix <span class="text-zinc-600">(optional)</span></label>
							<input id="gcs-prefix" class="field-input font-mono text-sm" bind:value={gcsCfg.key_prefix} placeholder="stacks/" />
						</div>
					</div>
					<div class="space-y-1.5">
						<label class="field-label" for="gcs-sa">Service account JSON</label>
						<textarea id="gcs-sa" class="field-input font-mono text-xs h-32 resize-y"
							bind:value={gcsCfg.service_account_json}
							placeholder='&#123;"type":"service_account","project_id":"…"&#125;' required></textarea>
						<p class="text-xs text-zinc-600">Paste the full service account key JSON downloaded from Google Cloud Console.</p>
					</div>
				</div>
			{:else if stateBackendProvider === 'azurerm'}
				<div class="grid grid-cols-2 gap-4">
					<div class="space-y-1.5">
						<label class="field-label" for="az-account">Storage account name</label>
						<input id="az-account" class="field-input font-mono text-sm" bind:value={azureCfg.account_name} placeholder="mystorageaccount" required />
					</div>
					<div class="space-y-1.5">
						<label class="field-label" for="az-container">Container</label>
						<input id="az-container" class="field-input font-mono text-sm" bind:value={azureCfg.container} placeholder="tfstate" required />
					</div>
					<div class="col-span-2 space-y-1.5">
						<label class="field-label" for="az-key">Account key</label>
						<input id="az-key" class="field-input font-mono text-sm" type="password" bind:value={azureCfg.account_key} autocomplete="new-password" required />
					</div>
					<div class="space-y-1.5">
						<label class="field-label" for="az-prefix">Key prefix <span class="text-zinc-600">(optional)</span></label>
						<input id="az-prefix" class="field-input font-mono text-sm" bind:value={azureCfg.key_prefix} placeholder="stacks/" />
					</div>
				</div>
			{/if}

			{#if stateBackendProvider}
				<div class="flex items-center gap-3">
					<button type="submit" disabled={savingStateBackend}
						class="bg-indigo-600 hover:bg-indigo-500 disabled:opacity-50 text-white text-sm px-4 py-1.5 rounded-lg transition-colors">
						{savingStateBackend ? 'Saving…' : 'Save state backend'}
					</button>
					{#if stack.has_state_backend}
						<button type="button" onclick={removeStateBackend} disabled={removingStateBackend}
							class="border border-red-900 hover:border-red-700 text-red-400 text-sm px-3 py-1.5 rounded-lg transition-colors disabled:opacity-50">
							{removingStateBackend ? 'Removing…' : 'Remove'}
						</button>
					{/if}
					{#if stateBackendSaved}
						<span class="text-xs text-green-400">Saved.</span>
					{/if}
				</div>
			{/if}
		</form>
	</section>

	<!-- Webhooks -->
	<section class="space-y-3">
		<h2 class="text-sm font-medium text-zinc-400 uppercase tracking-wide">Webhooks</h2>
		<p class="text-xs text-zinc-500">
			Point your GitHub, GitLab, or Gitea repository webhook at the URL below.
			Set the content type to <code class="text-zinc-300">application/json</code> and paste the secret.
			Push events trigger tracked runs; pull-request/merge-request events trigger proposed runs.
		</p>

		<div class="bg-zinc-900 border border-zinc-800 rounded-xl p-4 space-y-3">
			<div class="space-y-1">
				<p class="text-xs text-zinc-500 uppercase tracking-wide">Webhook URL</p>
				<div class="flex items-center gap-2">
					<code class="flex-1 text-xs text-zinc-200 break-all">{window?.location?.origin ?? ''}/api/v1/webhooks/{stackID}</code>
					<button
						onclick={() => navigator.clipboard.writeText(`${window?.location?.origin ?? ''}/api/v1/webhooks/${stackID}`)}
						class="shrink-0 text-xs text-zinc-500 hover:text-zinc-200 border border-zinc-700 hover:border-zinc-500 px-2 py-1 rounded transition-colors">
						Copy
					</button>
				</div>
			</div>

			<div class="space-y-1">
				<p class="text-xs text-zinc-500 uppercase tracking-wide">Secret</p>
				{#if newWebhookSecret}
					<div class="bg-yellow-950 border border-yellow-800 rounded-lg p-3 space-y-2">
						<p class="text-yellow-300 text-xs font-medium">New secret — copy it now and update your repository webhook. It won't be shown again.</p>
						<div class="flex items-center gap-2">
							<code class="flex-1 text-xs text-yellow-200 break-all font-mono">{newWebhookSecret}</code>
							<button
								onclick={() => navigator.clipboard.writeText(newWebhookSecret!)}
								class="shrink-0 text-xs text-zinc-400 hover:text-zinc-200 border border-zinc-700 hover:border-zinc-500 px-2 py-1 rounded transition-colors">
								Copy
							</button>
						</div>
						<button onclick={() => (newWebhookSecret = null)} class="text-xs text-yellow-600 hover:text-yellow-400">Dismiss</button>
					</div>
				{:else}
					<p class="text-xs text-zinc-600 italic">Kept secret. Rotate to generate a new one.</p>
				{/if}
			</div>
		</div>

		<button onclick={rotateWebhookSecret} disabled={rotatingWebhook}
			class="border border-zinc-700 hover:border-zinc-500 text-zinc-300 text-sm px-3 py-1.5 rounded-lg transition-colors disabled:opacity-50">
			{rotatingWebhook ? 'Rotating…' : 'Rotate secret'}
		</button>
	</section>

	<!-- Recent runs -->
	<section class="space-y-3">
		<h2 class="text-sm font-medium text-zinc-400 uppercase tracking-wide">Recent runs</h2>
		{#if recentRuns.length === 0}
			<p class="text-zinc-600 text-sm">No runs yet.</p>
		{:else}
			<div class="border border-zinc-800 rounded-xl overflow-hidden">
				<table class="w-full text-sm">
					<thead class="bg-zinc-900 text-zinc-500 text-xs uppercase tracking-wide">
						<tr>
							<th class="text-left px-4 py-2">Status</th>
							<th class="text-left px-4 py-2">Type</th>
							<th class="text-left px-4 py-2">Plan</th>
							<th class="text-left px-4 py-2">Trigger</th>
							<th class="text-left px-4 py-2">Queued</th>
						</tr>
					</thead>
					<tbody class="divide-y divide-zinc-800">
						{#each recentRuns as run (run.id)}
							<tr class="hover:bg-zinc-900/50 transition-colors">
								<td class="px-4 py-2.5">
									<a href="/runs/{run.id}" class="font-medium {statusColour[run.status] ?? 'text-zinc-400'}">
										{run.status}
									</a>
								</td>
								<td class="px-4 py-2.5 {run.type === 'destroy' ? 'text-orange-400 font-medium' : 'text-zinc-400'}">
									{run.type}{#if run.is_drift} <span class="text-xs text-amber-500">drift</span>{/if}
									{#if run.pr_number}
										<a href={run.pr_url} target="_blank" rel="noopener"
											class="ml-1 text-xs text-blue-400 hover:text-blue-300">#{run.pr_number}</a>
									{/if}
								</td>
								<td class="px-4 py-2.5 text-xs font-mono">
									{#if run.plan_add != null}
										<span class="text-green-400">+{run.plan_add}</span>
										<span class="text-yellow-400 ml-1">~{run.plan_change}</span>
										<span class="text-red-400 ml-1">-{run.plan_destroy}</span>
									{:else}
										<span class="text-zinc-600">—</span>
									{/if}
								</td>
								<td class="px-4 py-2.5 text-zinc-500">{run.trigger}</td>
								<td class="px-4 py-2.5 text-zinc-500 text-xs">{fmtDate(run.queued_at)}</td>
							</tr>
						{/each}
					</tbody>
				</table>
			</div>
		{/if}
	</section>

	<!-- State backend / tokens -->
	<section class="space-y-3">
		<h2 class="text-sm font-medium text-zinc-400 uppercase tracking-wide">State backend</h2>
		<div class="bg-zinc-900 border border-zinc-800 rounded-xl p-4 font-mono text-xs text-zinc-300 space-y-0.5">
			<div><span class="text-zinc-500">terraform &#123;</span></div>
			<div class="pl-4"><span class="text-zinc-500">backend "http" &#123;</span></div>
			<div class="pl-8">address  = <span class="text-green-400">"{window?.location?.origin ?? 'https://your-domain'}/api/v1/state/{stackID}"</span></div>
			<div class="pl-8">username = <span class="text-yellow-400">"TOKEN_ID"</span></div>
			<div class="pl-8">password = <span class="text-yellow-400">"TOKEN_SECRET"</span></div>
			<div class="pl-4"><span class="text-zinc-500">&#125;</span></div>
			<div><span class="text-zinc-500">&#125;</span></div>
		</div>

		{#if newTokenSecret}
			<div class="bg-green-950 border border-green-800 rounded-xl p-4 space-y-2">
				<p class="text-green-300 text-sm font-medium">Token created — copy the secret now, it won't be shown again.</p>
				<code class="block text-xs text-green-200 break-all">{newTokenSecret}</code>
				<button onclick={() => (newTokenSecret = null)} class="text-xs text-green-500 hover:text-green-300">Dismiss</button>
			</div>
		{/if}

		{#if tokens.length > 0}
			<div class="border border-zinc-800 rounded-xl overflow-hidden">
				<table class="w-full text-sm">
					<thead class="bg-zinc-900 text-zinc-500 text-xs uppercase tracking-wide">
						<tr>
							<th class="text-left px-4 py-2">Name</th>
							<th class="text-left px-4 py-2">Token ID</th>
							<th class="text-left px-4 py-2">Last used</th>
							<th class="px-4 py-2"></th>
						</tr>
					</thead>
					<tbody class="divide-y divide-zinc-800">
						{#each tokens as token (token.id)}
							<tr>
								<td class="px-4 py-2.5 text-zinc-300">{token.name}</td>
								<td class="px-4 py-2.5 font-mono text-xs text-zinc-500">{token.id}</td>
								<td class="px-4 py-2.5 text-zinc-500 text-xs">{token.last_used ? fmtDate(token.last_used) : '—'}</td>
								<td class="px-4 py-2.5 text-right">
									<button onclick={() => revokeToken(token.id)}
										class="text-xs text-red-500 hover:text-red-300">Revoke</button>
								</td>
							</tr>
						{/each}
					</tbody>
				</table>
			</div>
		{/if}

		<form onsubmit={createToken} class="flex items-center gap-2">
			<input class="field-input w-48" bind:value={newTokenName} placeholder="Token name" />
			<button type="submit" disabled={creatingToken}
				class="border border-zinc-700 hover:border-zinc-500 text-zinc-300 text-sm px-3 py-1.5 rounded-lg transition-colors disabled:opacity-50">
				{creatingToken ? 'Creating…' : 'New token'}
			</button>
		</form>
	</section>

</div>
{/if}

<!-- Destroy confirmation modal -->
{#if showDestroyModal && stack}
<div class="fixed inset-0 z-50 flex items-center justify-center bg-black/70 px-4">
	<div class="bg-zinc-900 border border-orange-900 rounded-2xl p-6 w-full max-w-md space-y-4 shadow-2xl">
		<div class="space-y-1">
			<h2 class="text-white font-semibold text-base">Destroy infrastructure?</h2>
			<p class="text-zinc-400 text-sm">
				This will run <code class="text-orange-300">tofu destroy</code> on <span class="text-white font-medium">{stack.name}</span>.
				A plan will be generated and you'll confirm before anything is deleted.
			</p>
		</div>
		<div class="bg-orange-950/50 border border-orange-900 rounded-lg px-4 py-3 text-orange-300 text-xs space-y-1">
			<p class="font-semibold">This will permanently destroy all managed infrastructure.</p>
			<p>You will review the plan before the destroy is executed.</p>
		</div>
		<div class="space-y-1.5">
			<label class="text-xs text-zinc-400" for="destroy-confirm">
				Type <span class="font-mono text-white">{stack.name}</span> to confirm
			</label>
			<input
				id="destroy-confirm"
				class="field-input"
				bind:value={destroyConfirmName}
				placeholder={stack.name}
				autocomplete="off"
			/>
		</div>
		<div class="flex gap-3 pt-1">
			<button
				onclick={triggerDestroy}
				disabled={destroyConfirmName !== stack.name || triggeringDestroy}
				class="flex-1 bg-orange-700 hover:bg-orange-600 disabled:opacity-40 disabled:cursor-not-allowed text-white text-sm px-4 py-2 rounded-lg transition-colors font-medium">
				{triggeringDestroy ? 'Queuing…' : 'Queue destroy run'}
			</button>
			<button
				onclick={() => { showDestroyModal = false; destroyConfirmName = ''; }}
				class="border border-zinc-700 hover:border-zinc-500 text-zinc-300 text-sm px-4 py-2 rounded-lg transition-colors">
				Cancel
			</button>
		</div>
	</div>
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
