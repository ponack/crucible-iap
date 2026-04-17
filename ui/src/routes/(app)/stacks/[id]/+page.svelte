<script lang="ts">
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { onMount } from 'svelte';
	import { stacks, runs, policies, integrations, varSets, deps, stackMembers, org, type Stack, type Run, type StackToken, type Policy, type StackPolicyRef, type StackEnvVar, type Integration, type StateBackendProvider, type S3StateBackendConfig, type GCSStateBackendConfig, type AzureStateBackendConfig, type RemoteStateSource, type WebhookDelivery, type VarSet, type StackVarSetRef, type StateResource, type StackDep, type StackMember, type OrgMember } from '$lib/api/client';
	import { triggerBadge } from '$lib/trigger';
	import { auth } from '$lib/stores/auth.svelte';

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
		name: '', description: '', repo_url: '', repo_branch: '', project_root: '',
		auto_apply: false, drift_detection: false, drift_schedule: '', auto_remediate_drift: false,
		scheduled_destroy_at: ''
	});

	// Token creation
	let newTokenName = $state('');
	let creatingToken = $state(false);
	let newTokenSecret = $state<string | null>(null);

	// Run creation
	let triggeringRun = $state(false);
	let triggeringDrift = $state(false);
	let showOverrides = $state(false);
	let overrides = $state<{ key: string; value: string }[]>([]);
	let newOverrideKey = $state('');
	let newOverrideValue = $state('');

	// Destroy modal
	let showDestroyModal = $state(false);
	let destroyConfirmName = $state('');
	let triggeringDestroy = $state(false);

	// Force unlock
	let forcingUnlock = $state(false);

	// Policy attachment
	let attachingPolicy = $state('');

	// Env vars
	let envVars = $state<StackEnvVar[]>([]);
	let newEnvName = $state('');
	let newEnvValue = $state('');
	let newEnvSecret = $state(true);
	let savingEnv = $state(false);

	// Notifications
	let notifVCSToken = $state('');
	let notifSlackWebhook = $state('');
	let notifGotifyURL = $state('');
	let notifGotifyToken = $state('');
	let notifNtfyURL = $state('');
	let notifNtfyToken = $state('');
	let notifEmail = $state('');
	let notifEvents = $state<string[]>([]);
	let savingNotif = $state(false);
	let notifSaved = $state(false);
	let testingSlack = $state(false);
	let slackTestResult = $state<{ ok: boolean; msg: string } | null>(null);
	let testingGotify = $state(false);
	let gotifyTestResult = $state<{ ok: boolean; msg: string } | null>(null);
	let testingNtfy = $state(false);
	let ntfyTestResult = $state<{ ok: boolean; msg: string } | null>(null);
	let testingEmail = $state(false);
	let emailTestResult = $state<{ ok: boolean; msg: string } | null>(null);

	// Org integrations (for assignment to this stack)
	let orgIntegrations = $state<Integration[]>([]);
	let selectedVCSIntegration = $state<string>('');
	let selectedSecretIntegration = $state<string>('');
	let savingIntegrations = $state(false);
	let integrationsSaved = $state(false);

	// Notifications VCS provider
	let notifVCSProvider = $state('');
	let notifVCSBaseURL = $state('');

	// Webhook
	let rotatingWebhook = $state(false);
	let newWebhookSecret = $state<string | null>(null);
	let webhookDeliveries = $state<WebhookDelivery[]>([]);
	let loadingDeliveries = $state(false);

	// Variable sets
	let stackVarSets = $state<StackVarSetRef[]>([]);
	let allVarSets = $state<VarSet[]>([]);
	let attachingVarSet = $state('');

	// Dependencies
	let upstreamDeps = $state<StackDep[]>([]);
	let downstreamDeps = $state<StackDep[]>([]);
	let addingDownstream = $state('');
	let depsError = $state<string | null>(null);

	// Disable/enable
	let togglingDisabled = $state(false);

	// Remote state sources
	let remoteSources = $state<RemoteStateSource[]>([]);
	let addingRemoteSource = $state('');
	let addingRemoteSourceError = $state<string | null>(null);
	let allStacksList = $state<{ id: string; name: string }[]>([]);

	// Resource explorer
	let stateResources = $state<StateResource[]>([]);
	let resourceFilter = $state('');

	// Access / stack members
	let members = $state<StackMember[]>([]);
	let orgUsers = $state<OrgMember[]>([]);
	let addMemberUserID = $state('');
	let addMemberRole = $state<'viewer' | 'approver'>('viewer');
	let addingMember = $state(false);

	// Module publishing
	let moduleNamespace = $state('');
	let moduleName = $state('');
	let moduleProvider = $state('aws');
	let savingModule = $state(false);
	let moduleSaved = $state(false);

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
			const [stackRes, runsRes, tokensRes, stackPoliciesRes, allPoliciesRes, envVarsRes, remoteSourcesRes, allStacksRes, integrationsRes, stackVarSetsRes, allVarSetsRes, upstreamRes, downstreamRes, membersRes, orgUsersRes] = await Promise.all([
				stacks.get(stackID),
				runs.list(stackID),
				stacks.tokens.list(stackID),
				policies.forStack(stackID),
				policies.list(),
				stacks.env.list(stackID),
				stacks.remoteState.list(stackID),
				stacks.list(0, 200),
				integrations.list(),
				varSets.forStack(stackID),
				varSets.list(),
				deps.upstream(stackID),
				deps.downstream(stackID),
				stackMembers.list(stackID),
				org.members.list()
			]);
			stack = stackRes;
			recentRuns = runsRes.data;
			tokens = tokensRes;
			stackPolicies = stackPoliciesRes;
			allPolicies = allPoliciesRes;
			envVars = envVarsRes;
			remoteSources = remoteSourcesRes;
			allStacksList = allStacksRes.data.filter(s => s.id !== stackID).map(s => ({ id: s.id, name: s.name }));
			orgIntegrations = integrationsRes;
			stackVarSets = stackVarSetsRes;
			allVarSets = allVarSetsRes;
			upstreamDeps = upstreamRes;
			downstreamDeps = downstreamRes;
			members = membersRes;
			orgUsers = orgUsersRes;
			selectedVCSIntegration = stackRes.vcs_integration_id ?? '';
			selectedSecretIntegration = stackRes.secret_integration_id ?? '';
			if (stackRes.state_backend_provider) {
				stateBackendProvider = stackRes.state_backend_provider as StateBackendProvider;
			}
			notifVCSProvider = stackRes.vcs_provider ?? 'github';
			notifVCSBaseURL = stackRes.vcs_base_url ?? '';
			moduleNamespace = stackRes.module_namespace ?? '';
			moduleName = stackRes.module_name ?? '';
			moduleProvider = stackRes.module_provider ?? 'aws';
			resetForm();
		} catch (e) {
			error = (e as Error).message;
		} finally {
			loading = false;
		}

		// Load webhook deliveries independently so a failure doesn't block the page.
		loadingDeliveries = true;
		try {
			const deliveriesRes = await stacks.webhook.deliveries(stackID);
			webhookDeliveries = deliveriesRes.data;
		} catch {
			// Non-fatal; deliveries section will just be empty.
		} finally {
			loadingDeliveries = false;
		}

		// Load state resources independently — no state yet is normal.
		stacks.state.resources(stackID).then(r => (stateResources = r)).catch(() => {});
	});

	function resetForm() {
		if (!stack) return;
		form = {
			name: stack.name,
			description: stack.description ?? '',
			repo_url: stack.repo_url,
			repo_branch: stack.repo_branch,
			project_root: stack.project_root,
			auto_apply: stack.auto_apply,
			drift_detection: stack.drift_detection,
			drift_schedule: stack.drift_schedule ?? '',
			auto_remediate_drift: stack.auto_remediate_drift,
			scheduled_destroy_at: stack.scheduled_destroy_at
				? stack.scheduled_destroy_at.slice(0, 16)
				: ''
		};
		notifEvents = [...(stack.notify_events ?? [])];
		notifGotifyURL = stack.gotify_url ?? '';
		notifNtfyURL = stack.ntfy_url ?? '';
		notifEmail = stack.notify_email ?? '';
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
		try {
			await stacks.delete(stackID);
			goto('/stacks');
		} catch (e) {
			alert((e as Error).message);
		}
	}

	async function forceUnlock() {
		if (!confirm('Force-unlock state? Only do this if the run that held the lock has already stopped.')) return;
		forcingUnlock = true;
		try {
			await stacks.state.forceUnlock(stackID);
			stateResources = await stacks.state.resources(stackID);
		} catch (e) {
			alert((e as Error).message);
		} finally {
			forcingUnlock = false;
		}
	}

	async function triggerRun() {
		triggeringRun = true;
		try {
			const run = await runs.create(stackID, 'tracked', overrides);
			goto(`/runs/${run.id}`);
		} catch (e) {
			alert((e as Error).message);
			triggeringRun = false;
		}
	}

	function addOverride() {
		const key = newOverrideKey.trim();
		const value = newOverrideValue.trim();
		if (!key) return;
		// Replace existing key if duplicate
		const idx = overrides.findIndex((o) => o.key === key);
		if (idx >= 0) {
			overrides[idx] = { key, value };
		} else {
			overrides = [...overrides, { key, value }];
		}
		newOverrideKey = '';
		newOverrideValue = '';
	}

	function removeOverride(key: string) {
		overrides = overrides.filter((o) => o.key !== key);
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
		try {
			await stacks.tokens.revoke(stackID, tokenID);
			tokens = tokens.filter((t) => t.id !== tokenID);
		} catch (e) {
			alert((e as Error).message);
		}
	}

	async function attachPolicy() {
		if (!attachingPolicy) return;
		try {
			await policies.attach(stackID, attachingPolicy);
			stackPolicies = await policies.forStack(stackID);
			attachingPolicy = '';
		} catch (e) {
			alert((e as Error).message);
		}
	}

	async function detachPolicy(policyID: string) {
		try {
			await policies.detach(stackID, policyID);
			stackPolicies = stackPolicies.filter((p) => p.policy_id !== policyID);
		} catch (e) {
			alert((e as Error).message);
		}
	}

	async function saveNotifications(e: SubmitEvent) {
		e.preventDefault();
		savingNotif = true;
		notifSaved = false;
		try {
			const data: { vcs_provider?: string; vcs_base_url?: string; vcs_token?: string; slack_webhook?: string; gotify_url?: string; gotify_token?: string; ntfy_url?: string; ntfy_token?: string; notify_email?: string; notify_events: string[] } = {
				notify_events: notifEvents
			};
			if (notifVCSProvider) data.vcs_provider = notifVCSProvider;
			data.vcs_base_url = notifVCSBaseURL; // allow clearing
			if (notifVCSToken !== '') data.vcs_token = notifVCSToken;
			if (notifSlackWebhook !== '') data.slack_webhook = notifSlackWebhook;
			// Send Gotify URL always (allows clearing); send token only if provided
			data.gotify_url = notifGotifyURL;
			if (notifGotifyToken !== '') data.gotify_token = notifGotifyToken;
			data.ntfy_url = notifNtfyURL; // allow clearing
			if (notifNtfyToken !== '') data.ntfy_token = notifNtfyToken;
			data.notify_email = notifEmail; // allow clearing
			await stacks.notifications.update(stackID, data);
			notifVCSToken = '';
			notifSlackWebhook = '';
			notifGotifyToken = '';
			notifNtfyToken = '';
			notifSaved = true;
			stack = await stacks.get(stackID);
		} catch (err) {
			alert((err as Error).message);
		} finally {
			savingNotif = false;
		}
	}

	async function testSlack() {
		testingSlack = true;
		slackTestResult = null;
		try {
			await stacks.notifications.test(stackID);
			slackTestResult = { ok: true, msg: 'Test message sent — check your Slack channel.' };
		} catch (e) {
			slackTestResult = { ok: false, msg: (e as Error).message };
		} finally {
			testingSlack = false;
		}
	}

	async function testGotify() {
		testingGotify = true;
		gotifyTestResult = null;
		try {
			await stacks.notifications.testGotify(stackID);
			gotifyTestResult = { ok: true, msg: 'Test message sent — check your Gotify app.' };
		} catch (e) {
			gotifyTestResult = { ok: false, msg: (e as Error).message };
		} finally {
			testingGotify = false;
		}
	}

	async function testNtfy() {
		testingNtfy = true;
		ntfyTestResult = null;
		try {
			await stacks.notifications.testNtfy(stackID);
			ntfyTestResult = { ok: true, msg: 'Test message sent — check your ntfy app.' };
		} catch (e) {
			ntfyTestResult = { ok: false, msg: (e as Error).message };
		} finally {
			testingNtfy = false;
		}
	}

	async function testEmail() {
		testingEmail = true;
		emailTestResult = null;
		try {
			await stacks.notifications.testEmail(stackID);
			emailTestResult = { ok: true, msg: 'Test email sent — check your inbox.' };
		} catch (e) {
			emailTestResult = { ok: false, msg: (e as Error).message };
		} finally {
			testingEmail = false;
		}
	}

	async function saveEnvVar(e: SubmitEvent) {
		e.preventDefault();
		if (!newEnvName.trim() || !newEnvValue.trim()) return;
		savingEnv = true;
		try {
			await stacks.env.upsert(stackID, newEnvName.trim(), newEnvValue.trim(), newEnvSecret);
			envVars = await stacks.env.list(stackID);
			newEnvName = '';
			newEnvValue = '';
			newEnvSecret = true;
		} catch (err) {
			alert((err as Error).message);
		} finally {
			savingEnv = false;
		}
	}

	async function deleteEnvVar(name: string) {
		if (!confirm(`Remove env var "${name}"?`)) return;
		try {
			await stacks.env.delete(stackID, name);
			envVars = envVars.filter((v) => v.name !== name);
		} catch (e) {
			alert((e as Error).message);
		}
	}

	async function setIntegrations() {
		savingIntegrations = true;
		integrationsSaved = false;
		try {
			await stacks.integrations.set(
				stackID,
				selectedVCSIntegration || null,
				selectedSecretIntegration || null
			);
			integrationsSaved = true;
			stack = await stacks.get(stackID);
		} catch (err) {
			alert((err as Error).message);
		} finally {
			savingIntegrations = false;
		}
	}

	async function saveModuleConfig(e: SubmitEvent) {
		e.preventDefault();
		savingModule = true;
		moduleSaved = false;
		try {
			stack = await stacks.update(stackID, {
				module_namespace: moduleNamespace,
				module_name: moduleName,
				module_provider: moduleProvider,
			});
			moduleSaved = true;
			setTimeout(() => (moduleSaved = false), 2000);
		} catch (e) {
			editError = (e as Error).message;
		} finally {
			savingModule = false;
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

	async function addRemoteSource() {
		if (!addingRemoteSource) return;
		addingRemoteSourceError = null;
		try {
			await stacks.remoteState.add(stackID, addingRemoteSource);
			remoteSources = await stacks.remoteState.list(stackID);
			addingRemoteSource = '';
		} catch (e) {
			addingRemoteSourceError = (e as Error).message;
		}
	}

	async function removeRemoteSource(sourceID: string) {
		if (!confirm('Remove this remote state source? Runs that reference it will fail.')) return;
		try {
			await stacks.remoteState.remove(stackID, sourceID);
			remoteSources = remoteSources.filter((r) => r.id !== sourceID);
		} catch (e) {
			alert((e as Error).message);
		}
	}

	async function upsertMember() {
		if (!addMemberUserID) return;
		addingMember = true;
		try {
			await stackMembers.upsert(stackID, addMemberUserID, addMemberRole);
			members = await stackMembers.list(stackID);
			addMemberUserID = '';
		} catch (e) {
			alert((e as Error).message);
		} finally {
			addingMember = false;
		}
	}

	async function removeMember(userID: string) {
		try {
			await stackMembers.remove(stackID, userID);
			members = members.filter((m) => m.user_id !== userID);
		} catch (e) {
			alert((e as Error).message);
		}
	}

	async function updateMemberRole(userID: string, role: 'viewer' | 'approver') {
		try {
			await stackMembers.upsert(stackID, userID, role);
			members = members.map((m) => m.user_id === userID ? { ...m, role } : m);
		} catch (e) {
			alert((e as Error).message);
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

	async function attachVarSet() {
		if (!attachingVarSet) return;
		try {
			await varSets.attachToStack(stackID, attachingVarSet);
			const updated = await varSets.forStack(stackID);
			stackVarSets = updated;
			attachingVarSet = '';
		} catch (e) {
			alert((e as Error).message);
		}
	}

	async function detachVarSet(vsID: string) {
		try {
			await varSets.detachFromStack(stackID, vsID);
			stackVarSets = stackVarSets.filter((s) => s.id !== vsID);
		} catch (e) {
			alert((e as Error).message);
		}
	}

	async function addDownstreamDep() {
		if (!addingDownstream) return;
		depsError = null;
		try {
			await deps.addDownstream(stackID, addingDownstream);
			downstreamDeps = await deps.downstream(stackID);
			addingDownstream = '';
		} catch (e) {
			depsError = (e as Error).message;
		}
	}

	async function removeDownstreamDep(downstreamID: string) {
		depsError = null;
		try {
			await deps.removeDownstream(stackID, downstreamID);
			downstreamDeps = downstreamDeps.filter((d) => d.id !== downstreamID);
		} catch (e) {
			depsError = (e as Error).message;
		}
	}

	async function removeUpstreamDep(upstreamID: string) {
		depsError = null;
		try {
			// Remove from the upstream's perspective: upstream triggers this stack,
			// so upstream=upstreamID, downstream=stackID.
			await deps.removeDownstream(upstreamID, stackID);
			upstreamDeps = upstreamDeps.filter((d) => d.id !== upstreamID);
		} catch (e) {
			depsError = (e as Error).message;
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
		pending_approval: 'text-purple-400',
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
			<!-- Primary run actions -->
			{#if stack.my_stack_role !== 'viewer'}
				<button onclick={triggerRun} disabled={triggeringRun}
					class="bg-indigo-600 hover:bg-indigo-500 disabled:opacity-50 text-white text-sm px-3 py-1.5 rounded-lg transition-colors">
					{triggeringRun ? 'Queuing…' : 'Trigger run'}
				</button>
				<button
					onclick={() => { showOverrides = !showOverrides; }}
					title="Variable overrides for this run"
					class="border transition-colors text-sm px-2 py-1.5 rounded-lg
						{overrides.length > 0
							? 'border-indigo-600 text-indigo-400 hover:border-indigo-400'
							: 'border-zinc-700 text-zinc-500 hover:border-zinc-500 hover:text-zinc-300'}">
					{overrides.length > 0 ? `Overrides (${overrides.length})` : 'Overrides'}
				</button>
				{#if stack.drift_detection}
					<button onclick={triggerDrift} disabled={triggeringDrift}
						class="border border-zinc-700 hover:border-zinc-500 text-zinc-300 text-sm px-3 py-1.5 rounded-lg transition-colors">
						{triggeringDrift ? 'Queuing…' : 'Drift check'}
					</button>
				{/if}

				<!-- Separator -->
				<div class="w-px h-5 bg-zinc-700 mx-1"></div>
			{/if}

			<!-- Stack management -->
			<button onclick={() => { editing = !editing; resetForm(); }}
				class="border border-zinc-700 hover:border-zinc-500 text-zinc-300 text-sm px-3 py-1.5 rounded-lg transition-colors">
				{editing ? 'Cancel' : 'Edit'}
			</button>
			{#if auth.isMemberOrAbove}
				<button onclick={toggleDisabled} disabled={togglingDisabled}
					class="border border-zinc-700 hover:border-zinc-500 text-zinc-400 text-sm px-3 py-1.5 rounded-lg transition-colors disabled:opacity-50">
					{togglingDisabled ? '…' : stack.is_disabled ? 'Enable' : 'Disable'}
				</button>
			{/if}

			<!-- Separator before destructive actions -->
			{#if stack.my_stack_role !== 'viewer'}
				<div class="w-px h-5 bg-zinc-700 mx-1"></div>
				<button onclick={() => { showDestroyModal = true; destroyConfirmName = ''; }}
					class="border border-orange-900 hover:border-orange-700 text-orange-400 text-sm px-3 py-1.5 rounded-lg transition-colors">
					Destroy
				</button>
			{/if}
			{#if auth.isAdmin}
				<button onclick={forceUnlock} disabled={forcingUnlock}
					class="border border-zinc-700 hover:border-zinc-600 text-zinc-500 text-sm px-3 py-1.5 rounded-lg transition-colors disabled:opacity-50"
					title="Clear a stuck state lock from a failed or cancelled run">
					{forcingUnlock ? '…' : 'Unlock'}
				</button>
				<button onclick={deleteStack}
					class="border border-red-900 hover:border-red-700 text-red-400 text-sm px-3 py-1.5 rounded-lg transition-colors">
					Delete
				</button>
			{/if}
		</div>
	</div>

	<!-- Variable overrides panel -->
	{#if showOverrides}
	<div class="border border-zinc-800 rounded-xl p-4 space-y-3">
		<div class="flex items-center justify-between">
			<div>
				<p class="text-sm font-medium text-white">Variable overrides</p>
				<p class="text-xs text-zinc-500 mt-0.5">KEY=value pairs injected into this run only, taking highest precedence over all other env sources.</p>
			</div>
			{#if overrides.length > 0}
				<button onclick={() => { overrides = []; }} class="text-xs text-zinc-500 hover:text-red-400 transition-colors">Clear all</button>
			{/if}
		</div>

		<!-- Existing overrides -->
		{#if overrides.length > 0}
		<div class="divide-y divide-zinc-800 border border-zinc-800 rounded-lg overflow-hidden">
			{#each overrides as ov}
			<div class="flex items-center gap-2 px-3 py-2 bg-zinc-900">
				<code class="text-xs text-indigo-300 font-mono flex-shrink-0">{ov.key}</code>
				<span class="text-zinc-600 text-xs">=</span>
				<code class="text-xs text-zinc-300 font-mono truncate flex-1">{ov.value || '(empty)'}</code>
				<button onclick={() => removeOverride(ov.key)} class="text-zinc-600 hover:text-red-400 transition-colors text-xs ml-1">✕</button>
			</div>
			{/each}
		</div>
		{/if}

		<!-- Add a new override -->
		<div class="flex gap-2">
			<input
				bind:value={newOverrideKey}
				placeholder="KEY"
				class="field-input font-mono text-xs flex-1 min-w-0"
				onkeydown={(e) => { if (e.key === 'Enter') { e.preventDefault(); addOverride(); } }}
			/>
			<input
				bind:value={newOverrideValue}
				placeholder="value"
				class="field-input font-mono text-xs flex-[2] min-w-0"
				onkeydown={(e) => { if (e.key === 'Enter') { e.preventDefault(); addOverride(); } }}
			/>
			<button
				onclick={addOverride}
				disabled={!newOverrideKey.trim()}
				class="bg-zinc-800 hover:bg-zinc-700 disabled:opacity-40 text-zinc-300 text-xs px-3 py-1.5 rounded-lg transition-colors whitespace-nowrap">
				Add
			</button>
		</div>
	</div>
	{/if}

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
				<label class="field-label" for="edit-repo">Repository URL</label>
				<input id="edit-repo" class="field-input font-mono text-sm" bind:value={form.repo_url} placeholder="https://github.com/org/repo.git" required />
			</div>
			<div class="space-y-1.5">
				<label class="field-label" for="edit-root">Project root</label>
				<input id="edit-root" class="field-input font-mono text-sm" bind:value={form.project_root} />
			</div>
			<div class="flex gap-6 flex-wrap">
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
				<label class="flex items-center gap-2 cursor-pointer text-sm text-zinc-300">
					<input type="checkbox" bind:checked={form.auto_remediate_drift} />
					Auto-remediate drift — automatically apply when drift is detected
				</label>
			{/if}
			<div class="space-y-1.5">
				<label class="field-label" for="edit-destroy-at">Scheduled destroy (UTC)</label>
				<div class="flex items-center gap-2">
					<input type="datetime-local" id="edit-destroy-at"
						class="field-input"
						bind:value={form.scheduled_destroy_at}
					/>
					{#if form.scheduled_destroy_at}
						<button type="button"
							onclick={() => form.scheduled_destroy_at = ''}
							class="text-xs text-zinc-500 hover:text-zinc-300 transition-colors">
							Clear
						</button>
					{/if}
				</div>
				<p class="text-xs text-zinc-600">If set, a destroy run will be triggered automatically at this time.</p>
			</div>
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
			['Auto-remediate drift', stack.drift_detection ? (stack.auto_remediate_drift ? 'Yes' : 'No') : '—'],
			['Scheduled destroy', stack.scheduled_destroy_at ? fmtDate(stack.scheduled_destroy_at) + ' UTC' : '—'],
			['Created', fmtDate(stack.created_at)]
		] as [label, value]}
			<div class="flex px-4 py-3">
				<span class="w-36 flex-shrink-0 text-zinc-500">{label}</span>
				<span class="text-zinc-200 font-mono text-xs break-all">{value}</span>
			</div>
		{/each}
	</div>

	<!-- Resource explorer -->
	<section class="space-y-3">
		<div class="flex items-center justify-between">
			<h2 class="text-sm font-medium text-zinc-400 uppercase tracking-wide">Resources</h2>
			{#if stateResources.length > 0}
				<span class="text-xs text-zinc-600">{stateResources.length} resource{stateResources.length === 1 ? '' : 's'}</span>
			{/if}
		</div>
		{#if stateResources.length === 0}
			<p class="text-zinc-600 text-sm">No state yet — trigger a run to populate resources.</p>
		{:else}
			{#if stateResources.length > 5}
				<input
					class="field-input w-64"
					placeholder="Filter by type or name…"
					bind:value={resourceFilter}
				/>
			{/if}
			{@const filtered = stateResources.filter(r =>
				!resourceFilter ||
				r.type.includes(resourceFilter) ||
				r.address.includes(resourceFilter)
			)}
			<div class="border border-zinc-800 rounded-xl overflow-hidden">
				<table class="w-full text-sm">
					<thead class="bg-zinc-900 text-zinc-500 text-xs uppercase tracking-wide">
						<tr>
							<th class="text-left px-4 py-2">Address</th>
							<th class="text-left px-4 py-2">Type</th>
							<th class="text-left px-4 py-2">Mode</th>
							<th class="text-right px-4 py-2">Instances</th>
						</tr>
					</thead>
					<tbody class="divide-y divide-zinc-800">
						{#each filtered as r (r.address)}
							<tr>
								<td class="px-4 py-2.5 font-mono text-xs text-zinc-200">{r.address}</td>
								<td class="px-4 py-2.5 font-mono text-xs text-zinc-400">{r.type}</td>
								<td class="px-4 py-2.5">
									<span class="text-xs {r.mode === 'managed' ? 'text-green-400' : 'text-zinc-500'}">
										{r.mode}
									</span>
								</td>
								<td class="px-4 py-2.5 text-right text-zinc-400 text-xs">{r.instance_count}</td>
							</tr>
						{/each}
						{#if filtered.length === 0}
							<tr>
								<td colspan="4" class="px-4 py-4 text-center text-zinc-600 text-sm">No resources match filter.</td>
							</tr>
						{/if}
					</tbody>
				</table>
			</div>
		{/if}
	</section>

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

	<!-- Remote state sources -->
	<section class="space-y-3">
		<h2 class="text-sm font-medium text-zinc-400 uppercase tracking-wide">Remote state sources</h2>
		<p class="text-xs text-zinc-500">
			Allow this stack to read the Terraform state of other stacks using
			<code class="text-zinc-300">terraform_remote_state</code>. Each source stack is accessible via
			env vars <code class="text-zinc-300">CRUCIBLE_REMOTE_STATE_&lt;SLUG&gt;_&#123;ADDRESS,USERNAME,PASSWORD&#125;</code>
			injected into runner containers.
		</p>

		{#if remoteSources.length > 0}
			<div class="border border-zinc-800 rounded-xl overflow-hidden">
				<table class="w-full text-sm">
					<thead class="bg-zinc-900 text-zinc-500 text-xs uppercase tracking-wide">
						<tr>
							<th class="text-left px-4 py-2">Source stack</th>
							<th class="text-left px-4 py-2">Env var prefix</th>
							<th class="text-left px-4 py-2">Added</th>
							<th class="px-4 py-2"></th>
						</tr>
					</thead>
					<tbody class="divide-y divide-zinc-800">
						{#each remoteSources as rs (rs.id)}
							<tr>
								<td class="px-4 py-2.5 text-zinc-200">
									<a href="/stacks/{rs.source_stack_id}" class="hover:text-white">{rs.source_stack_name}</a>
								</td>
								<td class="px-4 py-2.5 font-mono text-xs text-zinc-400">{rs.env_var_prefix}</td>
								<td class="px-4 py-2.5 text-zinc-500 text-xs">{fmtDate(rs.created_at)}</td>
								<td class="px-4 py-2.5 text-right">
									<button onclick={() => removeRemoteSource(rs.id)}
										class="text-xs text-zinc-500 hover:text-red-400">Remove</button>
								</td>
							</tr>
						{/each}
					</tbody>
				</table>
			</div>
		{:else}
			<p class="text-zinc-600 text-sm">No remote state sources configured.</p>
		{/if}

		{#if allStacksList.length > 0}
			<div class="flex items-center gap-2 flex-wrap">
				<select class="field-input w-64" bind:value={addingRemoteSource}>
					<option value="">— add a source stack —</option>
					{#each allStacksList.filter(s => !remoteSources.some(r => r.source_stack_id === s.id)) as s (s.id)}
						<option value={s.id}>{s.name}</option>
					{/each}
				</select>
				<button onclick={addRemoteSource} disabled={!addingRemoteSource}
					class="border border-zinc-700 hover:border-zinc-500 disabled:opacity-40 text-zinc-300 text-sm px-3 py-1.5 rounded-lg transition-colors">
					Add
				</button>
				{#if addingRemoteSourceError}
					<span class="text-xs text-red-400">{addingRemoteSourceError}</span>
				{/if}
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
							<th class="text-left px-4 py-2">Type</th>
							<th class="text-left px-4 py-2">Last updated</th>
							<th class="px-4 py-2"></th>
						</tr>
					</thead>
					<tbody class="divide-y divide-zinc-800">
						{#each envVars as ev (ev.id)}
							<tr>
								<td class="px-4 py-2.5 font-mono text-xs text-zinc-200">{ev.name}</td>
								<td class="px-4 py-2.5">
									{#if ev.is_secret}
										<span class="text-xs text-zinc-500" title="Value is masked — cannot be read back">🔒 secret</span>
									{:else}
										<span class="text-xs text-zinc-400">plain</span>
									{/if}
								</td>
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

		<form onsubmit={saveEnvVar} class="space-y-2">
			<div class="flex items-center gap-2 flex-wrap">
				<input id="env-name" class="field-input w-40 font-mono text-xs" bind:value={newEnvName} placeholder="NAME" autocomplete="off" />
				<input id="env-value" class="field-input w-56" type={newEnvSecret ? 'password' : 'text'} bind:value={newEnvValue} placeholder="value" autocomplete="new-password" />
				<label class="flex items-center gap-1.5 cursor-pointer text-xs text-zinc-400 select-none" for="env-secret">
					<input id="env-secret" type="checkbox" bind:checked={newEnvSecret} />
					Secret
				</label>
				<button type="submit" disabled={savingEnv || !newEnvName || !newEnvValue}
					class="border border-zinc-700 hover:border-zinc-500 text-zinc-300 text-sm px-3 py-1.5 rounded-lg transition-colors disabled:opacity-50">
					{savingEnv ? 'Saving…' : 'Set'}
				</button>
			</div>
			<p class="text-xs text-zinc-600">
				{#if newEnvSecret}
					Secret — value is masked, write-only, and never shown again after saving.
				{:else}
					Plain — value is still encrypted at rest, but the type is recorded for documentation purposes.
				{/if}
			</p>
		</form>
	</section>

	<!-- Variable sets -->
	<section class="space-y-3">
		<div class="flex items-center justify-between">
			<h2 class="text-sm font-medium text-zinc-400 uppercase tracking-wide">Variable sets</h2>
			<a href="/variable-sets" class="text-xs text-zinc-500 hover:text-zinc-300 transition-colors">Manage →</a>
		</div>
		<p class="text-xs text-zinc-500">Attach variable sets to inject shared environment variables into every run on this stack.</p>

		{#if stackVarSets.length > 0}
			<div class="border border-zinc-800 rounded-xl overflow-hidden">
				<table class="w-full text-sm">
					<thead class="bg-zinc-900 text-zinc-500 text-xs uppercase tracking-wide">
						<tr>
							<th class="text-left px-4 py-2">Name</th>
							<th class="text-left px-4 py-2">Variables</th>
							<th class="px-4 py-2"></th>
						</tr>
					</thead>
					<tbody class="divide-y divide-zinc-800">
						{#each stackVarSets as svs (svs.id)}
							<tr>
								<td class="px-4 py-2.5">
									<a href="/variable-sets/{svs.id}" class="text-zinc-200 hover:text-white transition-colors">{svs.name}</a>
									{#if svs.description}
										<p class="text-xs text-zinc-600 mt-0.5">{svs.description}</p>
									{/if}
								</td>
								<td class="px-4 py-2.5 text-zinc-500">{svs.var_count}</td>
								<td class="px-4 py-2.5 text-right">
									{#if auth.isMemberOrAbove}
										<button onclick={() => detachVarSet(svs.id)} class="text-xs text-zinc-500 hover:text-red-400">Detach</button>
									{/if}
								</td>
							</tr>
						{/each}
					</tbody>
				</table>
			</div>
		{:else}
			<p class="text-zinc-600 text-sm">No variable sets attached.</p>
		{/if}

		{#if auth.isMemberOrAbove}
			{#if allVarSets.length > stackVarSets.length}
				<div class="flex items-center gap-2">
					<select class="field-input w-56" bind:value={attachingVarSet}>
						<option value="">Select a variable set…</option>
						{#each allVarSets.filter(vs => !stackVarSets.find(s => s.id === vs.id)) as vs (vs.id)}
							<option value={vs.id}>{vs.name}</option>
						{/each}
					</select>
					<button onclick={attachVarSet} disabled={!attachingVarSet}
						class="border border-zinc-700 hover:border-zinc-500 text-zinc-300 text-sm px-3 py-1.5 rounded-lg transition-colors disabled:opacity-50">
						Attach
					</button>
				</div>
			{/if}
		{/if}
	</section>

	<!-- Dependencies -->
	<section class="space-y-4">
		<h2 class="text-sm font-medium text-zinc-400 uppercase tracking-wide">Dependencies</h2>
		<p class="text-xs text-zinc-500">Upstream stacks trigger this stack after they apply. Downstream stacks are triggered when this stack applies.</p>

		{#if depsError}
			<p class="text-sm text-red-400">{depsError}</p>
		{/if}

		<div class="space-y-2">
			<p class="text-xs text-zinc-500 uppercase tracking-wide">Upstream (runs before this stack)</p>
			{#if upstreamDeps.length > 0}
				<div class="border border-zinc-800 rounded-xl overflow-hidden">
					<table class="w-full text-sm">
						<tbody class="divide-y divide-zinc-800">
							{#each upstreamDeps as dep (dep.id)}
								<tr>
									<td class="px-4 py-2.5">
										<a href="/stacks/{dep.id}" class="text-zinc-200 hover:text-white transition-colors">{dep.name}</a>
										<span class="text-zinc-600 text-xs ml-2">{dep.slug}</span>
									</td>
									<td class="px-4 py-2.5 text-right">
										{#if auth.isMemberOrAbove}
											<button onclick={() => removeUpstreamDep(dep.id)} class="text-xs text-zinc-500 hover:text-red-400">Remove</button>
										{/if}
									</td>
								</tr>
							{/each}
						</tbody>
					</table>
				</div>
			{:else}
				<p class="text-zinc-600 text-sm">No upstream stacks configured.</p>
			{/if}
		</div>

		<div class="space-y-2">
			<p class="text-xs text-zinc-500 uppercase tracking-wide">Downstream (triggered after this stack applies)</p>
			{#if downstreamDeps.length > 0}
				<div class="border border-zinc-800 rounded-xl overflow-hidden">
					<table class="w-full text-sm">
						<tbody class="divide-y divide-zinc-800">
							{#each downstreamDeps as dep (dep.id)}
								<tr>
									<td class="px-4 py-2.5">
										<a href="/stacks/{dep.id}" class="text-zinc-200 hover:text-white transition-colors">{dep.name}</a>
										<span class="text-zinc-600 text-xs ml-2">{dep.slug}</span>
									</td>
									<td class="px-4 py-2.5 text-right">
										{#if auth.isMemberOrAbove}
											<button onclick={() => removeDownstreamDep(dep.id)} class="text-xs text-zinc-500 hover:text-red-400">Remove</button>
										{/if}
									</td>
								</tr>
							{/each}
						</tbody>
					</table>
				</div>
			{:else}
				<p class="text-zinc-600 text-sm">No downstream stacks configured.</p>
			{/if}

			{#if auth.isMemberOrAbove}
				{@const eligible = allStacksList.filter(s => s.id !== stackID && !downstreamDeps.find(d => d.id === s.id) && !upstreamDeps.find(u => u.id === s.id))}
				{#if eligible.length > 0}
					<div class="flex items-center gap-2">
						<select class="field-input w-56" bind:value={addingDownstream}>
							<option value="">Add downstream stack…</option>
							{#each eligible as s (s.id)}
								<option value={s.id}>{s.name}</option>
							{/each}
						</select>
						<button onclick={addDownstreamDep} disabled={!addingDownstream}
							class="border border-zinc-700 hover:border-zinc-500 text-zinc-300 text-sm px-3 py-1.5 rounded-lg transition-colors disabled:opacity-50">
							Add
						</button>
					</div>
				{/if}
			{/if}
		</div>
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
				<div class="space-y-1.5">
					<label class="field-label" for="notif-gotify-url">Gotify server URL</label>
					<input id="notif-gotify-url" class="field-input"
						bind:value={notifGotifyURL}
						placeholder="https://gotify.example.com" />
					<p class="text-xs text-zinc-600">Leave blank to disable Gotify notifications.</p>
				</div>
				<div class="space-y-1.5">
					<label class="field-label" for="notif-gotify-token">
						Gotify app token
						{#if stack.has_gotify_token}
							<span class="ml-1 text-green-500 text-xs">● set</span>
						{/if}
					</label>
					<input id="notif-gotify-token" class="field-input" type="password"
						bind:value={notifGotifyToken}
						placeholder={stack.has_gotify_token ? 'Enter new value to replace' : 'App token from Gotify'}
						autocomplete="new-password" />
				</div>
				<div class="space-y-1.5">
					<label class="field-label" for="notif-ntfy-url">ntfy topic URL</label>
					<input id="notif-ntfy-url" class="field-input"
						bind:value={notifNtfyURL}
						placeholder="https://ntfy.sh/my-topic" />
					<p class="text-xs text-zinc-600">Include the topic in the URL. Leave blank to disable ntfy notifications.</p>
				</div>
				<div class="space-y-1.5">
					<label class="field-label" for="notif-ntfy-token">
						ntfy access token <span class="text-zinc-600">(optional, for private topics)</span>
						{#if stack.has_ntfy_token}
							<span class="ml-1 text-green-500 text-xs">● set</span>
						{/if}
					</label>
					<input id="notif-ntfy-token" class="field-input" type="password"
						bind:value={notifNtfyToken}
						placeholder={stack.has_ntfy_token ? 'Enter new value to replace' : 'tk_…'}
						autocomplete="new-password" />
				</div>
			</div>

			<div class="space-y-1.5">
				<label class="field-label" for="notif-email">Email address(es)</label>
				<input id="notif-email" type="email" class="field-input"
					bind:value={notifEmail}
					placeholder="alerts@example.com"
					multiple />
				<p class="text-xs text-zinc-600">Separate multiple addresses with commas. Requires SMTP configured in Settings → Notifications.</p>
			</div>

			<div class="space-y-1.5">
				<p class="text-xs text-zinc-400">Events to notify on (Slack + Gotify + ntfy + email)</p>
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

			<div class="flex items-center gap-3 flex-wrap">
				<button type="submit" disabled={savingNotif}
					class="bg-indigo-600 hover:bg-indigo-500 disabled:opacity-50 text-white text-sm px-4 py-1.5 rounded-lg transition-colors">
					{savingNotif ? 'Saving…' : 'Save notifications'}
				</button>
				{#if stack.has_slack_webhook}
					<button type="button" onclick={testSlack} disabled={testingSlack}
						class="border border-zinc-700 hover:border-zinc-500 text-zinc-300 text-sm px-3 py-1.5 rounded-lg transition-colors disabled:opacity-50">
						{testingSlack ? 'Sending…' : 'Test Slack'}
					</button>
				{/if}
				{#if notifGotifyURL && stack.has_gotify_token}
					<button type="button" onclick={testGotify} disabled={testingGotify}
						class="border border-zinc-700 hover:border-zinc-500 text-zinc-300 text-sm px-3 py-1.5 rounded-lg transition-colors disabled:opacity-50">
						{testingGotify ? 'Sending…' : 'Test Gotify'}
					</button>
				{/if}
				{#if notifNtfyURL}
					<button type="button" onclick={testNtfy} disabled={testingNtfy}
						class="border border-zinc-700 hover:border-zinc-500 text-zinc-300 text-sm px-3 py-1.5 rounded-lg transition-colors disabled:opacity-50">
						{testingNtfy ? 'Sending…' : 'Test ntfy'}
					</button>
				{/if}
				{#if notifEmail}
					<button type="button" onclick={testEmail} disabled={testingEmail}
						class="border border-zinc-700 hover:border-zinc-500 text-zinc-300 text-sm px-3 py-1.5 rounded-lg transition-colors disabled:opacity-50">
						{testingEmail ? 'Sending…' : 'Test email'}
					</button>
				{/if}
				{#if notifSaved}
					<span class="text-xs text-green-400">Saved.</span>
				{/if}
				{#if slackTestResult}
					<span class="text-xs {slackTestResult.ok ? 'text-green-400' : 'text-red-400'}">
						{slackTestResult.msg}
					</span>
				{/if}
				{#if gotifyTestResult}
					<span class="text-xs {gotifyTestResult.ok ? 'text-green-400' : 'text-red-400'}">
						{gotifyTestResult.msg}
					</span>
				{/if}
				{#if ntfyTestResult}
					<span class="text-xs {ntfyTestResult.ok ? 'text-green-400' : 'text-red-400'}">
						{ntfyTestResult.msg}
					</span>
				{/if}
				{#if emailTestResult}
					<span class="text-xs {emailTestResult.ok ? 'text-green-400' : 'text-red-400'}">
						{emailTestResult.msg}
					</span>
				{/if}
			</div>
		</form>
	</section>

	<!-- Integrations -->
	<section class="space-y-3">
		<h2 class="text-sm font-medium text-zinc-400 uppercase tracking-wide">Integrations</h2>
		<p class="text-xs text-zinc-500">Assign org-level integrations to this stack. Manage credentials in <a href="/settings/integrations" class="text-indigo-400 hover:text-indigo-300">Settings → Integrations</a>.</p>

		<div class="border border-zinc-800 rounded-xl p-5 space-y-4">
			<div class="grid grid-cols-2 gap-4">
				<div class="space-y-1.5">
					<label class="field-label" for="vcs-integration">VCS / Git credential</label>
					<select id="vcs-integration" class="field-input" bind:value={selectedVCSIntegration}>
						<option value="">— none —</option>
						{#each orgIntegrations.filter(i => ['github', 'gitlab', 'gitea'].includes(i.type)) as intg}
							<option value={intg.id}>{intg.name} <span class="text-zinc-500">({intg.type})</span></option>
						{/each}
					</select>
					<p class="text-xs text-zinc-600">Used to authenticate git clone for private repositories.</p>
				</div>
				<div class="space-y-1.5">
					<label class="field-label" for="secret-integration">Secret store</label>
					<select id="secret-integration" class="field-input" bind:value={selectedSecretIntegration}>
						<option value="">— none —</option>
						{#each orgIntegrations.filter(i => ['aws_sm', 'hc_vault', 'bitwarden_sm', 'vaultwarden'].includes(i.type)) as intg}
							<option value={intg.id}>{intg.name} <span class="text-zinc-500">({intg.type})</span></option>
						{/each}
					</select>
					<p class="text-xs text-zinc-600">Secrets are fetched and injected into runs as env vars.</p>
				</div>
			</div>
			<div class="flex items-center gap-3">
				<button type="button" onclick={setIntegrations} disabled={savingIntegrations}
					class="bg-indigo-600 hover:bg-indigo-500 disabled:opacity-50 text-white text-sm px-4 py-1.5 rounded-lg transition-colors">
					{savingIntegrations ? 'Saving…' : 'Save integrations'}
				</button>
				{#if integrationsSaved}
					<span class="text-xs text-green-400">Saved.</span>
				{/if}
			</div>
		</div>
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

	<!-- Access -->
	{#if auth.isAdmin}
	<section class="space-y-3">
		<div class="flex items-center justify-between">
			<div>
				<h2 class="text-sm font-medium text-zinc-400 uppercase tracking-wide">Access</h2>
				<p class="text-xs text-zinc-500 mt-0.5">
					{#if stack.is_restricted}
						Restricted — only listed members can view and interact with this stack.
					{:else}
						Open — all org members can trigger runs; add a member to restrict access.
					{/if}
				</p>
			</div>
		</div>

		{#if members.length > 0}
			<div class="border border-zinc-800 rounded-xl overflow-hidden">
				<table class="w-full text-sm">
					<thead class="bg-zinc-900 text-zinc-500 text-xs uppercase tracking-wide">
						<tr>
							<th class="text-left px-4 py-2">User</th>
							<th class="text-left px-4 py-2">Role</th>
							<th class="px-4 py-2"></th>
						</tr>
					</thead>
					<tbody class="divide-y divide-zinc-800">
						{#each members as m (m.user_id)}
							<tr>
								<td class="px-4 py-2.5">
									<span class="text-zinc-200">{m.name || m.email}</span>
									{#if m.name}<span class="text-zinc-500 text-xs ml-1">{m.email}</span>{/if}
								</td>
								<td class="px-4 py-2.5">
									<select
										value={m.role}
										onchange={(e) => updateMemberRole(m.user_id, (e.target as HTMLSelectElement).value as 'viewer' | 'approver')}
										class="field-input w-auto text-xs py-1">
										<option value="viewer">viewer</option>
										<option value="approver">approver</option>
									</select>
								</td>
								<td class="px-4 py-2.5 text-right">
									<button onclick={() => removeMember(m.user_id)} class="text-xs text-zinc-500 hover:text-red-400">Remove</button>
								</td>
							</tr>
						{/each}
					</tbody>
				</table>
			</div>
		{:else}
			<p class="text-zinc-600 text-sm">No explicit members — stack is open to all org members.</p>
		{/if}

		{#if orgUsers.some(u => !members.some(m => m.user_id === u.user_id))}
			{@const eligible = orgUsers.filter(u => !members.some(m => m.user_id === u.user_id))}
			<div class="flex items-center gap-2">
				<select class="field-input w-64" bind:value={addMemberUserID}>
					<option value="">— add a member —</option>
					{#each eligible as u (u.user_id)}
						<option value={u.user_id}>{u.name || u.email}</option>
					{/each}
				</select>
				<select class="field-input w-32" bind:value={addMemberRole}>
					<option value="viewer">viewer</option>
					<option value="approver">approver</option>
				</select>
				<button onclick={upsertMember} disabled={!addMemberUserID || addingMember}
					class="border border-zinc-700 hover:border-zinc-500 disabled:opacity-40 text-zinc-300 text-sm px-3 py-1.5 rounded-lg transition-colors">
					{addingMember ? 'Adding…' : 'Add'}
				</button>
			</div>
		{/if}
	</section>
	{/if}

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

	<!-- Webhook deliveries -->
	<section class="space-y-3">
		<h2 class="text-sm font-medium text-zinc-400 uppercase tracking-wide">Webhook deliveries</h2>
		{#if loadingDeliveries}
			<p class="text-zinc-600 text-sm">Loading…</p>
		{:else if webhookDeliveries.length === 0}
			<div class="border border-zinc-800 rounded-xl p-6 text-center">
				<p class="text-zinc-500 text-sm">No webhook deliveries yet.</p>
				<p class="text-zinc-600 text-xs mt-1">Every inbound webhook request will be logged here.</p>
			</div>
		{:else}
			<div class="border border-zinc-800 rounded-xl overflow-hidden">
				<table class="w-full text-sm">
					<thead class="bg-zinc-900 text-zinc-500 text-xs uppercase tracking-wide">
						<tr>
							<th class="text-left px-4 py-2">Time</th>
							<th class="text-left px-4 py-2">Forge</th>
							<th class="text-left px-4 py-2">Event</th>
							<th class="text-left px-4 py-2">Outcome</th>
							<th class="text-left px-4 py-2">Detail</th>
							<th class="px-4 py-2"></th>
						</tr>
					</thead>
					<tbody class="divide-y divide-zinc-800">
						{#each webhookDeliveries as d (d.id)}
							<tr class="hover:bg-zinc-900/50 transition-colors">
								<td class="px-4 py-2.5 text-zinc-500 text-xs whitespace-nowrap">{fmtDate(d.received_at)}</td>
								<td class="px-4 py-2.5 text-zinc-400 text-xs capitalize">{d.forge}</td>
								<td class="px-4 py-2.5 text-zinc-400 text-xs">{d.event_type}</td>
								<td class="px-4 py-2.5">
									{#if d.outcome === 'triggered'}
										<span class="text-xs font-medium text-green-400">triggered</span>
									{:else if d.outcome === 'skipped'}
										<span class="text-xs font-medium text-zinc-500">skipped</span>
									{:else}
										<span class="text-xs font-medium text-red-400">rejected</span>
									{/if}
								</td>
								<td class="px-4 py-2.5 text-xs">
									{#if d.run_id}
										<a href="/runs/{d.run_id}" class="text-indigo-400 hover:text-indigo-300">run →</a>
									{:else if d.skip_reason}
										<span class="text-zinc-600">{d.skip_reason.replace(/_/g, ' ')}</span>
									{:else}
										<span class="text-zinc-700">—</span>
									{/if}
								</td>
								<td class="px-4 py-2.5 text-xs text-right">
									<button
										title="Re-deliver"
										onclick={async () => {
											try {
												const res = await stacks.webhook.redeliver(stackID, d.id);
												goto('/runs/' + res.run_id);
											} catch { /* ignore */ }
										}}
										class="text-zinc-500 hover:text-indigo-400 transition-colors"
									>↺</button>
								</td>
							</tr>
						{/each}
					</tbody>
				</table>
			</div>
		{/if}
	</section>

	<!-- Recent runs -->
	<section class="space-y-3">
		<div class="flex items-center justify-between">
			<h2 class="text-sm font-medium text-zinc-400 uppercase tracking-wide">Recent runs</h2>
			{#if recentRuns.length > 0}
				<a href="/runs?stack={stackID}" class="text-xs text-zinc-500 hover:text-zinc-300 transition-colors">
					View all →
				</a>
			{/if}
		</div>
		{#if recentRuns.length === 0}
			<div class="border border-zinc-800 rounded-xl p-8 text-center space-y-3">
				<p class="text-zinc-500 text-sm">No runs yet.</p>
				<p class="text-zinc-600 text-xs">Trigger a run to deploy or plan this stack's infrastructure.</p>
			</div>
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
							{@const tb = triggerBadge(run.trigger)}
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
								<td class="px-4 py-2.5">
									<span class="text-xs px-1.5 py-0.5 rounded font-medium {tb.classes}">{tb.label}</span>
								</td>
								<td class="px-4 py-2.5 text-zinc-500 text-xs">{fmtDate(run.queued_at)}</td>
							</tr>
						{/each}
					</tbody>
				</table>
			</div>
		{/if}
	</section>

	<!-- Module publishing -->
	<section class="space-y-3">
		<div class="flex items-center justify-between">
			<h2 class="text-sm font-medium text-zinc-400 uppercase tracking-wide">Module publishing</h2>
			{#if stack.module_namespace}
				<a href="/registry" class="text-xs text-zinc-500 hover:text-zinc-300 transition-colors">View in registry →</a>
			{/if}
		</div>
		<p class="text-xs text-zinc-600">When configured, tag pushes matching a semver pattern (e.g. <span class="font-mono text-zinc-500">v1.2.3</span>) will automatically publish a new module version to the private Terraform registry.</p>
		{#if auth.isAdmin}
			<form onsubmit={saveModuleConfig} class="border border-zinc-800 rounded-xl p-5 space-y-4">
				<div class="grid grid-cols-3 gap-4">
					<div class="space-y-1.5">
						<label class="field-label" for="mod-namespace">Namespace</label>
						<input id="mod-namespace" class="field-input" bind:value={moduleNamespace}
							placeholder="myorg" />
					</div>
					<div class="space-y-1.5">
						<label class="field-label" for="mod-name">Module name</label>
						<input id="mod-name" class="field-input" bind:value={moduleName}
							placeholder="vpc" />
					</div>
					<div class="space-y-1.5">
						<label class="field-label" for="mod-provider">Provider</label>
						<input id="mod-provider" class="field-input" bind:value={moduleProvider}
							placeholder="aws" />
					</div>
				</div>
				<div class="flex items-center gap-3">
					<button type="submit" disabled={savingModule}
						class="border border-zinc-700 hover:border-zinc-500 text-zinc-300 text-sm px-3 py-1.5 rounded-lg transition-colors disabled:opacity-50">
						{savingModule ? 'Saving…' : moduleSaved ? 'Saved' : 'Save'}
					</button>
					{#if moduleNamespace || moduleName}
						<button type="button" onclick={async () => { moduleNamespace = ''; moduleName = ''; moduleProvider = ''; stack = await stacks.update(stackID, { module_namespace: '', module_name: '', module_provider: '' }); }}
							class="text-xs text-zinc-500 hover:text-red-400 transition-colors">
							Clear
						</button>
					{/if}
				</div>
			</form>
		{:else if stack.module_namespace}
			<div class="border border-zinc-800 rounded-xl p-4 text-sm text-zinc-400">
				Publishing as <span class="font-mono text-white">{stack.module_namespace}/{stack.module_name}/{stack.module_provider}</span>
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
