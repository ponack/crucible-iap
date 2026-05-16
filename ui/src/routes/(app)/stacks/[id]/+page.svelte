<script lang="ts">
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { onMount } from 'svelte';
	import { stacks, runs, policies, integrations, varSets, deps, stackMembers, org, orgTags, cloudOIDC, stackTemplates, workerPools, githubApp, projects, complianceApi, analyticsApi, type Stack, type Run, type StackToken, type Policy, type StackPolicyRef, type StackEnvVar, type Integration, type StateBackendProvider, type S3StateBackendConfig, type GCSStateBackendConfig, type AzureStateBackendConfig, type RemoteStateSource, type WebhookDelivery, type VarSet, type StackVarSetRef, type StateResource, type StateVersion, type StateDiff, type PlanDiff, type StackDep, type StackMember, type OrgMember, type CloudOIDCConfig, type OutgoingWebhook, type OutgoingWebhookDelivery, type StackTemplate, type WorkerPool, type Tag, type GitHubAppView, type Project, type PolicyPack, type CatalogEntry, type CostPoint } from '$lib/api/client';
	import { triggerBadge } from '$lib/trigger';
	import { auth } from '$lib/stores/auth.svelte';
	import DepGraph from '$lib/components/DepGraph.svelte';
	import { toast } from '$lib/stores/toasts.svelte';

	const stackID = $derived(page.params.id as string);

	let stack = $state<Stack | null>(null);
	let recentRuns = $state<Run[]>([]);
	let tokens = $state<StackToken[]>([]);
	let stackPolicies = $state<StackPolicyRef[]>([]);
	let allPolicies = $state<Policy[]>([]);
	let stackPolicyPacks = $state<PolicyPack[]>([]);
	let catalogEntries = $state<CatalogEntry[]>([]);
	let loading = $state(true);
	let error = $state<string | null>(null);

	// Tags
	let allOrgTags = $state<Tag[]>([]);
	let tagPickerOpen = $state(false);
	let savingTags = $state(false);

	async function loadOrgTags() {
		try { allOrgTags = await orgTags.list(); } catch { /* non-fatal */ }
	}

	async function toggleStackTag(tagID: string) {
		if (!stack) return;
		savingTags = true;
		try {
			const current = stack.tags.map((t) => t.id);
			const next = current.includes(tagID)
				? current.filter((id) => id !== tagID)
				: [...current, tagID];
			await stacks.setTags(stack.id, next);
			const updated = await stacks.get(stack.id);
			stack = updated;
		} catch (e) {
			toast.error((e as Error).message);
		} finally {
			savingTags = false;
		}
	}

	// Edit form state
	let editing = $state(false);
	let saving = $state(false);
	let editError = $state<string | null>(null);
	let form = $state({
		name: '', description: '', repo_url: '', repo_branch: '', project_root: '',
		auto_apply: false, drift_detection: false, drift_schedule: '', auto_remediate_drift: false,
		scheduled_destroy_at: '',
		plan_schedule: '', apply_schedule: '', destroy_schedule: '',
		pre_plan_hook: '', post_plan_hook: '', pre_apply_hook: '', post_apply_hook: '',
		max_concurrent_runs: 0,
		pr_preview_enabled: false,
		pr_preview_template_id: '',
		worker_pool_id: '',
		project_id: '',
		plan_alert_add: undefined as number | undefined,
		plan_alert_change: undefined as number | undefined,
		plan_alert_destroy: undefined as number | undefined,
		plan_block_on_alert: false,
		budget_threshold_usd: undefined as number | undefined,
		validation_interval: 0
	});

	// Token creation
	let newTokenName = $state('');
	let creatingToken = $state(false);
	let newTokenSecret = $state<string | null>(null);

	// Run creation
	let triggeringRun = $state(false);
	let triggeringDrift = $state(false);

	// Continuous validation
	let validationResults = $state<import('$lib/api/stacks').ValidationResult[]>([]);
	let triggeringValidation = $state(false);
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
	let envValueInput = $state<HTMLInputElement | null>(null);

	// Notifications
	let notifVCSToken = $state('');
	let notifVCSUsername = $state('');
	let notifSlackWebhook = $state('');
	let notifDiscordWebhook = $state('');
	let notifTeamsWebhook = $state('');
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
	let testingDiscord = $state(false);
	let discordTestResult = $state<{ ok: boolean; msg: string } | null>(null);
	let testingTeams = $state(false);
	let teamsTestResult = $state<{ ok: boolean; msg: string } | null>(null);
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

	// GitHub App authentication for this stack
	let ghApp = $state<GitHubAppView | null>(null);
	let selectedGHInstallation = $state<string>('');
	let savingGHAuth = $state(false);
	let ghAuthSaved = $state(false);

	// Notifications VCS provider
	let notifVCSProvider = $state('');
	let notifVCSBaseURL = $state('');

	// Webhook
	let rotatingWebhook = $state(false);
	let newWebhookSecret = $state<string | null>(null);
	let webhookDeliveries = $state<WebhookDelivery[]>([]);
	let loadingDeliveries = $state(false);
	let expandedDeliveryID = $state<string | null>(null);
	let deliveryPayloads = $state<Record<string, unknown>>({});

	// Outgoing webhooks
	let outgoingWebhooks = $state<OutgoingWebhook[]>([]);
	let newOWURL = $state('');
	let newOWEvents = $state<string[]>(['plan_complete', 'run_finished', 'run_failed']);
	let newOWWithSecret = $state(true);
	let newOWHeaders = $state('');
	let addingOW = $state(false);
	let owError = $state<string | null>(null);
	let owRevealedSecret = $state<string | null>(null);
	let owDeliveries = $state<Record<string, OutgoingWebhookDelivery[]>>({});
	let expandedOWID = $state<string | null>(null);

	// Variable sets
	let stackVarSets = $state<StackVarSetRef[]>([]);
	let allVarSets = $state<VarSet[]>([]);
	let attachingVarSet = $state('');

	// PR preview
	let allTemplates = $state<StackTemplate[]>([]);
	let allWorkerPools = $state<WorkerPool[]>([]);
	let allProjects = $state<Project[]>([]);

	// Dependencies
	let upstreamDeps = $state<StackDep[]>([]);
	let downstreamDeps = $state<StackDep[]>([]);
	let addingDownstream = $state('');
	let depsError = $state<string | null>(null);

	// Disable/enable
	let togglingDisabled = $state(false);

	// Lock/unlock (maintenance mode)
	let togglingLocked = $state(false);

	// Clone
	let showCloneModal = $state(false);
	let cloneName = $state('');
	let cloning = $state(false);
	let cloneError = $state<string | null>(null);

	// Remote state sources
	let remoteSources = $state<RemoteStateSource[]>([]);
	let addingRemoteSource = $state('');
	let addingRemoteSourceError = $state<string | null>(null);
	let allStacksList = $state<{ id: string; name: string }[]>([]);

	// Resource explorer
	let stateResources = $state<StateResource[]>([]);
	let resourceFilter = $state('');

	// State version history
	let stateVersions = $state<StateVersion[]>([]);
	let expandedDiff = $state<string | null>(null);
	let loadedDiffs = $state<Record<string, StateDiff>>({});

	// Plan diff
	let planDiffFrom = $state('');
	let planDiffTo = $state('');
	let planDiffResult = $state<PlanDiff | null>(null);
	let planDiffLoading = $state(false);
	let planDiffError = $state('');
	let planRuns = $derived(recentRuns.filter(r => r.plan_add != null || r.plan_change != null));

	// Cost sparkline
	let costHistory = $state<CostPoint[]>([]);
	const maxCostAdd = $derived(Math.max(...costHistory.map(p => p.cost_add), 0.01));

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

	// Cloud OIDC workload identity federation
	let oidcConfig = $state<CloudOIDCConfig | null>(null);
	let oidcProvider = $state<CloudOIDCConfig['provider']>('aws');
	let oidcAWSRoleARN = $state('');
	let oidcGCPAudience = $state('');
	let oidcGCPSA = $state('');
	let oidcAzureTenant = $state('');
	let oidcAzureClient = $state('');
	let oidcAzureSubscription = $state('');
	let oidcVaultAddr = $state('');
	let oidcVaultRole = $state('');
	let oidcVaultMount = $state('');
	let oidcAuthentikURL = $state('');
	let oidcAuthentikClientID = $state('');
	let oidcGenericTokenURL = $state('');
	let oidcGenericClientID = $state('');
	let oidcGenericScope = $state('');
	let oidcAudienceOverride = $state('');
	let savingOIDC = $state(false);
	let oidcSaved = $state(false);
	let oidcError = $state<string | null>(null);

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
			complianceApi.listStackPacks(stackID).then(r => (stackPolicyPacks = r)).catch(() => {});
			complianceApi.getCatalog().then(r => (catalogEntries = r)).catch(() => {});
			envVars = envVarsRes;
			remoteSources = remoteSourcesRes;
			allStacksList = allStacksRes.data.filter(s => s.id !== stackID).map(s => ({ id: s.id, name: s.name }));
			orgIntegrations = integrationsRes;
			stackVarSets = stackVarSetsRes;
			allVarSets = allVarSetsRes;
		stackTemplates.list().then(t => (allTemplates = t)).catch((e) => console.error('stackTemplates.list', e));
			workerPools.list().then(r => (allWorkerPools = r.data)).catch((e) => console.error('workerPools.list', e));
		projects.list().then(r => (allProjects = r)).catch(() => {});
			upstreamDeps = upstreamRes;
			downstreamDeps = downstreamRes;
			members = membersRes;
			orgUsers = orgUsersRes;
			selectedVCSIntegration = stackRes.vcs_integration_id ?? '';
			selectedSecretIntegration = stackRes.secret_integration_id ?? '';
			selectedGHInstallation = stackRes.github_installation_uuid ?? '';
			githubApp.get().then(a => (ghApp = a)).catch((e) => console.error('githubApp.get', e));
			if (stackRes.state_backend_provider) {
				stateBackendProvider = stackRes.state_backend_provider as StateBackendProvider;
			}
			notifVCSProvider = stackRes.vcs_provider ?? 'github';
			notifVCSBaseURL = stackRes.vcs_base_url ?? '';
			notifVCSUsername = stackRes.vcs_username ?? '';
			moduleNamespace = stackRes.module_namespace ?? '';
			moduleName = stackRes.module_name ?? '';
			moduleProvider = stackRes.module_provider ?? 'aws';
			resetForm();

			// Load cost sparkline — best-effort, empty list if no Infracost data.
			analyticsApi.getStackCostHistory(stackID).then(h => (costHistory = h)).catch(() => {});

			// Load OIDC config independently — 404 is expected when not configured.
			try {
				oidcConfig = await cloudOIDC.get(stackID);
				oidcProvider = oidcConfig.provider;
				oidcAWSRoleARN = oidcConfig.aws_role_arn ?? '';
				oidcGCPAudience = oidcConfig.gcp_workload_identity_audience ?? '';
				oidcGCPSA = oidcConfig.gcp_service_account_email ?? '';
				oidcAzureTenant = oidcConfig.azure_tenant_id ?? '';
				oidcAzureClient = oidcConfig.azure_client_id ?? '';
				oidcAzureSubscription = oidcConfig.azure_subscription_id ?? '';
				oidcVaultAddr = oidcConfig.vault_addr ?? '';
				oidcVaultRole = oidcConfig.vault_role ?? '';
				oidcVaultMount = oidcConfig.vault_mount ?? '';
				oidcAuthentikURL = oidcConfig.authentik_url ?? '';
				oidcAuthentikClientID = oidcConfig.authentik_client_id ?? '';
				oidcGenericTokenURL = oidcConfig.generic_token_url ?? '';
				oidcGenericClientID = oidcConfig.generic_client_id ?? '';
				oidcGenericScope = oidcConfig.generic_scope ?? '';
				oidcAudienceOverride = oidcConfig.audience_override ?? '';
			} catch {
				oidcConfig = null;
			}
		} catch (e) {
			error = (e as Error).message;
		} finally {
			loading = false;
		}

		loadOrgTags();

		// Restore webhook secret for this session (cleared on tab close).
		newWebhookSecret = sessionStorage.getItem(`webhook_secret_${stackID}`);

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

		// Load state resources and version history independently — no state yet is normal.
		stacks.state.resources(stackID).then(r => (stateResources = r)).catch((e) => console.error('state.resources', e));
		stacks.state.versions(stackID).then(r => (stateVersions = r)).catch((e) => console.error('state.versions', e));

		// Load outgoing webhooks independently.
		stacks.outgoingWebhooks.list(stackID).then(r => (outgoingWebhooks = r)).catch((e) => console.error('outgoingWebhooks.list', e));

		// Load validation results if interval is configured.
		if (stack && stack.validation_interval > 0) {
			stacks.validation.listResults(stackID).then(r => (validationResults = r)).catch(() => {});
		}
	});

	// Separate sync onMount so we can return a cleanup — async onMount can't return a cleanup.
	// Poll recent runs every 10s so the list stays fresh when new runs are created while
	// the user is on this page.
	onMount(() => {
		const poller = setInterval(() => {
			runs.list(stackID).then(r => (recentRuns = r.data)).catch((e) => console.error('runs.list poll', e));
		}, 10_000);
		return () => clearInterval(poller);
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
				: '',
			plan_schedule: stack.plan_schedule ?? '',
			apply_schedule: stack.apply_schedule ?? '',
			destroy_schedule: stack.destroy_schedule ?? '',
			pre_plan_hook: stack.pre_plan_hook ?? '',
			post_plan_hook: stack.post_plan_hook ?? '',
			pre_apply_hook: stack.pre_apply_hook ?? '',
			post_apply_hook: stack.post_apply_hook ?? '',
			max_concurrent_runs: stack.max_concurrent_runs ?? 0,
			pr_preview_enabled: stack.pr_preview_enabled ?? false,
			pr_preview_template_id: stack.pr_preview_template_id ?? '',
			worker_pool_id: stack.worker_pool_id ?? '',
			project_id: stack.project_id ?? '',
			plan_alert_add: stack.plan_alert_add,
			plan_alert_change: stack.plan_alert_change,
			plan_alert_destroy: stack.plan_alert_destroy,
			plan_block_on_alert: stack.plan_block_on_alert ?? false,
			budget_threshold_usd: stack.budget_threshold_usd,
			validation_interval: stack.validation_interval ?? 0
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

	async function cloneStack(e: SubmitEvent) {
		e.preventDefault();
		cloning = true;
		cloneError = null;
		try {
			const { stack_id } = await stacks.clone(stackID, cloneName);
			showCloneModal = false;
			goto(`/stacks/${stack_id}`);
		} catch (err) {
			cloneError = (err as Error).message;
		} finally {
			cloning = false;
		}
	}

	async function deleteStack() {
		if (!confirm(`Delete stack "${stack?.name}"? This cannot be undone.`)) return;
		try {
			await stacks.delete(stackID);
			goto('/stacks');
		} catch (e) {
			toast.error((e as Error).message);
		}
	}

	async function forceUnlock() {
		if (!confirm('Force-unlock state? Only do this if the run that held the lock has already stopped.')) return;
		forcingUnlock = true;
		try {
			await stacks.state.forceUnlock(stackID);
			stateResources = await stacks.state.resources(stackID);
			stateVersions = await stacks.state.versions(stackID);
		} catch (e) {
			toast.error((e as Error).message);
		} finally {
			forcingUnlock = false;
		}
	}

	async function toggleDiff(versionID: string) {
		if (expandedDiff === versionID) {
			expandedDiff = null;
			return;
		}
		expandedDiff = versionID;
		if (!loadedDiffs[versionID]) {
			try {
				loadedDiffs[versionID] = await stacks.state.versionDiff(stackID, versionID);
			} catch {
				// Non-fatal; diff just won't show.
			}
		}
	}

	async function loadPlanDiff() {
		if (!planDiffFrom || !planDiffTo) return;
		planDiffLoading = true;
		planDiffError = '';
		planDiffResult = null;
		try {
			planDiffResult = await stacks.planDiff(stackID, planDiffFrom, planDiffTo);
		} catch (e: unknown) {
			planDiffError = e instanceof Error ? e.message : 'Failed to load plan diff';
		} finally {
			planDiffLoading = false;
		}
	}

	async function triggerRun() {
		triggeringRun = true;
		try {
			const run = await runs.create(stackID, 'tracked', overrides);
			goto(`/runs/${run.id}`);
		} catch (e) {
			toast.error((e as Error).message);
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
			toast.error((e as Error).message);
			triggeringDrift = false;
		}
	}

	async function triggerValidation() {
		triggeringValidation = true;
		try {
			await stacks.validation.trigger(stackID);
			toast.success('Validation queued');
			setTimeout(async () => {
				validationResults = await stacks.validation.listResults(stackID);
			}, 3000);
		} catch (e) {
			toast.error((e as Error).message);
		} finally {
			triggeringValidation = false;
		}
	}

	async function triggerDestroy() {
		if (!stack || destroyConfirmName !== stack.name) return;
		triggeringDestroy = true;
		try {
			const run = await runs.create(stackID, 'destroy');
			goto(`/runs/${run.id}`);
		} catch (e) {
			toast.error((e as Error).message);
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
			toast.error((e as Error).message);
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
			toast.error((e as Error).message);
		}
	}

	async function attachPolicy() {
		if (!attachingPolicy) return;
		try {
			await policies.attach(stackID, attachingPolicy);
			stackPolicies = await policies.forStack(stackID);
			attachingPolicy = '';
		} catch (e) {
			toast.error((e as Error).message);
		}
	}

	async function detachPolicy(policyID: string) {
		try {
			await policies.detach(stackID, policyID);
			stackPolicies = stackPolicies.filter((p) => p.policy_id !== policyID);
		} catch (e) {
			toast.error((e as Error).message);
		}
	}

	let attachingPackID = $state('');

	async function attachPack() {
		if (!attachingPackID) return;
		try {
			await complianceApi.attachPack(stackID, attachingPackID);
			stackPolicyPacks = await complianceApi.listStackPacks(stackID);
			attachingPackID = '';
		} catch (e) {
			toast.error((e as Error).message);
		}
	}

	async function detachPack(packID: string) {
		try {
			await complianceApi.detachPack(stackID, packID);
			stackPolicyPacks = stackPolicyPacks.filter((p) => p.id !== packID);
		} catch (e) {
			toast.error((e as Error).message);
		}
	}

	const unattachedPacks = $derived(
		catalogEntries
			.filter(e => e.installed && !stackPolicyPacks.some(p => p.id === e.installed!.id))
			.map(e => e.installed!)
	);

	async function saveNotifications(e: SubmitEvent) {
		e.preventDefault();
		savingNotif = true;
		notifSaved = false;
		try {
			const data: Record<string, unknown> = { notify_events: notifEvents };
			if (notifVCSProvider) data.vcs_provider = notifVCSProvider;
			data.vcs_base_url = notifVCSBaseURL; // allow clearing
			data.vcs_username = notifVCSUsername; // allow clearing
			if (notifVCSToken !== '') data.vcs_token = notifVCSToken;
			if (notifSlackWebhook !== '') data.slack_webhook = notifSlackWebhook;
			if (notifDiscordWebhook !== '') data.discord_webhook = notifDiscordWebhook;
			if (notifTeamsWebhook !== '') data.teams_webhook = notifTeamsWebhook;
			data.gotify_url = notifGotifyURL; // allow clearing
			if (notifGotifyToken !== '') data.gotify_token = notifGotifyToken;
			data.ntfy_url = notifNtfyURL; // allow clearing
			if (notifNtfyToken !== '') data.ntfy_token = notifNtfyToken;
			data.notify_email = notifEmail; // allow clearing
			await stacks.notifications.update(stackID, data);
			notifVCSToken = '';
			notifSlackWebhook = '';
			notifDiscordWebhook = '';
			notifTeamsWebhook = '';
			notifGotifyToken = '';
			notifNtfyToken = '';
			notifSaved = true;
			stack = await stacks.get(stackID);
		} catch (err) {
			toast.error((err as Error).message);
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

	async function testDiscord() {
		testingDiscord = true;
		discordTestResult = null;
		try {
			await stacks.notifications.testDiscord(stackID);
			discordTestResult = { ok: true, msg: 'Test message sent — check your Discord channel.' };
		} catch (e) {
			discordTestResult = { ok: false, msg: (e as Error).message };
		} finally {
			testingDiscord = false;
		}
	}

	async function testTeams() {
		testingTeams = true;
		teamsTestResult = null;
		try {
			await stacks.notifications.testTeams(stackID);
			teamsTestResult = { ok: true, msg: 'Test message sent — check your Teams channel.' };
		} catch (e) {
			teamsTestResult = { ok: false, msg: (e as Error).message };
		} finally {
			testingTeams = false;
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
			toast.error((err as Error).message);
		} finally {
			savingEnv = false;
		}
	}

	function editEnvVar(ev: StackEnvVar) {
		newEnvName = ev.name;
		newEnvValue = ev.is_secret ? '' : (ev.value ?? '');
		newEnvSecret = ev.is_secret;
		setTimeout(() => {
			envValueInput?.focus();
			envValueInput?.scrollIntoView({ behavior: 'smooth', block: 'center' });
		}, 0);
	}

	async function deleteEnvVar(name: string) {
		if (!confirm(`Remove env var "${name}"?`)) return;
		try {
			await stacks.env.delete(stackID, name);
			envVars = envVars.filter((v) => v.name !== name);
		} catch (e) {
			toast.error((e as Error).message);
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
			toast.error((err as Error).message);
		} finally {
			savingIntegrations = false;
		}
	}

	async function saveGHAuth() {
		savingGHAuth = true;
		ghAuthSaved = false;
		try {
			stack = await stacks.update(stackID, {
				github_installation_uuid: selectedGHInstallation || ''
			} as Partial<Stack>);
			ghAuthSaved = true;
		} catch (err) {
			toast.error((err as Error).message);
		} finally {
			savingGHAuth = false;
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

	async function saveOIDC(e: SubmitEvent) {
		e.preventDefault();
		savingOIDC = true;
		oidcSaved = false;
		oidcError = null;
		try {
			const body: Partial<CloudOIDCConfig> = { provider: oidcProvider };
			if (oidcProvider === 'aws') {
				body.aws_role_arn = oidcAWSRoleARN;
			} else if (oidcProvider === 'gcp') {
				body.gcp_workload_identity_audience = oidcGCPAudience;
				body.gcp_service_account_email = oidcGCPSA;
			} else if (oidcProvider === 'azure') {
				body.azure_tenant_id = oidcAzureTenant;
				body.azure_client_id = oidcAzureClient;
				body.azure_subscription_id = oidcAzureSubscription;
			} else if (oidcProvider === 'vault') {
				body.vault_addr = oidcVaultAddr;
				body.vault_role = oidcVaultRole;
				if (oidcVaultMount) body.vault_mount = oidcVaultMount;
			} else if (oidcProvider === 'authentik') {
				body.authentik_url = oidcAuthentikURL;
				body.authentik_client_id = oidcAuthentikClientID;
			} else if (oidcProvider === 'generic') {
				body.generic_token_url = oidcGenericTokenURL;
				if (oidcGenericClientID) body.generic_client_id = oidcGenericClientID;
				if (oidcGenericScope) body.generic_scope = oidcGenericScope;
			}
			if (oidcAudienceOverride) body.audience_override = oidcAudienceOverride;
			oidcConfig = await cloudOIDC.upsert(stackID, body);
			oidcSaved = true;
			setTimeout(() => (oidcSaved = false), 2000);
		} catch (err) {
			oidcError = (err as Error).message;
		} finally {
			savingOIDC = false;
		}
	}

	async function deleteOIDC() {
		if (!confirm('Remove cloud OIDC federation config? Runs will no longer receive OIDC tokens.')) return;
		try {
			await cloudOIDC.delete(stackID);
			oidcConfig = null;
			oidcAWSRoleARN = '';
			oidcGCPAudience = '';
			oidcGCPSA = '';
			oidcAzureTenant = '';
			oidcAzureClient = '';
			oidcAzureSubscription = '';
			oidcVaultAddr = '';
			oidcVaultRole = '';
			oidcVaultMount = '';
			oidcAuthentikURL = '';
			oidcAuthentikClientID = '';
			oidcGenericTokenURL = '';
			oidcGenericClientID = '';
			oidcGenericScope = '';
			oidcAudienceOverride = '';
		} catch (err) {
			toast.error((err as Error).message);
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
			toast.error((err as Error).message);
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
			toast.error((e as Error).message);
		} finally {
			togglingDisabled = false;
		}
	}

	async function toggleLock() {
		if (!stack) return;
		if (stack.is_locked) {
			if (!confirm('Unlock this stack?')) return;
			togglingLocked = true;
			try {
				await stacks.unlock(stackID);
				stack = await stacks.get(stackID);
			} catch (e) {
				toast.error((e as Error).message);
			} finally {
				togglingLocked = false;
			}
		} else {
			const reason = prompt('Lock reason (optional):') ?? null;
			if (reason === null) return;
			togglingLocked = true;
			try {
				await stacks.lock(stackID, reason);
				stack = await stacks.get(stackID);
			} catch (e) {
				toast.error((e as Error).message);
			} finally {
				togglingLocked = false;
			}
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
			toast.error((e as Error).message);
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
			toast.error((e as Error).message);
		} finally {
			addingMember = false;
		}
	}

	async function removeMember(userID: string) {
		try {
			await stackMembers.remove(stackID, userID);
			members = members.filter((m) => m.user_id !== userID);
		} catch (e) {
			toast.error((e as Error).message);
		}
	}

	async function updateMemberRole(userID: string, role: 'viewer' | 'approver') {
		try {
			await stackMembers.upsert(stackID, userID, role);
			members = members.map((m) => m.user_id === userID ? { ...m, role } : m);
		} catch (e) {
			toast.error((e as Error).message);
		}
	}

	async function rotateWebhookSecret() {
		if (!confirm('Rotate the webhook secret? The old secret will stop working immediately — update your repository webhook settings before the next push.')) return;
		rotatingWebhook = true;
		newWebhookSecret = null;
		try {
			const res = await stacks.webhook.rotateSecret(stackID);
			newWebhookSecret = res.webhook_secret;
			sessionStorage.setItem(`webhook_secret_${stackID}`, res.webhook_secret);
		} catch (e) {
			toast.error((e as Error).message);
		} finally {
			rotatingWebhook = false;
		}
	}

	async function toggleDeliveryPayload(deliveryID: string) {
		if (expandedDeliveryID === deliveryID) {
			expandedDeliveryID = null;
			return;
		}
		expandedDeliveryID = deliveryID;
		if (deliveryPayloads[deliveryID] !== undefined) return;
		try {
			const res = await stacks.webhook.deliveryPayload(stackID, deliveryID);
			deliveryPayloads = { ...deliveryPayloads, [deliveryID]: res.payload };
		} catch {
			deliveryPayloads = { ...deliveryPayloads, [deliveryID]: null };
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
			toast.error((e as Error).message);
		}
	}

	async function detachVarSet(vsID: string) {
		try {
			await varSets.detachFromStack(stackID, vsID);
			stackVarSets = stackVarSets.filter((s) => s.id !== vsID);
		} catch (e) {
			toast.error((e as Error).message);
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
			toast.error((err as Error).message);
		} finally {
			removingStateBackend = false;
		}
	}

	const unattachedPolicies = $derived(
		allPolicies.filter((p) => !stackPolicies.some((sp) => sp.policy_id === p.id))
	);

	const statusColour: Record<string, string> = {
		queued: 'text-zinc-400',
		preparing: 'text-teal-400',
		planning: 'text-teal-400',
		unconfirmed: 'text-yellow-400',
		pending_approval: 'text-purple-400',
		confirmed: 'text-teal-400',
		applying: 'text-teal-400',
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

	function parseHeaders(raw: string): Record<string, string> {
		const result: Record<string, string> = {};
		for (const line of raw.split('\n')) {
			const colon = line.indexOf(':');
			if (colon === -1) continue;
			const key = line.slice(0, colon).trim();
			const val = line.slice(colon + 1).trim();
			if (key) result[key] = val;
		}
		return result;
	}

	async function addOutgoingWebhook(e: Event) {
		e.preventDefault();
		if (!newOWURL.trim()) return;
		addingOW = true;
		owError = null;
		owRevealedSecret = null;
		try {
			const created = await stacks.outgoingWebhooks.create(stackID, {
				url: newOWURL.trim(),
				event_types: newOWEvents,
				headers: parseHeaders(newOWHeaders),
				with_secret: newOWWithSecret
			});
			outgoingWebhooks = [...outgoingWebhooks, created];
			if (created.secret) owRevealedSecret = created.secret;
			newOWURL = '';
			newOWHeaders = '';
			newOWEvents = ['plan_complete', 'run_finished', 'run_failed'];
			newOWWithSecret = true;
		} catch (err) {
			owError = (err as Error).message;
		} finally {
			addingOW = false;
		}
	}

	async function toggleOWActive(wh: OutgoingWebhook) {
		await stacks.outgoingWebhooks.update(stackID, wh.id, { is_active: !wh.is_active });
		outgoingWebhooks = outgoingWebhooks.map(w => w.id === wh.id ? { ...w, is_active: !wh.is_active } : w);
	}

	async function deleteOW(id: string) {
		await stacks.outgoingWebhooks.delete(stackID, id);
		outgoingWebhooks = outgoingWebhooks.filter(w => w.id !== id);
	}

	async function rotateOWSecret(id: string) {
		const res = await stacks.outgoingWebhooks.rotateSecret(stackID, id);
		owRevealedSecret = res.secret;
	}

	async function toggleOWDeliveries(id: string) {
		if (expandedOWID === id) {
			expandedOWID = null;
			return;
		}
		expandedOWID = id;
		if (!owDeliveries[id]) {
			const d = await stacks.outgoingWebhooks.deliveries(stackID, id);
			owDeliveries = { ...owDeliveries, [id]: d };
		}
	}
</script>

<svelte:window onkeydown={(e) => { if (e.key === 'Escape' && editing) { editing = false; } }} />

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

	<!-- Locked banner -->
	{#if stack.is_locked}
		<div class="bg-amber-950 border border-amber-900 rounded-xl px-5 py-3">
			<p class="text-amber-300 text-sm font-semibold">This stack is locked.</p>
			{#if stack.lock_reason}
				<p class="text-amber-400 text-xs mt-0.5">{stack.lock_reason}</p>
			{/if}
			<p class="text-amber-600 text-xs mt-0.5">New runs cannot be triggered until the stack is unlocked.</p>
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
					 stack.tool === 'terragrunt' ? 'bg-green-900 text-green-300' :
					 'bg-sky-900 text-sky-300'}">
					{stack.tool}
				</span>
				{#if stack.description}
					<span class="text-zinc-400 text-sm">{stack.description}</span>
				{/if}
				<!-- Tag pills in header -->
				{#if stack.tags?.length > 0}
					<div class="flex items-center gap-1.5 mt-1 flex-wrap">
						{#each stack.tags as tag (tag.id)}
							<span class="inline-flex items-center gap-1 text-xs px-2 py-0.5 rounded-full border"
								style="border-color: {tag.color}33; background: {tag.color}18; color: var(--color-zinc-300);">
								<span class="w-1.5 h-1.5 rounded-full flex-shrink-0" style="background: {tag.color};"></span>
								{tag.name}
							</span>
						{/each}
					</div>
				{/if}
			</div>
		</div>
		<div class="flex items-center gap-2">
			<!-- Primary run actions -->
			{#if stack.my_stack_role !== 'viewer'}
				<button onclick={triggerRun} disabled={triggeringRun}
					class="bg-teal-600 hover:bg-teal-500 disabled:opacity-50 text-white text-sm px-3 py-1.5 rounded-lg transition-colors">
					{triggeringRun ? 'Queuing…' : 'Trigger run'}
				</button>
				<button
					onclick={() => { showOverrides = !showOverrides; }}
					title="Variable overrides for this run"
					class="border transition-colors text-sm px-2 py-1.5 rounded-lg
						{overrides.length > 0
							? 'border-teal-600 text-teal-400 hover:border-teal-400'
							: 'border-zinc-700 text-zinc-500 hover:border-zinc-500 hover:text-zinc-300'}">
					{overrides.length > 0 ? `Overrides (${overrides.length})` : 'Overrides'}
				</button>
				{#if stack.drift_detection}
					<button onclick={triggerDrift} disabled={triggeringDrift}
						class="border border-zinc-700 hover:border-zinc-500 text-zinc-300 text-sm px-3 py-1.5 rounded-lg transition-colors">
						{triggeringDrift ? 'Queuing…' : 'Drift check'}
					</button>
				{/if}
				{#if stack.validation_interval > 0}
					<button onclick={triggerValidation} disabled={triggeringValidation}
						class="border border-zinc-700 hover:border-zinc-500 text-zinc-300 text-sm px-3 py-1.5 rounded-lg transition-colors">
						{triggeringValidation ? 'Queuing…' : 'Validate'}
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
				<button onclick={() => { cloneName = `Copy of ${stack?.name ?? ''}`; cloneError = null; showCloneModal = true; }}
					class="border border-zinc-700 hover:border-zinc-500 text-zinc-400 text-sm px-3 py-1.5 rounded-lg transition-colors">
					Clone
				</button>
			{/if}
			{#if auth.isMemberOrAbove}
				<button onclick={toggleDisabled} disabled={togglingDisabled}
					class="border border-zinc-700 hover:border-zinc-500 text-zinc-400 text-sm px-3 py-1.5 rounded-lg transition-colors disabled:opacity-50">
					{togglingDisabled ? '…' : stack.is_disabled ? 'Enable' : 'Disable'}
				</button>
				<button onclick={toggleLock} disabled={togglingLocked}
					class="border text-sm px-3 py-1.5 rounded-lg transition-colors disabled:opacity-50
						{stack.is_locked
							? 'border-amber-700 hover:border-amber-500 text-amber-400'
							: 'border-zinc-700 hover:border-zinc-500 text-zinc-400'}">
					{togglingLocked ? '…' : stack.is_locked ? 'Unlock' : 'Lock'}
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
		<div class="divide-y divide-zinc-700 border border-zinc-800 rounded-lg overflow-hidden">
			{#each overrides as ov}
			<div class="flex items-center gap-2 px-3 py-2 bg-zinc-900">
				<code class="text-xs text-teal-300 font-mono flex-shrink-0">{ov.key}</code>
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
				<input id="edit-repo" class="field-input font-mono text-sm" bind:value={form.repo_url} placeholder="https://github.com/org/repo" required />
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
				<label class="field-label" for="edit-validation-interval">Continuous validation interval (minutes)</label>
				<input id="edit-validation-interval" type="number" min="0" step="5"
					class="field-input w-32"
					bind:value={form.validation_interval}
					placeholder="0" />
				<p class="text-xs text-zinc-600">Set to 0 to disable. Requires at least one <em>validation</em>-type policy attached to the stack.</p>
			</div>
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
			<div class="space-y-3">
				<p class="field-label">Cron schedules <span class="font-normal text-zinc-500">(5-field cron — leave blank to disable)</span></p>
				<p class="text-xs text-zinc-600">Examples: <span class="font-mono">0 2 * * *</span> = 2 am daily &nbsp;·&nbsp; <span class="font-mono">0 6 * * 1</span> = 6 am every Monday &nbsp;·&nbsp; <span class="font-mono">0 */6 * * *</span> = every 6 hours</p>
				<div class="grid grid-cols-3 gap-4">
					<div class="space-y-1.5">
						<label class="field-label font-normal text-zinc-400" for="edit-plan-sched">Plan schedule</label>
						<input id="edit-plan-sched" class="field-input font-mono text-sm"
							placeholder="0 2 * * *" bind:value={form.plan_schedule} />
						{#if stack.plan_next_run_at && form.plan_schedule === (stack.plan_schedule ?? '')}
							<p class="text-xs text-zinc-600">Next: {new Date(stack.plan_next_run_at).toLocaleString()}</p>
						{/if}
					</div>
					<div class="space-y-1.5">
						<label class="field-label font-normal text-zinc-400" for="edit-apply-sched">Apply schedule</label>
						<input id="edit-apply-sched" class="field-input font-mono text-sm"
							placeholder="0 6 * * 1" bind:value={form.apply_schedule} />
						{#if stack.apply_next_run_at && form.apply_schedule === (stack.apply_schedule ?? '')}
							<p class="text-xs text-zinc-600">Next: {new Date(stack.apply_next_run_at).toLocaleString()}</p>
						{/if}
					</div>
					<div class="space-y-1.5">
						<label class="field-label font-normal text-zinc-400" for="edit-destroy-sched">Destroy schedule</label>
						<input id="edit-destroy-sched" class="field-input font-mono text-sm"
							placeholder="0 22 * * 5" bind:value={form.destroy_schedule} />
						{#if stack.destroy_next_run_at && form.destroy_schedule === (stack.destroy_schedule ?? '')}
							<p class="text-xs text-zinc-600">Next: {new Date(stack.destroy_next_run_at).toLocaleString()}</p>
						{/if}
					</div>
				</div>
			</div>
			<div class="space-y-3">
				<p class="field-label">Lifecycle hooks <span class="font-normal text-zinc-500">(bash scripts — leave blank to skip)</span></p>
				<div class="grid grid-cols-2 gap-4">
					<div class="space-y-1.5">
						<label class="field-label font-normal text-zinc-400" for="edit-pre-plan">Pre-plan</label>
						<textarea id="edit-pre-plan" class="field-input font-mono text-xs h-24 resize-y"
							placeholder="#!/usr/bin/env bash&#10;echo 'before plan'" bind:value={form.pre_plan_hook}></textarea>
					</div>
					<div class="space-y-1.5">
						<label class="field-label font-normal text-zinc-400" for="edit-post-plan">Post-plan</label>
						<textarea id="edit-post-plan" class="field-input font-mono text-xs h-24 resize-y"
							placeholder="#!/usr/bin/env bash&#10;echo 'after plan'" bind:value={form.post_plan_hook}></textarea>
					</div>
					<div class="space-y-1.5">
						<label class="field-label font-normal text-zinc-400" for="edit-pre-apply">Pre-apply</label>
						<textarea id="edit-pre-apply" class="field-input font-mono text-xs h-24 resize-y"
							placeholder="#!/usr/bin/env bash&#10;echo 'before apply'" bind:value={form.pre_apply_hook}></textarea>
					</div>
					<div class="space-y-1.5">
						<label class="field-label font-normal text-zinc-400" for="edit-post-apply">Post-apply</label>
						<textarea id="edit-post-apply" class="field-input font-mono text-xs h-24 resize-y"
							placeholder="#!/usr/bin/env bash&#10;echo 'after apply'" bind:value={form.post_apply_hook}></textarea>
					</div>
				</div>
				<p class="text-xs text-zinc-600">Hooks run inside the runner container with full access to stack env vars. A non-zero exit fails the run.</p>
			</div>
			<div class="space-y-1.5">
				<label class="field-label" for="edit-max-concurrent">Max concurrent runs <span class="font-normal text-zinc-500">(0 = unlimited)</span></label>
				<input id="edit-max-concurrent" type="number" min="0" max="99" class="field-input w-32"
					bind:value={form.max_concurrent_runs} placeholder="0" />
				<p class="text-xs text-zinc-600">Set to 1 to ensure this stack only runs one job at a time (recommended for production).</p>
			</div>
			<div class="space-y-3 rounded-lg border border-zinc-800 p-4">
				<div class="flex items-center gap-3">
					<input id="edit-pr-preview" type="checkbox" class="h-4 w-4 rounded border-zinc-600 bg-zinc-800 text-teal-500"
						bind:checked={form.pr_preview_enabled} />
					<label class="field-label mb-0" for="edit-pr-preview">PR preview environments</label>
				</div>
				{#if form.pr_preview_enabled}
					<div class="space-y-1.5">
						<label class="field-label" for="edit-pr-template">Template</label>
						<select id="edit-pr-template" class="field-input" bind:value={form.pr_preview_template_id}>
							<option value="">— select a template —</option>
							{#each allTemplates as t (t.id)}
								<option value={t.id}>{t.name}</option>
							{/each}
						</select>
						<p class="text-xs text-zinc-600">When a PR opens against this stack's repo, Crucible creates a preview stack from this template using the PR branch, then destroys it when the PR closes.</p>
					</div>
				{/if}
			</div>
			<div class="space-y-1.5">
				<label class="field-label" for="edit-worker-pool">Worker pool</label>
				<select id="edit-worker-pool" class="field-input" bind:value={form.worker_pool_id}>
					<option value="">Built-in runner (default)</option>
					{#each allWorkerPools as wp (wp.id)}
						<option value={wp.id}>{wp.name}</option>
					{/each}
				</select>
				<p class="text-xs text-zinc-600">Run this stack's jobs on an external agent pool instead of the built-in Docker runner.</p>
			</div>
			{#if allProjects.length > 0}
				<div class="space-y-1.5">
					<label class="field-label" for="edit-project">Project</label>
					<select id="edit-project" class="field-input" bind:value={form.project_id}>
						<option value="">— unassigned —</option>
						{#each allProjects as p (p.id)}
							<option value={p.id}>{p.name}</option>
						{/each}
					</select>
				</div>
			{/if}
			<!-- Budget alerts -->
			<div class="space-y-3 rounded-lg border border-zinc-800 p-4">
				<p class="field-label uppercase tracking-wide">Budget alerts</p>
				<p class="text-xs text-zinc-500">Notify (and optionally block auto-apply) when a plan exceeds these resource-change limits. Leave blank to disable.</p>
				<div class="grid grid-cols-3 gap-4">
					<div class="space-y-1.5">
						<label class="field-label" for="edit-alert-add">Max adds</label>
						<input id="edit-alert-add" type="number" min="0" class="field-input w-full"
							placeholder="e.g. 20"
							value={form.plan_alert_add ?? ''}
							oninput={(e) => form.plan_alert_add = (e.currentTarget as HTMLInputElement).value === '' ? undefined : Number((e.currentTarget as HTMLInputElement).value)} />
					</div>
					<div class="space-y-1.5">
						<label class="field-label" for="edit-alert-change">Max changes</label>
						<input id="edit-alert-change" type="number" min="0" class="field-input w-full"
							placeholder="e.g. 10"
							value={form.plan_alert_change ?? ''}
							oninput={(e) => form.plan_alert_change = (e.currentTarget as HTMLInputElement).value === '' ? undefined : Number((e.currentTarget as HTMLInputElement).value)} />
					</div>
					<div class="space-y-1.5">
						<label class="field-label" for="edit-alert-destroy">Max destroys</label>
						<input id="edit-alert-destroy" type="number" min="0" class="field-input w-full"
							placeholder="e.g. 5"
							value={form.plan_alert_destroy ?? ''}
							oninput={(e) => form.plan_alert_destroy = (e.currentTarget as HTMLInputElement).value === '' ? undefined : Number((e.currentTarget as HTMLInputElement).value)} />
					</div>
				</div>
				<label class="flex items-center gap-2 cursor-pointer text-sm text-zinc-300">
					<input type="checkbox" bind:checked={form.plan_block_on_alert} class="rounded border-zinc-700 bg-zinc-900 text-teal-500" />
					Block auto-apply when a budget threshold is exceeded
				</label>
				<div class="space-y-1.5 pt-1">
					<label class="field-label" for="edit-budget-threshold">Infracost cost add limit (USD/mo)</label>
					<input id="edit-budget-threshold" type="number" min="0" step="0.01" class="field-input w-full max-w-[200px]"
						placeholder="e.g. 100.00"
						value={form.budget_threshold_usd ?? ''}
						oninput={(e) => form.budget_threshold_usd = (e.currentTarget as HTMLInputElement).value === '' ? undefined : Number((e.currentTarget as HTMLInputElement).value)} />
					<p class="text-xs text-zinc-600">Alert (and optionally block) when Infracost estimated monthly cost add exceeds this amount. Leave blank to disable.</p>
				</div>
			</div>

			<div class="flex gap-3 pt-1">
				<button type="submit" disabled={saving}
					class="bg-teal-600 hover:bg-teal-500 disabled:opacity-50 text-white text-sm px-4 py-1.5 rounded-lg transition-colors">
					{saving ? 'Saving…' : 'Save changes'}
				</button>
			</div>
		</form>
	</div>
	{/if}

	<!-- Stack details -->
	<div class="border border-zinc-800 rounded-xl divide-y divide-zinc-700 text-sm">
		{#each [
			['Repository', stack.repo_url],
			['Branch', stack.repo_branch],
			['Project root', stack.project_root],
			['Auto-apply', stack.auto_apply ? 'Yes' : 'No'],
			['Drift detection', stack.drift_detection ? 'Yes' : 'No'],
			['Drift interval', stack.drift_detection ? driftScheduleLabel(stack.drift_schedule) : '—'],
			['Auto-remediate drift', stack.drift_detection ? (stack.auto_remediate_drift ? 'Yes' : 'No') : '—'],
			['Scheduled destroy', stack.scheduled_destroy_at ? fmtDate(stack.scheduled_destroy_at) + ' UTC' : '—'],
			['Plan schedule', stack.plan_schedule || '—'],
			['Apply schedule', stack.apply_schedule || '—'],
			['Destroy schedule', stack.destroy_schedule || '—'],
			['Created', fmtDate(stack.created_at)]
		] as [label, value]}
			<div class="flex px-4 py-3">
				<span class="w-36 flex-shrink-0 text-zinc-500">{label}</span>
				<span class="text-zinc-200 font-mono text-xs break-all">{value}</span>
			</div>
		{/each}
	</div>

	<!-- Lifecycle hooks (read-only — edit via the edit form above) -->
	{#if stack.pre_plan_hook || stack.post_plan_hook || stack.pre_apply_hook || stack.post_apply_hook}
	<div class="border border-zinc-800 rounded-xl overflow-hidden">
		<div class="bg-zinc-900 px-4 py-2 text-xs text-zinc-500 uppercase tracking-wide font-medium">Lifecycle hooks</div>
		{#each [
			['Pre-plan', stack.pre_plan_hook],
			['Post-plan', stack.post_plan_hook],
			['Pre-apply', stack.pre_apply_hook],
			['Post-apply', stack.post_apply_hook],
		] as [label, script]}
			{#if script}
			<div class="border-t border-zinc-800 px-4 py-3 space-y-1">
				<span class="text-xs text-zinc-500">{label}</span>
				<pre class="text-xs text-zinc-300 font-mono whitespace-pre-wrap break-all leading-relaxed">{script}</pre>
			</div>
			{/if}
		{/each}
	</div>
	{/if}

	<!-- Continuous validation -->
	{#if stack.validation_interval > 0}
	{@const vs = stack.validation_status}
	<section class="space-y-3">
		<div class="flex items-center justify-between">
			<h2 class="text-sm font-medium text-zinc-400 uppercase tracking-wide">Continuous Validation</h2>
			<div class="flex items-center gap-3">
				<span class="flex items-center gap-1.5 text-xs {vs === 'pass' ? 'text-green-400' : vs === 'warn' ? 'text-yellow-400' : vs === 'fail' ? 'text-red-400' : 'text-zinc-500'}">
					<span class="h-2 w-2 rounded-full {vs === 'pass' ? 'bg-green-400' : vs === 'warn' ? 'bg-yellow-400' : vs === 'fail' ? 'bg-red-400' : 'bg-zinc-600'}"></span>
					{vs}
					{#if stack.last_validated_at}
						<span class="text-zinc-600 ml-1">· {fmtDate(stack.last_validated_at)}</span>
					{/if}
				</span>
				<span class="text-xs text-zinc-600">every {stack.validation_interval} min</span>
			</div>
		</div>
		{#if validationResults.length === 0}
			<p class="text-zinc-600 text-sm">No validation runs yet — click Validate or wait for the next scheduled check.</p>
		{:else}
			<div class="border border-zinc-800 rounded-xl overflow-hidden">
				{#each validationResults.slice(0, 5) as result (result.id)}
					<div class="border-b border-zinc-800 last:border-b-0 px-4 py-3 space-y-2">
						<div class="flex items-center justify-between">
							<span class="flex items-center gap-2">
								<span class="h-2 w-2 rounded-full {result.status === 'pass' ? 'bg-green-400' : result.status === 'warn' ? 'bg-yellow-400' : 'bg-red-400'}"></span>
								<span class="text-sm font-medium {result.status === 'pass' ? 'text-green-400' : result.status === 'warn' ? 'text-yellow-400' : 'text-red-400'}">{result.status}</span>
								{#if result.deny_count > 0}
									<span class="text-xs text-red-400">{result.deny_count} violation{result.deny_count === 1 ? '' : 's'}</span>
								{/if}
								{#if result.warn_count > 0}
									<span class="text-xs text-yellow-400">{result.warn_count} warning{result.warn_count === 1 ? '' : 's'}</span>
								{/if}
							</span>
							<span class="text-xs text-zinc-600">{fmtDate(result.evaluated_at)}</span>
						</div>
						{#if result.details?.length > 0 && result.status !== 'pass'}
							<div class="space-y-1 pl-4">
								{#each result.details.filter(d => d.status !== 'pass') as d (d.policy_id)}
									<div class="text-xs text-zinc-400">
										<span class="text-zinc-500">{d.policy_name}:</span>
										{#each [...(d.deny ?? []), ...(d.warn ?? [])] as msg}
											<span class="block pl-2 text-zinc-400">{msg}</span>
										{/each}
									</div>
								{/each}
							</div>
						{/if}
					</div>
				{/each}
			</div>
		{/if}
	</section>
	{/if}

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
					<tbody class="divide-y divide-zinc-700">
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

	<!-- State version history -->
	<section class="space-y-3">
		<h2 class="text-sm font-medium text-zinc-400 uppercase tracking-wide">State History</h2>
		{#if stateVersions.length === 0}
			<p class="text-zinc-600 text-sm">No state versions recorded yet — versions are captured on each successful apply.</p>
		{:else}
			<div class="border border-zinc-800 rounded-xl overflow-hidden">
				<table class="w-full text-sm">
					<thead class="bg-zinc-900 text-zinc-500 text-xs uppercase tracking-wide">
						<tr>
							<th class="text-left px-4 py-2">Serial</th>
							<th class="text-left px-4 py-2">Resources</th>
							<th class="text-left px-4 py-2">Run</th>
							<th class="text-left px-4 py-2">Date</th>
							<th class="px-4 py-2"></th>
						</tr>
					</thead>
					<tbody class="divide-y divide-zinc-800">
						{#each stateVersions as v (v.id)}
							<tr class="hover:bg-zinc-800/40 transition-colors">
								<td class="px-4 py-2.5 font-mono text-xs text-zinc-300">#{v.serial}</td>
								<td class="px-4 py-2.5 text-zinc-400 text-xs">{v.resource_count}</td>
								<td class="px-4 py-2.5">
									{#if v.run_id}
										<a href="/runs/{v.run_id}" class="text-xs font-mono text-zinc-400 hover:text-zinc-200 transition-colors">
											{v.run_id.slice(0, 8)}…
										</a>
									{:else}
										<span class="text-zinc-700 text-xs">—</span>
									{/if}
								</td>
								<td class="px-4 py-2.5 text-zinc-500 text-xs">{new Date(v.created_at).toLocaleString()}</td>
								<td class="px-4 py-2.5 text-right">
									<button
										onclick={() => toggleDiff(v.id)}
										class="text-xs px-2.5 py-1 rounded-lg border transition-colors"
										class:border-zinc-700={expandedDiff !== v.id}
										class:text-zinc-400={expandedDiff !== v.id}
										class:hover:border-zinc-500={expandedDiff !== v.id}
										style={expandedDiff === v.id ? 'background:var(--accent-muted);color:var(--accent);border-color:var(--accent-border)' : ''}>
										{expandedDiff === v.id ? 'Hide diff' : 'Diff'}
									</button>
								</td>
							</tr>
							{#if expandedDiff === v.id}
								{@const diff = loadedDiffs[v.id]}
								<tr>
									<td colspan="5" class="px-4 py-3 bg-zinc-900/60">
										{#if !diff}
											<p class="text-zinc-500 text-xs">Loading…</p>
										{:else if diff.added.length === 0 && diff.removed.length === 0 && diff.changed.length === 0}
											<p class="text-zinc-500 text-xs">No resource changes in this version.</p>
										{:else}
											<div class="space-y-3 text-xs">
												{#if diff.added.length > 0}
													<div>
														<p class="text-green-400 font-medium mb-1">+ Added ({diff.added.length})</p>
														{#each diff.added as r (r.address)}
															<div class="font-mono text-green-300/80 ml-2">+ {r.address}</div>
														{/each}
													</div>
												{/if}
												{#if diff.removed.length > 0}
													<div>
														<p class="text-red-400 font-medium mb-1">− Removed ({diff.removed.length})</p>
														{#each diff.removed as r (r.address)}
															<div class="font-mono text-red-300/80 ml-2">− {r.address}</div>
														{/each}
													</div>
												{/if}
												{#if diff.changed.length > 0}
													<div>
														<p class="text-yellow-400 font-medium mb-1">~ Changed ({diff.changed.length})</p>
														{#each diff.changed as r (r.address)}
															<div class="ml-2 mb-2">
																<div class="font-mono text-yellow-300/80 mb-1">~ {r.address}</div>
																{#each Object.keys({ ...r.before, ...r.after }) as k (k)}
																	<div class="ml-4 font-mono">
																		{#if k in r.before}
																			<div class="text-red-300/70">- {k} = {JSON.stringify(r.before[k])}</div>
																		{/if}
																		{#if k in r.after}
																			<div class="text-green-300/70">+ {k} = {JSON.stringify(r.after[k])}</div>
																		{/if}
																	</div>
																{/each}
															</div>
														{/each}
													</div>
												{/if}
											</div>
										{/if}
									</td>
								</tr>
							{/if}
						{/each}
					</tbody>
				</table>
			</div>
		{/if}
	</section>

	<!-- Plan comparison -->
	<section class="space-y-3">
		<h2 class="text-sm font-medium text-zinc-400 uppercase tracking-wide">Plan Comparison</h2>
		{#if planRuns.length < 2}
			<p class="text-zinc-600 text-sm">At least two plan runs with recorded changes are needed to compare. Run a plan to start building history.</p>
		{:else}
			<div class="border border-zinc-800 rounded-xl p-4 space-y-4">
				<div class="flex items-end gap-3">
					<div class="flex-1 space-y-1">
						<label class="text-xs text-zinc-500 uppercase tracking-wide" for="plan-diff-from">From run</label>
						<select id="plan-diff-from" class="field-input text-sm" bind:value={planDiffFrom}>
							<option value="">Select a run…</option>
							{#each planRuns as r (r.id)}
								<option value={r.id}>{r.id.slice(0, 8)}… — {r.type} ({new Date(r.queued_at).toLocaleDateString()})</option>
							{/each}
						</select>
					</div>
					<div class="flex-1 space-y-1">
						<label class="text-xs text-zinc-500 uppercase tracking-wide" for="plan-diff-to">To run</label>
						<select id="plan-diff-to" class="field-input text-sm" bind:value={planDiffTo}>
							<option value="">Select a run…</option>
							{#each planRuns as r (r.id)}
								<option value={r.id}>{r.id.slice(0, 8)}… — {r.type} ({new Date(r.queued_at).toLocaleDateString()})</option>
							{/each}
						</select>
					</div>
					<button
						onclick={loadPlanDiff}
						disabled={!planDiffFrom || !planDiffTo || planDiffFrom === planDiffTo || planDiffLoading}
						class="px-4 py-2 text-sm rounded-lg border border-zinc-700 hover:border-zinc-500 text-zinc-300 transition-colors disabled:opacity-40">
						{planDiffLoading ? 'Loading…' : 'Compare'}
					</button>
				</div>

				{#if planDiffError}
					<p class="text-xs text-red-400">{planDiffError}</p>
				{/if}

				{#if planDiffResult}
					{@const pd = planDiffResult}
					{#if pd.new.length === 0 && pd.removed.length === 0 && pd.changed.length === 0}
						<p class="text-zinc-500 text-xs">No differences between the two plans.</p>
					{:else}
						<div class="space-y-3 text-xs">
							{#if pd.new.length > 0}
								<div>
									<p class="text-green-400 font-medium mb-1">+ New in plan ({pd.new.length})</p>
									{#each pd.new as r (r.address)}
										<div class="font-mono text-green-300/80 ml-2">+ {r.address} <span class="text-zinc-500">[{r.actions.join(',')}]</span></div>
									{/each}
								</div>
							{/if}
							{#if pd.removed.length > 0}
								<div>
									<p class="text-red-400 font-medium mb-1">− Removed from plan ({pd.removed.length})</p>
									{#each pd.removed as r (r.address)}
										<div class="font-mono text-red-300/80 ml-2">− {r.address} <span class="text-zinc-500">[{r.actions.join(',')}]</span></div>
									{/each}
								</div>
							{/if}
							{#if pd.changed.length > 0}
								<div>
									<p class="text-yellow-400 font-medium mb-1">~ Changed ({pd.changed.length})</p>
									{#each pd.changed as r (r.address)}
										<div class="ml-2 mb-2">
											<div class="font-mono text-yellow-300/80 mb-1">~ {r.address}</div>
											{#if r.from_actions.join(',') !== r.to_actions.join(',')}
												<div class="ml-4 font-mono text-zinc-400">action: <span class="text-red-300/70">{r.from_actions.join(',')}</span> → <span class="text-green-300/70">{r.to_actions.join(',')}</span></div>
											{/if}
											{#if r.attrs_before && r.attrs_after}
												{#each Object.keys({ ...r.attrs_before, ...r.attrs_after }) as k (k)}
													<div class="ml-4 font-mono">
														{#if k in (r.attrs_before ?? {})}
															<div class="text-red-300/70">- {k} = {JSON.stringify(r.attrs_before?.[k])}</div>
														{/if}
														{#if k in (r.attrs_after ?? {})}
															<div class="text-green-300/70">+ {k} = {JSON.stringify(r.attrs_after?.[k])}</div>
														{/if}
													</div>
												{/each}
											{/if}
										</div>
									{/each}
								</div>
							{/if}
						</div>
					{/if}
				{/if}
			</div>
		{/if}
	</section>

	<!-- Cost history sparkline -->
	{#if costHistory.length > 0}
	<section class="space-y-3">
		<div class="flex items-center justify-between">
			<h2 class="text-sm font-medium text-zinc-400 uppercase tracking-wide">Infracost history</h2>
			{#if stack.budget_threshold_usd}
				<span class="text-xs text-zinc-500">Budget: <span class="text-zinc-300">${stack.budget_threshold_usd.toLocaleString('en-US', { minimumFractionDigits: 2, maximumFractionDigits: 2 })}/mo</span></span>
			{/if}
		</div>
		<div class="rounded-xl border border-zinc-800 p-4 space-y-3">
			<p class="text-xs text-zinc-500">Estimated monthly cost add — last {costHistory.length} runs with cost data (newest → oldest)</p>
			<div class="flex items-end gap-1 h-16">
				{#each [...costHistory].reverse() as p (p.run_id)}
					{@const h = Math.round((p.cost_add / maxCostAdd) * 100)}
					{@const overBudget = stack.budget_threshold_usd != null && p.cost_add > stack.budget_threshold_usd}
					<div class="flex-1 flex flex-col justify-end"
						title="{new Date(p.queued_at).toLocaleDateString()}: +${p.cost_add.toFixed(2)} / ~${p.cost_change.toFixed(2)} / -${p.cost_remove.toFixed(2)} {p.currency}">
						{#if h > 0}
							<div class="rounded-sm {overBudget ? 'bg-red-500' : 'bg-orange-500'}" style="height:{h}%"></div>
						{:else}
							<div class="rounded-sm bg-zinc-700" style="height:2px"></div>
						{/if}
					</div>
				{/each}
			</div>
			{#if stack.budget_threshold_usd}
				<div class="flex items-center gap-4 text-xs text-zinc-500">
					<span class="flex items-center gap-1.5"><span class="w-2 h-2 rounded-sm bg-orange-500 inline-block"></span>Within budget</span>
					<span class="flex items-center gap-1.5"><span class="w-2 h-2 rounded-sm bg-red-500 inline-block"></span>Over budget</span>
				</div>
			{/if}
		</div>
	</section>
	{/if}

	<!-- Tags -->
	{#if stack.my_stack_role !== 'viewer'}
	<section class="space-y-3">
		<div class="flex items-center justify-between">
			<h2 class="text-sm font-medium text-zinc-400 uppercase tracking-wide">Tags</h2>
			{#if allOrgTags.length > 0}
				<div class="relative">
					<button
						onclick={() => (tagPickerOpen = !tagPickerOpen)}
						class="text-xs px-3 py-1.5 rounded-lg transition-colors"
						style="background: var(--accent-muted); color: var(--accent); border: 1px solid var(--accent-border);">
						Edit tags
					</button>
					{#if tagPickerOpen}
						<!-- svelte-ignore a11y_click_events_have_key_events a11y_no_static_element_interactions -->
						<div class="fixed inset-0 z-10" onclick={() => (tagPickerOpen = false)}></div>
						<div class="absolute top-full mt-1 right-0 z-20 min-w-48 rounded-xl border border-zinc-700 shadow-xl py-1"
							style="background: var(--color-zinc-900);">
							{#each allOrgTags as tag (tag.id)}
								{@const selected = stack.tags?.some((t) => t.id === tag.id)}
								<button
									onclick={() => toggleStackTag(tag.id)}
									disabled={savingTags}
									class="w-full flex items-center gap-2.5 px-3 py-2 text-sm hover:bg-zinc-800 transition-colors text-left disabled:opacity-50">
									<span class="w-3 h-3 rounded-full flex-shrink-0 {selected ? 'ring-2 ring-offset-1' : ''}"
										style="background: {tag.color}; {selected ? `ring-color: ${tag.color}; ring-offset-color: var(--color-zinc-900);` : ''}">
									</span>
									<span class="flex-1 text-zinc-200">{tag.name}</span>
									{#if selected}
										<svg class="h-3.5 w-3.5 flex-shrink-0" style="color: var(--accent);" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5">
											<path d="m4.5 12.75 6 6 9-13.5"/>
										</svg>
									{/if}
								</button>
							{/each}
							{#if allOrgTags.length === 0}
								<p class="px-3 py-2 text-xs text-zinc-500">No tags yet — create them in Settings → Tags.</p>
							{/if}
						</div>
					{/if}
				</div>
			{:else}
				<a href="/settings/tags" class="text-xs text-zinc-500 hover:text-zinc-300 transition-colors">
					Create tags in Settings →
				</a>
			{/if}
		</div>
		{#if stack.tags?.length > 0}
			<div class="flex items-center gap-2 flex-wrap">
				{#each stack.tags as tag (tag.id)}
					<span class="inline-flex items-center gap-1.5 text-sm px-2.5 py-1 rounded-full border"
						style="border-color: {tag.color}33; background: {tag.color}18; color: var(--color-zinc-200);">
						<span class="w-2 h-2 rounded-full flex-shrink-0" style="background: {tag.color};"></span>
						{tag.name}
					</span>
				{/each}
			</div>
		{:else}
			<p class="text-zinc-600 text-sm">No tags on this stack.{allOrgTags.length === 0 ? ' Create tags in Settings → Tags first.' : ''}</p>
		{/if}
	</section>
	{/if}

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
					<tbody class="divide-y divide-zinc-700">
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

	<!-- Policy Packs -->
	<section class="space-y-3">
		<h2 class="text-sm font-medium text-zinc-400 uppercase tracking-wide">Compliance Policy Packs</h2>
		{#if stackPolicyPacks.length > 0}
			<div class="border border-zinc-800 rounded-xl overflow-hidden">
				<table class="w-full text-sm">
					<thead class="bg-zinc-900 text-zinc-500 text-xs uppercase tracking-wide">
						<tr>
							<th class="text-left px-4 py-2">Pack</th>
							<th class="text-left px-4 py-2">Synced</th>
							<th class="text-left px-4 py-2">Policies</th>
							<th class="px-4 py-2"></th>
						</tr>
					</thead>
					<tbody class="divide-y divide-zinc-700">
						{#each stackPolicyPacks as pack (pack.id)}
							<tr>
								<td class="px-4 py-2.5 text-zinc-200">{pack.name}</td>
								<td class="px-4 py-2.5 text-zinc-500 text-xs">{pack.last_synced_at ? new Date(pack.last_synced_at).toLocaleDateString() : '—'}</td>
								<td class="px-4 py-2.5 text-zinc-500 text-xs">{pack.policy_count}</td>
								<td class="px-4 py-2.5 text-right">
									<button onclick={() => detachPack(pack.id)}
										class="text-xs text-zinc-500 hover:text-red-400">Remove</button>
								</td>
							</tr>
						{/each}
					</tbody>
				</table>
			</div>
		{:else}
			<p class="text-zinc-600 text-sm">No compliance packs attached.</p>
		{/if}

		{#if unattachedPacks.length > 0}
			<div class="flex items-center gap-2">
				<select class="field-input w-64" bind:value={attachingPackID}>
					<option value="">— attach a pack —</option>
					{#each unattachedPacks as pack (pack.id)}
						<option value={pack.id}>{pack.name}</option>
					{/each}
				</select>
				<button onclick={attachPack} disabled={!attachingPackID}
					class="border border-zinc-700 hover:border-zinc-500 disabled:opacity-40 text-zinc-300 text-sm px-3 py-1.5 rounded-lg transition-colors">
					Attach
				</button>
			</div>
		{:else if catalogEntries.length > 0 && catalogEntries.every(e => !e.installed)}
			<p class="text-xs text-zinc-500">
				No compliance packs installed for this org. <a href="/policies/compliance-packs" class="text-teal-400 hover:underline">Install from the catalog.</a>
			</p>
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
					<tbody class="divide-y divide-zinc-700">
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
		<p class="text-xs text-zinc-500">Values are encrypted at rest and injected into runner containers. Secret values are write-only and cannot be read back; plain values are visible here.</p>

		{#if envVars.length > 0}
			<div class="border border-zinc-800 rounded-xl overflow-hidden">
				<table class="w-full text-sm">
					<thead class="bg-zinc-900 text-zinc-500 text-xs uppercase tracking-wide">
						<tr>
							<th class="text-left px-4 py-2">Name</th>
							<th class="text-left px-4 py-2">Type</th>
							<th class="text-left px-4 py-2">Value</th>
							<th class="text-left px-4 py-2">Last updated</th>
							<th class="px-4 py-2"></th>
						</tr>
					</thead>
					<tbody class="divide-y divide-zinc-700">
						{#each envVars as ev (ev.id)}
							<tr>
								<td class="px-4 py-2.5 font-mono text-xs text-zinc-200">{ev.name}</td>
								<td class="px-4 py-2.5">
									{#if ev.is_secret}
										<span class="text-xs text-zinc-500">secret</span>
									{:else}
										<span class="text-xs text-zinc-400">plain</span>
									{/if}
								</td>
								<td class="px-4 py-2.5 font-mono text-xs">
									{#if ev.is_secret}
										<span class="text-zinc-600">••••••••</span>
									{:else}
										<span class="text-zinc-300">{ev.value ?? ''}</span>
									{/if}
								</td>
								<td class="px-4 py-2.5 text-zinc-500 text-xs">{fmtDate(ev.updated_at)}</td>
								<td class="px-4 py-2.5 text-right">
									<div class="flex items-center justify-end gap-3">
										<button onclick={() => editEnvVar(ev)}
											class="text-xs text-zinc-500 hover:text-zinc-300">{ev.is_secret ? 'Replace' : 'Edit'}</button>
										<button onclick={() => deleteEnvVar(ev.name)}
											class="text-xs text-zinc-500 hover:text-red-400">Remove</button>
									</div>
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
				<input id="env-value" class="field-input w-56" type={newEnvSecret ? 'password' : 'text'} bind:value={newEnvValue} bind:this={envValueInput} placeholder="value" autocomplete="new-password" />
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
					<tbody class="divide-y divide-zinc-700">
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

		<!-- Flow diagram -->
		{#if stack}
			<div class="bg-zinc-950 border border-zinc-800 rounded-xl p-4">
				<DepGraph
					current={{ id: stack.id, name: stack.name, slug: stack.slug }}
					upstream={upstreamDeps}
					downstream={downstreamDeps}
				/>
			</div>
		{/if}

		<!-- Management controls -->
		{#if auth.isMemberOrAbove && (upstreamDeps.length > 0 || downstreamDeps.length > 0)}
			<div class="border border-zinc-800 rounded-xl overflow-hidden">
				<table class="w-full text-sm">
					<tbody class="divide-y divide-zinc-700">
						{#each upstreamDeps as dep (dep.id)}
							<tr class="hover:bg-zinc-800/30 transition-colors">
								<td class="px-4 py-2.5 text-xs text-zinc-500 w-20">upstream</td>
								<td class="px-4 py-2.5">
									<a href="/stacks/{dep.id}" class="text-zinc-300 hover:text-white transition-colors">{dep.name}</a>
									<span class="text-zinc-600 text-xs ml-2">{dep.slug}</span>
								</td>
								<td class="px-4 py-2.5 text-right">
									<button onclick={() => removeUpstreamDep(dep.id)} class="text-xs text-zinc-500 hover:text-red-400">Remove</button>
								</td>
							</tr>
						{/each}
						{#each downstreamDeps as dep (dep.id)}
							<tr class="hover:bg-zinc-800/30 transition-colors">
								<td class="px-4 py-2.5 text-xs text-zinc-500 w-20">downstream</td>
								<td class="px-4 py-2.5">
									<a href="/stacks/{dep.id}" class="text-zinc-300 hover:text-white transition-colors">{dep.name}</a>
									<span class="text-zinc-600 text-xs ml-2">{dep.slug}</span>
								</td>
								<td class="px-4 py-2.5 text-right">
									<button onclick={() => removeDownstreamDep(dep.id)} class="text-xs text-zinc-500 hover:text-red-400">Remove</button>
								</td>
							</tr>
						{/each}
					</tbody>
				</table>
			</div>
		{/if}

		<div class="space-y-2">

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
						<option value="bitbucket">Bitbucket Cloud</option>
						<option value="azure_devops">Azure DevOps</option>
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
				{:else if notifVCSProvider === 'bitbucket'}
					<div class="space-y-1.5">
						<label class="field-label" for="notif-vcs-username">Workspace username</label>
						<input id="notif-vcs-username" class="field-input"
							bind:value={notifVCSUsername}
							placeholder="my-workspace" />
						<p class="text-xs text-zinc-600">Required for PR comments and commit status checks.</p>
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
					<label class="field-label" for="notif-discord">
						Discord webhook URL
						{#if stack.has_discord_webhook}
							<span class="ml-1 text-green-500 text-xs">● set</span>
						{/if}
					</label>
					<input id="notif-discord" class="field-input" type="password"
						bind:value={notifDiscordWebhook}
						placeholder={stack.has_discord_webhook ? 'Enter new value to replace' : 'https://discord.com/api/webhooks/…'}
						autocomplete="new-password" />
				</div>
				<div class="space-y-1.5">
					<label class="field-label" for="notif-teams">
						Microsoft Teams webhook URL
						{#if stack.has_teams_webhook}
							<span class="ml-1 text-green-500 text-xs">● set</span>
						{/if}
					</label>
					<input id="notif-teams" class="field-input" type="password"
						bind:value={notifTeamsWebhook}
						placeholder={stack.has_teams_webhook ? 'Enter new value to replace' : 'https://outlook.office.com/webhook/…'}
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
					class="bg-teal-600 hover:bg-teal-500 disabled:opacity-50 text-white text-sm px-4 py-1.5 rounded-lg transition-colors">
					{savingNotif ? 'Saving…' : 'Save notifications'}
				</button>
				{#if stack.has_slack_webhook}
					<button type="button" onclick={testSlack} disabled={testingSlack}
						class="border border-zinc-700 hover:border-zinc-500 text-zinc-300 text-sm px-3 py-1.5 rounded-lg transition-colors disabled:opacity-50">
						{testingSlack ? 'Sending…' : 'Test Slack'}
					</button>
				{/if}
				{#if stack.has_discord_webhook}
					<button type="button" onclick={testDiscord} disabled={testingDiscord}
						class="border border-zinc-700 hover:border-zinc-500 text-zinc-300 text-sm px-3 py-1.5 rounded-lg transition-colors disabled:opacity-50">
						{testingDiscord ? 'Sending…' : 'Test Discord'}
					</button>
				{/if}
				{#if stack.has_teams_webhook}
					<button type="button" onclick={testTeams} disabled={testingTeams}
						class="border border-zinc-700 hover:border-zinc-500 text-zinc-300 text-sm px-3 py-1.5 rounded-lg transition-colors disabled:opacity-50">
						{testingTeams ? 'Sending…' : 'Test Teams'}
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
				{#if discordTestResult}
					<span class="text-xs {discordTestResult.ok ? 'text-green-400' : 'text-red-400'}">
						{discordTestResult.msg}
					</span>
				{/if}
				{#if teamsTestResult}
					<span class="text-xs {teamsTestResult.ok ? 'text-green-400' : 'text-red-400'}">
						{teamsTestResult.msg}
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
		<p class="text-xs text-zinc-500">Assign org-level integrations to this stack. Manage credentials in <a href="/settings/integrations" class="text-teal-400 hover:text-teal-300">Settings → Integrations</a>.</p>
		{#if ghApp && ghApp.installations.length > 0 && !stack?.github_installation_uuid}
			<div class="rounded-lg bg-zinc-800/50 border border-zinc-700/50 px-3 py-2 text-xs text-zinc-400">
				A <strong class="text-zinc-200">GitHub App</strong> is registered for this org —
				<a href="#github-app-auth" class="text-teal-400 hover:underline">switch to App authentication ↓</a>
				to replace per-stack tokens with short-lived App tokens.
			</div>
		{/if}

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
					class="bg-teal-600 hover:bg-teal-500 disabled:opacity-50 text-white text-sm px-4 py-1.5 rounded-lg transition-colors">
					{savingIntegrations ? 'Saving…' : 'Save integrations'}
				</button>
				{#if integrationsSaved}
					<span class="text-xs text-green-400">Saved.</span>
				{/if}
			</div>
		</div>
	</section>

	<!-- GitHub App authentication -->
	<section id="github-app-auth" class="space-y-3">
		<div class="flex items-center justify-between">
			<h2 class="text-sm font-medium text-zinc-400 uppercase tracking-wide">GitHub App authentication</h2>
			{#if stack?.github_installation_uuid}
				<span class="text-xs font-medium px-2 py-0.5 rounded-full bg-teal-900/60 text-teal-300 border border-teal-800">App token active</span>
			{:else if stack?.has_vcs_token}
				<span class="text-xs font-medium px-2 py-0.5 rounded-full bg-zinc-800 text-zinc-400 border border-zinc-700">Using PAT</span>
			{/if}
		</div>
		<p class="text-xs text-zinc-500">
			Authenticate VCS calls (PR comments, commit status, repo clone) via the org's
			<a href="/settings/github-app" class="text-teal-400 hover:text-teal-300">GitHub App</a>
			instead of a per-stack PAT. When an installation is selected, Crucible mints short-lived
			tokens automatically — no per-stack webhook URL or token rotation needed.
		</p>
		<div class="border border-zinc-800 rounded-xl p-5 space-y-4">
			{#if !ghApp}
				<p class="text-xs text-zinc-400">
					No GitHub App registered for this org.
					<a href="/settings/github-app" class="text-teal-400 hover:text-teal-300">Set one up in Settings → GitHub App</a>
					to enable installation-based auth for all stacks.
				</p>
			{:else if ghApp.installations.length === 0}
				<p class="text-xs text-zinc-400">
					No installations recorded yet.
					<a href="/settings/github-app" class="text-teal-400 hover:text-teal-300">Install the App on a GitHub account</a>
					in Settings → GitHub App, then return here to select it.
				</p>
			{:else}
				<div class="space-y-1.5">
					<label class="field-label" for="gh-installation">Installation</label>
					<select id="gh-installation" class="field-input" bind:value={selectedGHInstallation}>
						<option value="">— use PAT instead —</option>
						{#each ghApp.installations as inst (inst.id)}
							<option value={inst.id}>{inst.account_login} ({inst.account_type}) — id {inst.installation_id}</option>
						{/each}
					</select>
					<p class="text-xs text-zinc-500">
						Choose the installation whose account owns this stack's repository. If the repo is not
						accessible to the selected installation, VCS calls will fail — verify repo access in
						<a href="/settings/github-app" class="text-teal-400 hover:teal-300">Settings → GitHub App</a>.
						Select <em>use PAT instead</em> to revert to per-stack token auth.
					</p>
				</div>
				<div class="flex items-center gap-3">
					<button type="button" onclick={saveGHAuth} disabled={savingGHAuth}
						class="bg-teal-600 hover:bg-teal-500 disabled:opacity-50 text-white text-sm px-4 py-1.5 rounded-lg transition-colors">
						{savingGHAuth ? 'Saving…' : 'Save'}
					</button>
					{#if ghAuthSaved}
						<span class="text-xs text-green-400">Saved.</span>
					{/if}
				</div>
			{/if}
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
						class="bg-teal-600 hover:bg-teal-500 disabled:opacity-50 text-white text-sm px-4 py-1.5 rounded-lg transition-colors">
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
					<tbody class="divide-y divide-zinc-700">
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
						<button onclick={() => { newWebhookSecret = null; sessionStorage.removeItem(`webhook_secret_${stackID}`); }} class="text-xs text-yellow-600 hover:text-yellow-400">Dismiss</button>
					</div>
				{:else}
					<p class="text-xs text-zinc-600 italic">Kept secret — shown only when first generated or rotated.</p>
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
					<tbody class="divide-y divide-zinc-700">
						{#each webhookDeliveries as d (d.id)}
							<tr class="hover:bg-zinc-900/50 transition-colors cursor-pointer" onclick={() => toggleDeliveryPayload(d.id)}>
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
										<a href="/runs/{d.run_id}" class="text-teal-400 hover:text-teal-300" onclick={(e) => e.stopPropagation()}>run →</a>
									{:else if d.skip_reason}
										<span class="text-zinc-600">{d.skip_reason.replace(/_/g, ' ')}</span>
									{:else}
										<span class="text-zinc-700">—</span>
									{/if}
								</td>
								<td class="px-4 py-2.5 text-xs text-right flex items-center justify-end gap-3">
									<span class="text-zinc-600">{expandedDeliveryID === d.id ? '▲' : '▼'}</span>
									<button
										title="Re-deliver"
										onclick={async (e) => {
											e.stopPropagation();
											try {
												const res = await stacks.webhook.redeliver(stackID, d.id);
												goto('/runs/' + res.run_id);
											} catch { /* ignore */ }
										}}
										class="text-zinc-500 hover:text-teal-400 transition-colors"
									>↺</button>
								</td>
							</tr>
							{#if expandedDeliveryID === d.id}
							<tr class="bg-zinc-950">
								<td colspan="6" class="px-4 py-3">
									{#if deliveryPayloads[d.id] === undefined}
										<span class="text-zinc-600 text-xs">Loading payload…</span>
									{:else if deliveryPayloads[d.id] === null}
										<span class="text-zinc-600 text-xs">Payload unavailable.</span>
									{:else}
										<pre class="text-xs text-zinc-300 font-mono whitespace-pre-wrap break-all max-h-64 overflow-y-auto leading-relaxed">{JSON.stringify(deliveryPayloads[d.id], null, 2)}</pre>
									{/if}
								</td>
							</tr>
							{/if}
						{/each}
					</tbody>
				</table>
			</div>
		{/if}
	</section>

	<!-- Outgoing webhooks -->
	<section class="space-y-3">
		<h2 class="text-sm font-medium text-zinc-400 uppercase tracking-wide">Outgoing webhooks</h2>
		<p class="text-xs text-zinc-500">
			Send a signed HTTP POST to an external URL on run events. Use this to integrate with PagerDuty, ServiceNow, or any custom endpoint.
		</p>

		{#if owRevealedSecret}
			<div class="bg-amber-950/40 border border-amber-800/50 rounded-xl px-4 py-3 space-y-1">
				<p class="text-xs text-amber-400">New signing secret — copy it now, it won't be shown again:</p>
				<div class="flex items-center gap-2">
					<code class="flex-1 text-xs text-amber-200 font-mono break-all">{owRevealedSecret}</code>
					<button onclick={() => navigator.clipboard.writeText(owRevealedSecret!)} class="shrink-0 text-xs text-zinc-400 hover:text-zinc-200 border border-zinc-700 px-2 py-1 rounded">Copy</button>
				</div>
				<button onclick={() => (owRevealedSecret = null)} class="text-xs text-amber-700 hover:text-amber-500">Dismiss</button>
			</div>
		{/if}

		{#if outgoingWebhooks.length > 0}
			<div class="border border-zinc-800 rounded-xl overflow-hidden">
				<table class="w-full text-sm">
					<tbody class="divide-y divide-zinc-700">
						{#each outgoingWebhooks as wh (wh.id)}
							<tr class="hover:bg-zinc-900/50 transition-colors">
								<td class="px-4 py-3 font-mono text-zinc-300 text-xs break-all max-w-xs">{wh.url}</td>
								<td class="px-4 py-3 text-xs text-zinc-500">{wh.event_types.join(', ')}</td>
								<td class="px-4 py-3">
									<span class="text-xs {wh.is_active ? 'text-green-400' : 'text-zinc-600'}">{wh.is_active ? 'active' : 'paused'}</span>
								</td>
								<td class="px-4 py-3 text-xs text-right">
									<div class="flex items-center justify-end gap-3">
										<button onclick={() => toggleOWDeliveries(wh.id)} class="text-zinc-500 hover:text-zinc-300 text-xs">
											{expandedOWID === wh.id ? 'hide log' : 'log'}
										</button>
										<button onclick={() => toggleOWActive(wh)} class="text-zinc-500 hover:text-zinc-300 text-xs">
											{wh.is_active ? 'pause' : 'resume'}
										</button>
										<button onclick={() => rotateOWSecret(wh.id)} class="text-zinc-500 hover:text-zinc-300 text-xs">rotate secret</button>
										<button onclick={() => deleteOW(wh.id)} class="text-red-500 hover:text-red-400 text-xs">delete</button>
									</div>
								</td>
							</tr>
							{#if expandedOWID === wh.id}
								<tr>
									<td colspan="4" class="bg-zinc-950 px-4 py-3">
										{#if owDeliveries[wh.id]}
											{#if owDeliveries[wh.id].length === 0}
												<p class="text-xs text-zinc-600">No deliveries yet.</p>
											{:else}
												<table class="w-full text-xs">
													<thead class="text-zinc-500">
														<tr>
															<th class="text-left py-1">Time</th>
															<th class="text-left py-1">Event</th>
															<th class="text-left py-1">Attempt</th>
															<th class="text-left py-1">Status</th>
															<th class="text-left py-1">Error</th>
														</tr>
													</thead>
													<tbody class="divide-y divide-zinc-900">
														{#each owDeliveries[wh.id] as d (d.id)}
															<tr>
																<td class="py-1 text-zinc-500 whitespace-nowrap">{fmtDate(d.delivered_at)}</td>
																<td class="py-1 text-zinc-400">{d.event_type}</td>
																<td class="py-1 text-zinc-500">{d.attempt}</td>
																<td class="py-1">
																	{#if d.status_code && d.status_code < 300}
																		<span class="text-green-400">{d.status_code}</span>
																	{:else if d.status_code}
																		<span class="text-red-400">{d.status_code}</span>
																	{:else}
																		<span class="text-zinc-600">—</span>
																	{/if}
																</td>
																<td class="py-1 text-red-400 font-mono">{d.error ?? ''}</td>
															</tr>
														{/each}
													</tbody>
												</table>
											{/if}
										{:else}
											<p class="text-xs text-zinc-600">Loading…</p>
										{/if}
									</td>
								</tr>
							{/if}
						{/each}
					</tbody>
				</table>
			</div>
		{/if}

		<!-- Add form -->
		<div class="border border-zinc-800 rounded-xl p-4 space-y-3">
			<p class="text-xs text-zinc-500 font-medium">Add outgoing webhook</p>
			<form onsubmit={addOutgoingWebhook} class="space-y-3">
				<div class="space-y-1">
					<label class="text-xs text-zinc-400" for="ow-url">URL</label>
					<input id="ow-url" type="url" bind:value={newOWURL} placeholder="https://example.com/hook" required
						class="w-full bg-zinc-800 border border-zinc-700 text-zinc-100 placeholder-zinc-500 text-sm rounded-lg px-3 py-2 font-mono focus:outline-none focus:ring-2 focus:ring-teal-500" />
				</div>
				<div class="space-y-1">
					<p class="text-xs text-zinc-400">Events</p>
					<div class="flex gap-4">
						{#each ['plan_complete', 'run_finished', 'run_failed'] as ev}
							<label class="flex items-center gap-1.5 text-xs text-zinc-400 cursor-pointer">
								<input type="checkbox" bind:group={newOWEvents} value={ev} class="accent-teal-500" />
								{ev.replace('_', ' ')}
							</label>
						{/each}
					</div>
				</div>
				<div class="space-y-1">
					<label class="text-xs text-zinc-400" for="ow-headers">Extra headers <span class="text-zinc-600">(optional, one per line: Key: Value)</span></label>
					<textarea id="ow-headers" bind:value={newOWHeaders} rows="2" placeholder="Authorization: Bearer token123"
						class="w-full bg-zinc-800 border border-zinc-700 text-zinc-100 placeholder-zinc-500 text-xs rounded-lg px-3 py-2 font-mono focus:outline-none focus:ring-2 focus:ring-teal-500 resize-none"></textarea>
				</div>
				<label class="flex items-center gap-2 text-xs text-zinc-400 cursor-pointer">
					<input type="checkbox" bind:checked={newOWWithSecret} class="accent-teal-500" />
					Generate HMAC signing secret <span class="text-zinc-600">(adds <code class="text-zinc-300">X-Crucible-Signature</code> header)</span>
				</label>
				{#if owError}
					<p class="text-xs text-red-400">{owError}</p>
				{/if}
				<button type="submit" disabled={addingOW || newOWEvents.length === 0}
					class="bg-teal-600 hover:bg-teal-500 disabled:opacity-50 text-white text-sm px-4 py-2 rounded-lg transition-colors">
					{addingOW ? 'Adding…' : 'Add webhook'}
				</button>
			</form>
		</div>
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
					<tbody class="divide-y divide-zinc-700">
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
											class="ml-1 text-xs text-teal-400 hover:text-teal-300">#{run.pr_number}</a>
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

	<!-- Cloud OIDC workload identity federation -->
	<section class="space-y-3">
		<div class="flex items-center justify-between">
			<h2 class="text-sm font-medium text-zinc-400 uppercase tracking-wide">Cloud OIDC federation</h2>
			{#if oidcConfig}
				<button onclick={deleteOIDC} class="text-xs text-red-500 hover:text-red-300 transition-colors">Remove</button>
			{/if}
		</div>
		<p class="text-xs text-zinc-600">Exchange a short-lived OIDC token for cloud credentials on every run — no static secrets stored in Crucible.</p>
		{#if !oidcConfig}
			<p class="text-xs text-teal-400/70">No per-stack config — the org-level OIDC default from Settings will be used if one is configured.</p>
		{/if}
		<form onsubmit={saveOIDC} class="border border-zinc-800 rounded-xl p-5 space-y-4">
			{#if oidcError}
				<p class="text-red-400 text-sm">{oidcError}</p>
			{/if}
			<div class="space-y-1.5">
				<label class="field-label" for="oidc-provider">Cloud provider</label>
				<select id="oidc-provider" class="field-input" bind:value={oidcProvider}>
					<optgroup label="Cloud">
						<option value="aws">AWS</option>
						<option value="gcp">Google Cloud</option>
						<option value="azure">Azure</option>
					</optgroup>
					<optgroup label="Self-hosted">
						<option value="vault">HashiCorp Vault</option>
						<option value="authentik">Authentik</option>
						<option value="generic">Generic (Keycloak, Zitadel, Dex…)</option>
					</optgroup>
				</select>
			</div>

			{#if oidcProvider === 'aws'}
				<div class="space-y-1.5">
					<label class="field-label" for="oidc-aws-role">IAM Role ARN</label>
					<input id="oidc-aws-role" class="field-input font-mono" bind:value={oidcAWSRoleARN}
						placeholder="arn:aws:iam::123456789:role/crucible-runner" />
					<p class="text-xs text-zinc-600">The role must trust the Crucible OIDC provider as a web identity federation source.</p>
				</div>
			{:else if oidcProvider === 'gcp'}
				<div class="space-y-1.5">
					<label class="field-label" for="oidc-gcp-audience">Workload identity audience</label>
					<input id="oidc-gcp-audience" class="field-input font-mono" bind:value={oidcGCPAudience}
						placeholder="//iam.googleapis.com/projects/PROJECT/locations/global/workloadIdentityPools/POOL/providers/PROVIDER" />
				</div>
				<div class="space-y-1.5">
					<label class="field-label" for="oidc-gcp-sa">Service account email</label>
					<input id="oidc-gcp-sa" class="field-input font-mono" bind:value={oidcGCPSA}
						placeholder="runner@my-project.iam.gserviceaccount.com" />
				</div>
			{:else if oidcProvider === 'azure'}
				<div class="grid grid-cols-2 gap-4">
					<div class="space-y-1.5">
						<label class="field-label" for="oidc-az-tenant">Tenant ID</label>
						<input id="oidc-az-tenant" class="field-input font-mono" bind:value={oidcAzureTenant} placeholder="xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx" />
					</div>
					<div class="space-y-1.5">
						<label class="field-label" for="oidc-az-client">Client (App) ID</label>
						<input id="oidc-az-client" class="field-input font-mono" bind:value={oidcAzureClient} placeholder="xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx" />
					</div>
					<div class="space-y-1.5">
						<label class="field-label" for="oidc-az-sub">Subscription ID</label>
						<input id="oidc-az-sub" class="field-input font-mono" bind:value={oidcAzureSubscription} placeholder="xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx" />
					</div>
				</div>
			{:else if oidcProvider === 'vault'}
				<div class="space-y-4">
					<div class="space-y-1.5">
						<label class="field-label" for="oidc-vault-addr">Vault address</label>
						<input id="oidc-vault-addr" class="field-input font-mono" bind:value={oidcVaultAddr}
							placeholder="https://vault.example.com" />
					</div>
					<div class="grid grid-cols-2 gap-4">
						<div class="space-y-1.5">
							<label class="field-label" for="oidc-vault-role">JWT auth role</label>
							<input id="oidc-vault-role" class="field-input font-mono" bind:value={oidcVaultRole}
								placeholder="crucible-runner" />
						</div>
						<div class="space-y-1.5">
							<label class="field-label" for="oidc-vault-mount">JWT auth mount <span class="font-normal text-zinc-500">(optional, default: jwt)</span></label>
							<input id="oidc-vault-mount" class="field-input font-mono" bind:value={oidcVaultMount}
								placeholder="jwt" />
						</div>
					</div>
					<p class="text-xs text-zinc-600">The runner receives <code class="text-zinc-400">VAULT_ADDR</code>, <code class="text-zinc-400">CRUCIBLE_OIDC_VAULT_ROLE</code>, and <code class="text-zinc-400">CRUCIBLE_OIDC_TOKEN_FILE=/tmp/oidc-token</code>. Your entrypoint script exchanges the token via <code class="text-zinc-400">vault write auth/&lt;mount&gt;/login</code>.</p>
				</div>
			{:else if oidcProvider === 'authentik'}
				<div class="space-y-4">
					<div class="space-y-1.5">
						<label class="field-label" for="oidc-authentik-url">Authentik URL</label>
						<input id="oidc-authentik-url" class="field-input font-mono" bind:value={oidcAuthentikURL}
							placeholder="https://auth.example.com" />
					</div>
					<div class="space-y-1.5">
						<label class="field-label" for="oidc-authentik-cid">JWT source client ID</label>
						<input id="oidc-authentik-cid" class="field-input font-mono" bind:value={oidcAuthentikClientID}
							placeholder="crucible" />
					</div>
					<p class="text-xs text-zinc-600">The runner receives <code class="text-zinc-400">AUTHENTIK_URL</code>, <code class="text-zinc-400">CRUCIBLE_OIDC_AUTHENTIK_CLIENT_ID</code>, and <code class="text-zinc-400">CRUCIBLE_OIDC_TOKEN_FILE=/tmp/oidc-token</code>. Configure an Authentik JWT source to trust the Crucible issuer.</p>
				</div>
			{:else if oidcProvider === 'generic'}
				<div class="space-y-4">
					<div class="space-y-1.5">
						<label class="field-label" for="oidc-generic-url">Token exchange endpoint</label>
						<input id="oidc-generic-url" class="field-input font-mono" bind:value={oidcGenericTokenURL}
							placeholder="https://keycloak.example.com/realms/myrealm/protocol/openid-connect/token" />
					</div>
					<div class="grid grid-cols-2 gap-4">
						<div class="space-y-1.5">
							<label class="field-label" for="oidc-generic-cid">Client ID <span class="font-normal text-zinc-500">(optional)</span></label>
							<input id="oidc-generic-cid" class="field-input font-mono" bind:value={oidcGenericClientID}
								placeholder="crucible-runner" />
						</div>
						<div class="space-y-1.5">
							<label class="field-label" for="oidc-generic-scope">Scope <span class="font-normal text-zinc-500">(optional)</span></label>
							<input id="oidc-generic-scope" class="field-input font-mono" bind:value={oidcGenericScope}
								placeholder="openid" />
						</div>
					</div>
					<p class="text-xs text-zinc-600">The runner receives <code class="text-zinc-400">CRUCIBLE_OIDC_TOKEN_URL</code>, <code class="text-zinc-400">CRUCIBLE_OIDC_CLIENT_ID</code>, and <code class="text-zinc-400">CRUCIBLE_OIDC_TOKEN_FILE=/tmp/oidc-token</code>. Your entrypoint script performs the token exchange using these env vars.</p>
				</div>
			{/if}

			<details class="text-xs">
				<summary class="text-zinc-500 cursor-pointer select-none hover:text-zinc-300">Advanced</summary>
				<div class="mt-3 space-y-1.5">
					<label class="field-label" for="oidc-audience-override">Audience override</label>
					<input id="oidc-audience-override" class="field-input font-mono" bind:value={oidcAudienceOverride}
						placeholder="Leave empty to use cloud-provider default" />
				</div>
			</details>

			<div class="flex items-center gap-3">
				<button type="submit" disabled={savingOIDC}
					class="border border-zinc-700 hover:border-zinc-500 text-zinc-300 text-sm px-3 py-1.5 rounded-lg transition-colors disabled:opacity-50">
					{savingOIDC ? 'Saving…' : oidcSaved ? 'Saved' : oidcConfig ? 'Update' : 'Enable'}
				</button>
				{#if oidcConfig}
					{@const providerLabel = { aws: 'AWS', gcp: 'Google Cloud', azure: 'Azure', vault: 'Vault', authentik: 'Authentik', generic: 'Generic OIDC' }}
					<span class="text-xs text-emerald-500">Enabled · {providerLabel[oidcConfig.provider] ?? oidcConfig.provider}</span>
				{/if}
			</div>
		</form>
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
					<tbody class="divide-y divide-zinc-700">
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

<!-- Clone modal -->
{#if showCloneModal && stack}
<div class="fixed inset-0 z-50 flex items-center justify-center bg-black/70 px-4">
	<div class="bg-zinc-900 border border-zinc-700 rounded-2xl p-6 w-full max-w-md space-y-4 shadow-2xl">
		<div class="space-y-1">
			<h2 class="text-white font-semibold text-base">Clone stack</h2>
			<p class="text-zinc-400 text-sm">
				Copies tool config, repo settings, hooks, worker pool, env vars, and tags from
				<span class="text-white font-medium">{stack.name}</span>. State, runs, and notification secrets are not copied.
			</p>
		</div>
		<form onsubmit={cloneStack} class="space-y-4">
			<div class="space-y-1.5">
				<label class="text-xs text-zinc-400" for="clone-name">New stack name</label>
				<input
					id="clone-name"
					class="field-input"
					bind:value={cloneName}
					placeholder="Copy of {stack.name}"
					required
					autocomplete="off"
				/>
			</div>
			{#if cloneError}
				<p class="text-xs text-red-400">{cloneError}</p>
			{/if}
			<div class="flex gap-3 pt-1">
				<button
					type="submit"
					disabled={cloning || !cloneName.trim()}
					class="flex-1 bg-teal-600 hover:bg-teal-500 disabled:opacity-40 disabled:cursor-not-allowed text-white text-sm px-4 py-2 rounded-lg transition-colors font-medium">
					{cloning ? 'Cloning…' : 'Clone stack'}
				</button>
				<button
					type="button"
					onclick={() => { showCloneModal = false; cloneError = null; }}
					class="border border-zinc-700 hover:border-zinc-500 text-zinc-300 text-sm px-4 py-2 rounded-lg transition-colors">
					Cancel
				</button>
			</div>
		</form>
	</div>
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
