<script lang="ts">
	import { onMount } from 'svelte';
	import { auth } from '$lib/stores/auth.svelte';
	import { system, type HealthStatus, type SystemSettings } from '$lib/api/client';

	let loading = $state(true);
	let health = $state<HealthStatus | null>(null);

	// Runner settings
	let runnerSettings = $state<SystemSettings | null>(null);
	let runnerForm = $state({ runner_default_image: '', runner_max_concurrent: 5, runner_job_timeout_mins: 60, runner_memory_limit: '', runner_cpu_limit: '' });
	let savingRunner = $state(false);
	let runnerSaved = $state(false);
	let runnerError = $state<string | null>(null);

	// Retention settings
	let retentionForm = $state({ artifact_retention_days: 0 });
	let savingRetention = $state(false);
	let retentionSaved = $state(false);
	let retentionError = $state<string | null>(null);

	// Org OIDC default
	let oidcForm = $state({
		oidc_provider: '',
		oidc_aws_role_arn: '', oidc_aws_session_duration_secs: 3600,
		oidc_gcp_audience: '', oidc_gcp_service_account_email: '',
		oidc_azure_tenant_id: '', oidc_azure_client_id: '', oidc_azure_subscription_id: '',
		oidc_vault_addr: '', oidc_vault_role: '', oidc_vault_mount: '',
		oidc_authentik_url: '', oidc_authentik_client_id: '',
		oidc_generic_token_url: '', oidc_generic_client_id: '', oidc_generic_scope: '',
		oidc_audience_override: ''
	});
	let savingOIDC = $state(false);
	let oidcSaved = $state(false);
	let oidcError = $state<string | null>(null);

	// IaC security scan settings
	let scanForm = $state<{ scan_tool: 'none' | 'checkov' | 'trivy'; scan_severity_threshold: 'CRITICAL' | 'HIGH' | 'MEDIUM' | 'LOW' }>({ scan_tool: 'none', scan_severity_threshold: 'HIGH' });
	let savingScan = $state(false);
	let scanSaved = $state(false);
	let scanError = $state<string | null>(null);

	// Infracost settings
	let infracostForm = $state({ infracost_api_key: '', infracost_pricing_api_endpoint: '' });
	let savingInfracost = $state(false);
	let infracostSaved = $state(false);
	let infracostError = $state<string | null>(null);

	// AI settings
	let aiForm = $state({ ai_provider: 'anthropic' as 'anthropic' | 'openai', ai_model: '', ai_base_url: '' });
	let aiKeyInput = $state('');
	let aiKeySet = $state(false);
	let savingAI = $state(false);
	let aiSaved = $state(false);
	let aiError = $state<string | null>(null);

	onMount(async () => {
		system.health().then((h) => (health = h)).catch(() => {});
		system.settings.get().then((s) => {
			runnerSettings = s;
			runnerForm = {
				runner_default_image: s.runner_default_image,
				runner_max_concurrent: s.runner_max_concurrent,
				runner_job_timeout_mins: s.runner_job_timeout_mins,
				runner_memory_limit: s.runner_memory_limit,
				runner_cpu_limit: s.runner_cpu_limit
			};
			retentionForm = { artifact_retention_days: s.artifact_retention_days ?? 0 };
			oidcForm = {
				oidc_provider: s.oidc_provider ?? '',
				oidc_aws_role_arn: s.oidc_aws_role_arn ?? '',
				oidc_aws_session_duration_secs: s.oidc_aws_session_duration_secs ?? 3600,
				oidc_gcp_audience: s.oidc_gcp_audience ?? '',
				oidc_gcp_service_account_email: s.oidc_gcp_service_account_email ?? '',
				oidc_azure_tenant_id: s.oidc_azure_tenant_id ?? '',
				oidc_azure_client_id: s.oidc_azure_client_id ?? '',
				oidc_azure_subscription_id: s.oidc_azure_subscription_id ?? '',
				oidc_vault_addr: s.oidc_vault_addr ?? '',
				oidc_vault_role: s.oidc_vault_role ?? '',
				oidc_vault_mount: s.oidc_vault_mount ?? '',
				oidc_authentik_url: s.oidc_authentik_url ?? '',
				oidc_authentik_client_id: s.oidc_authentik_client_id ?? '',
				oidc_generic_token_url: s.oidc_generic_token_url ?? '',
				oidc_generic_client_id: s.oidc_generic_client_id ?? '',
				oidc_generic_scope: s.oidc_generic_scope ?? '',
				oidc_audience_override: s.oidc_audience_override ?? ''
			};
			infracostForm.infracost_pricing_api_endpoint = s.infracost_pricing_api_endpoint ?? '';
			aiForm.ai_provider = (s.ai_provider ?? 'anthropic') as 'anthropic' | 'openai';
			aiForm.ai_model = s.ai_model ?? '';
			aiForm.ai_base_url = s.ai_base_url ?? '';
			aiKeySet = s.ai_api_key_set ?? false;
			scanForm = {
				scan_tool: (s.scan_tool ?? 'none') as 'none' | 'checkov' | 'trivy',
				scan_severity_threshold: (s.scan_severity_threshold ?? 'HIGH') as 'CRITICAL' | 'HIGH' | 'MEDIUM' | 'LOW'
			};
		}).catch((e) => console.error('settings.get', e));
		loading = false;
	});

	async function saveRunnerSettings(e: SubmitEvent) {
		e.preventDefault();
		savingRunner = true;
		runnerSaved = false;
		runnerError = null;
		try {
			runnerSettings = await system.settings.update(runnerForm);
			runnerSaved = true;
			setTimeout(() => (runnerSaved = false), 3000);
		} catch (err) {
			runnerError = (err as Error).message;
		} finally {
			savingRunner = false;
		}
	}

	async function saveOIDC(e: SubmitEvent) {
		e.preventDefault();
		savingOIDC = true;
		oidcSaved = false;
		oidcError = null;
		try {
			await system.settings.update(oidcForm);
			oidcSaved = true;
			setTimeout(() => (oidcSaved = false), 3000);
		} catch (err) {
			oidcError = (err as Error).message;
		} finally {
			savingOIDC = false;
		}
	}

	async function saveScan(e: SubmitEvent) {
		e.preventDefault();
		savingScan = true;
		scanSaved = false;
		scanError = null;
		try {
			await system.settings.update(scanForm);
			scanSaved = true;
			setTimeout(() => (scanSaved = false), 3000);
		} catch (err) {
			scanError = (err as Error).message;
		} finally {
			savingScan = false;
		}
	}

	async function saveInfracost(e: SubmitEvent) {
		e.preventDefault();
		savingInfracost = true;
		infracostSaved = false;
		infracostError = null;
		try {
			const payload: Record<string, string> = {};
			if (infracostForm.infracost_api_key) payload.infracost_api_key = infracostForm.infracost_api_key;
			if (infracostForm.infracost_pricing_api_endpoint !== undefined) payload.infracost_pricing_api_endpoint = infracostForm.infracost_pricing_api_endpoint;
			await system.settings.update(payload);
			infracostForm.infracost_api_key = '';
			infracostSaved = true;
			setTimeout(() => (infracostSaved = false), 3000);
		} catch (err) {
			infracostError = (err as Error).message;
		} finally {
			savingInfracost = false;
		}
	}

	async function saveAI(e: SubmitEvent) {
		e.preventDefault();
		savingAI = true;
		aiSaved = false;
		aiError = null;
		try {
			const payload: Parameters<typeof system.settings.update>[0] = {
				ai_provider: aiForm.ai_provider,
				ai_model: aiForm.ai_model || undefined,
				ai_base_url: aiForm.ai_base_url || undefined,
			};
			if (aiKeyInput) payload.ai_api_key = aiKeyInput;
			const res = await system.settings.update(payload);
			aiKeySet = res.ai_api_key_set ?? aiKeySet;
			aiKeyInput = '';
			aiSaved = true;
			setTimeout(() => (aiSaved = false), 3000);
		} catch (err) {
			aiError = (err as Error).message;
		} finally {
			savingAI = false;
		}
	}

	async function saveRetention(e: SubmitEvent) {
		e.preventDefault();
		savingRetention = true;
		retentionSaved = false;
		retentionError = null;
		try {
			await system.settings.update(retentionForm);
			retentionSaved = true;
			setTimeout(() => (retentionSaved = false), 3000);
		} catch (err) {
			retentionError = (err as Error).message;
		} finally {
			savingRetention = false;
		}
	}
</script>

<div class="max-w-2xl space-y-8">
	<h1 class="text-xl font-semibold text-white">General</h1>

	<!-- Update banner -->
	{#if health?.update_available}
		<div class="bg-yellow-950 border border-yellow-700 rounded-xl px-5 py-4 flex items-center justify-between gap-4">
			<div>
				<p class="text-yellow-300 text-sm font-medium">Update available</p>
				<p class="text-yellow-500 text-xs mt-0.5">
					Running <span class="font-mono">{health.version}</span> —
					<span class="font-mono">{health.latest_version}</span> is available.
				</p>
			</div>
			<a href="https://github.com/ponack/crucible-iap/releases/latest"
				target="_blank" rel="noopener"
				class="shrink-0 text-xs bg-yellow-700 hover:bg-yellow-600 text-yellow-100 px-3 py-1.5 rounded-lg transition-colors">
				View release
			</a>
		</div>
	{/if}

	<!-- Account -->
	<div class="bg-zinc-900 border border-zinc-800 rounded-xl divide-y divide-zinc-700">
		<div class="px-6 py-4">
			<p class="text-xs text-zinc-500 uppercase tracking-widest mb-3">Account</p>
			<div class="space-y-1">
				<p class="text-sm text-zinc-100">{auth.user?.name || auth.user?.email}</p>
				<p class="text-xs text-zinc-500">{auth.user?.email}</p>
			</div>
		</div>
	</div>

	<!-- Runner settings -->
	{#if runnerSettings}
		<div class="bg-zinc-900 border border-zinc-800 rounded-xl">
			<div class="px-6 py-4 border-b border-zinc-800">
				<p class="text-xs text-zinc-500 uppercase tracking-widest">Runner</p>
				<p class="text-xs text-zinc-600 mt-1">Changes apply to new runs. Max concurrency takes effect after restart.</p>
			</div>
			<form onsubmit={saveRunnerSettings} class="px-6 py-5 space-y-4">
				{#if runnerError}
					<div class="bg-red-950 border border-red-800 rounded-lg px-4 py-3 text-red-300 text-sm">{runnerError}</div>
				{/if}
				<div class="space-y-1.5">
					<label class="field-label" for="runner-image">Default runner image</label>
					<input id="runner-image" class="field-input font-mono text-sm"
						bind:value={runnerForm.runner_default_image}
						placeholder="ghcr.io/ponack/crucible-iap-runner:latest" />
				</div>
				<div class="grid grid-cols-2 gap-4">
					<div class="space-y-1.5">
						<label class="field-label" for="runner-concurrency">Max concurrent runs</label>
						<input id="runner-concurrency" type="number" min="1" max="50" class="field-input"
							bind:value={runnerForm.runner_max_concurrent} />
					</div>
					<div class="space-y-1.5">
						<label class="field-label" for="runner-timeout">Job timeout (minutes)</label>
						<input id="runner-timeout" type="number" min="1" max="480" class="field-input"
							bind:value={runnerForm.runner_job_timeout_mins} />
					</div>
				</div>
				<div class="grid grid-cols-2 gap-4">
					<div class="space-y-1.5">
						<label class="field-label" for="runner-memory">Memory limit</label>
						<input id="runner-memory" class="field-input font-mono text-sm"
							bind:value={runnerForm.runner_memory_limit} placeholder="2g" />
					</div>
					<div class="space-y-1.5">
						<label class="field-label" for="runner-cpu">CPU limit</label>
						<input id="runner-cpu" class="field-input font-mono text-sm"
							bind:value={runnerForm.runner_cpu_limit} placeholder="1.0" />
					</div>
				</div>
				<div class="flex items-center gap-3">
					<button type="submit" disabled={savingRunner}
						class="bg-teal-600 hover:bg-teal-500 disabled:opacity-50 text-white text-sm px-4 py-1.5 rounded-lg transition-colors">
						{savingRunner ? 'Saving…' : 'Save runner settings'}
					</button>
					{#if runnerSaved}
						<span class="text-xs text-green-400">Saved.</span>
					{/if}
				</div>
			</form>
		</div>

		<!-- Retention -->
		<div class="bg-zinc-900 border border-zinc-800 rounded-xl">
			<div class="px-6 py-4 border-b border-zinc-800">
				<p class="text-xs text-zinc-500 uppercase tracking-widest">Retention</p>
				<p class="text-xs text-zinc-600 mt-1">Plan artifacts and run logs are automatically deleted after the configured period. Set to 0 to retain indefinitely.</p>
			</div>
			<form onsubmit={saveRetention} class="px-6 py-5 space-y-4">
				{#if retentionError}
					<div class="bg-red-950 border border-red-800 rounded-lg px-4 py-3 text-red-300 text-sm">{retentionError}</div>
				{/if}
				<div class="space-y-1.5">
					<label class="field-label" for="retention-days">Artifact retention (days)</label>
					<div class="flex items-center gap-3">
						<input id="retention-days" type="number" min="0" max="3650" class="field-input w-32"
							bind:value={retentionForm.artifact_retention_days} />
						<span class="text-xs text-zinc-500">
							{retentionForm.artifact_retention_days === 0 ? 'Retain indefinitely' : `Delete after ${retentionForm.artifact_retention_days} day${retentionForm.artifact_retention_days === 1 ? '' : 's'}`}
						</span>
					</div>
					<p class="text-xs text-zinc-600">Applies to plan binary files and terminal logs. Terraform state is never automatically deleted.</p>
				</div>
				<div class="flex items-center gap-3">
					<button type="submit" disabled={savingRetention}
						class="bg-teal-600 hover:bg-teal-500 disabled:opacity-50 text-white text-sm px-4 py-1.5 rounded-lg transition-colors">
						{savingRetention ? 'Saving…' : 'Save retention policy'}
					</button>
					{#if retentionSaved}
						<span class="text-xs text-green-400">Saved.</span>
					{/if}
				</div>
			</form>
		</div>
	{/if}

	<!-- Org OIDC default -->
	{#if runnerSettings}
	<div class="bg-zinc-900 border border-zinc-800 rounded-xl">
		<div class="px-6 py-4 border-b border-zinc-800">
			<p class="text-xs text-zinc-500 uppercase tracking-widest">Cloud OIDC default</p>
			<p class="text-xs text-zinc-600 mt-1">Applied to any stack that has no per-stack OIDC federation configured. Stacks with their own OIDC config are unaffected.</p>
		</div>
		<form onsubmit={saveOIDC} class="px-6 py-5 space-y-4">
			{#if oidcError}
				<div class="bg-red-950 border border-red-800 rounded-lg px-4 py-3 text-red-300 text-sm">{oidcError}</div>
			{/if}
			<div class="space-y-1.5">
				<label class="field-label" for="oidc-provider">Cloud provider</label>
				<select id="oidc-provider" class="field-input" bind:value={oidcForm.oidc_provider}>
					<option value="">Disabled (no org default)</option>
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

			{#if oidcForm.oidc_provider === 'aws'}
				<div class="space-y-4">
					<div class="space-y-1.5">
						<label class="field-label" for="oidc-aws-role">IAM Role ARN</label>
						<input id="oidc-aws-role" class="field-input font-mono text-sm"
							bind:value={oidcForm.oidc_aws_role_arn}
							placeholder="arn:aws:iam::123456789012:role/crucible-runner" />
						<p class="text-xs text-zinc-600">Trust policy should use <code class="text-zinc-400">sts:AssumeRoleWithWebIdentity</code> and can condition on the <code class="text-zinc-400">sub</code> claim (<code class="text-zinc-400">stack:my-slug</code>) to scope per stack.</p>
					</div>
					<div class="space-y-1.5">
						<label class="field-label" for="oidc-aws-duration">Session duration (seconds)</label>
						<input id="oidc-aws-duration" type="number" min="900" max="43200" class="field-input w-36"
							bind:value={oidcForm.oidc_aws_session_duration_secs} placeholder="3600" />
					</div>
				</div>
			{:else if oidcForm.oidc_provider === 'gcp'}
				<div class="space-y-4">
					<div class="space-y-1.5">
						<label class="field-label" for="oidc-gcp-audience">Workload identity audience</label>
						<input id="oidc-gcp-audience" class="field-input font-mono text-sm"
							bind:value={oidcForm.oidc_gcp_audience}
							placeholder="//iam.googleapis.com/projects/PROJECT_NUMBER/locations/global/workloadIdentityPools/POOL/providers/PROVIDER" />
					</div>
					<div class="space-y-1.5">
						<label class="field-label" for="oidc-gcp-sa">Service account email</label>
						<input id="oidc-gcp-sa" class="field-input font-mono text-sm"
							bind:value={oidcForm.oidc_gcp_service_account_email}
							placeholder="crucible@my-project.iam.gserviceaccount.com" />
					</div>
				</div>
			{:else if oidcForm.oidc_provider === 'azure'}
				<div class="space-y-4">
					<div class="grid grid-cols-2 gap-4">
						<div class="space-y-1.5">
							<label class="field-label" for="oidc-azure-tenant">Tenant ID</label>
							<input id="oidc-azure-tenant" class="field-input font-mono text-sm"
								bind:value={oidcForm.oidc_azure_tenant_id} placeholder="xxxxxxxx-xxxx-…" />
						</div>
						<div class="space-y-1.5">
							<label class="field-label" for="oidc-azure-client">Client ID (app registration)</label>
							<input id="oidc-azure-client" class="field-input font-mono text-sm"
								bind:value={oidcForm.oidc_azure_client_id} placeholder="xxxxxxxx-xxxx-…" />
						</div>
					</div>
					<div class="space-y-1.5">
						<label class="field-label" for="oidc-azure-sub">Subscription ID</label>
						<input id="oidc-azure-sub" class="field-input font-mono text-sm"
							bind:value={oidcForm.oidc_azure_subscription_id} placeholder="xxxxxxxx-xxxx-…" />
					</div>
				</div>
			{:else if oidcForm.oidc_provider === 'vault'}
				<div class="space-y-4">
					<div class="space-y-1.5">
						<label class="field-label" for="oidc-vault-addr">Vault address</label>
						<input id="oidc-vault-addr" class="field-input font-mono text-sm"
							bind:value={oidcForm.oidc_vault_addr} placeholder="https://vault.example.com" />
					</div>
					<div class="grid grid-cols-2 gap-4">
						<div class="space-y-1.5">
							<label class="field-label" for="oidc-vault-role">JWT auth role</label>
							<input id="oidc-vault-role" class="field-input font-mono text-sm"
								bind:value={oidcForm.oidc_vault_role} placeholder="crucible-runner" />
						</div>
						<div class="space-y-1.5">
							<label class="field-label" for="oidc-vault-mount">JWT auth mount <span class="font-normal text-zinc-500">(optional, default: jwt)</span></label>
							<input id="oidc-vault-mount" class="field-input font-mono text-sm"
								bind:value={oidcForm.oidc_vault_mount} placeholder="jwt" />
						</div>
					</div>
				</div>
			{:else if oidcForm.oidc_provider === 'authentik'}
				<div class="space-y-4">
					<div class="space-y-1.5">
						<label class="field-label" for="oidc-authentik-url">Authentik URL</label>
						<input id="oidc-authentik-url" class="field-input font-mono text-sm"
							bind:value={oidcForm.oidc_authentik_url} placeholder="https://auth.example.com" />
					</div>
					<div class="space-y-1.5">
						<label class="field-label" for="oidc-authentik-cid">JWT source client ID</label>
						<input id="oidc-authentik-cid" class="field-input font-mono text-sm"
							bind:value={oidcForm.oidc_authentik_client_id} placeholder="crucible" />
					</div>
				</div>
			{:else if oidcForm.oidc_provider === 'generic'}
				<div class="space-y-4">
					<div class="space-y-1.5">
						<label class="field-label" for="oidc-generic-url">Token exchange endpoint</label>
						<input id="oidc-generic-url" class="field-input font-mono text-sm"
							bind:value={oidcForm.oidc_generic_token_url}
							placeholder="https://keycloak.example.com/realms/myrealm/protocol/openid-connect/token" />
					</div>
					<div class="grid grid-cols-2 gap-4">
						<div class="space-y-1.5">
							<label class="field-label" for="oidc-generic-cid">Client ID <span class="font-normal text-zinc-500">(optional)</span></label>
							<input id="oidc-generic-cid" class="field-input font-mono text-sm"
								bind:value={oidcForm.oidc_generic_client_id} placeholder="crucible-runner" />
						</div>
						<div class="space-y-1.5">
							<label class="field-label" for="oidc-generic-scope">Scope <span class="font-normal text-zinc-500">(optional)</span></label>
							<input id="oidc-generic-scope" class="field-input font-mono text-sm"
								bind:value={oidcForm.oidc_generic_scope} placeholder="openid" />
						</div>
					</div>
				</div>
			{/if}

			{#if oidcForm.oidc_provider}
				<div class="space-y-1.5">
					<label class="field-label" for="oidc-audience-override">Audience override <span class="font-normal text-zinc-500">(optional)</span></label>
					<input id="oidc-audience-override" class="field-input font-mono text-sm"
						bind:value={oidcForm.oidc_audience_override}
						placeholder="Leave blank to use provider default" />
				</div>
			{/if}

			<div class="flex items-center gap-3">
				<button type="submit" disabled={savingOIDC}
					class="bg-teal-600 hover:bg-teal-500 disabled:opacity-50 text-white text-sm px-4 py-1.5 rounded-lg transition-colors">
					{savingOIDC ? 'Saving…' : 'Save OIDC default'}
				</button>
				{#if oidcSaved}
					<span class="text-xs text-green-400">Saved.</span>
				{/if}
			</div>
		</form>
	</div>
	{/if}

	<!-- IaC security scanning -->
	{#if auth.isAdmin}
		<div class="bg-zinc-900 border border-zinc-800 rounded-xl divide-y divide-zinc-700">
			<div class="px-6 py-4">
				<p class="text-xs text-zinc-500 uppercase tracking-widest mb-1">IaC security scanning</p>
				<p class="text-xs text-zinc-600">Run Checkov or Trivy post-plan. Findings surfaced in the run detail view. Set a severity threshold to block apply on critical issues.</p>
			</div>
			<form class="px-6 py-5 space-y-4" onsubmit={saveScan}>
				<div class="grid grid-cols-2 gap-4">
					<div class="space-y-1.5">
						<label class="field-label" for="scan-tool">Scan tool</label>
						<select id="scan-tool" class="field-input" bind:value={scanForm.scan_tool}>
							<option value="none">Disabled</option>
							<option value="checkov">Checkov</option>
							<option value="trivy">Trivy</option>
						</select>
					</div>
					<div class="space-y-1.5">
						<label class="field-label" for="scan-threshold">Block on severity</label>
						<select id="scan-threshold" class="field-input" bind:value={scanForm.scan_severity_threshold}
							disabled={scanForm.scan_tool === 'none'}>
							<option value="CRITICAL">CRITICAL only</option>
							<option value="HIGH">HIGH and above</option>
							<option value="MEDIUM">MEDIUM and above</option>
							<option value="LOW">LOW and above</option>
						</select>
					</div>
				</div>
				{#if scanError}
					<p class="text-xs text-red-400">{scanError}</p>
				{/if}
				<div class="flex items-center gap-3">
					<button type="submit" disabled={savingScan}
						class="bg-teal-600 hover:bg-teal-500 disabled:opacity-50 text-white text-sm px-4 py-1.5 rounded-lg transition-colors">
						{savingScan ? 'Saving…' : 'Save scan settings'}
					</button>
					{#if scanSaved}
						<span class="text-xs text-green-400">Saved.</span>
					{/if}
				</div>
			</form>
		</div>
	{/if}

	<!-- Infracost -->
	{#if auth.isAdmin}
		<div class="bg-zinc-900 border border-zinc-800 rounded-xl divide-y divide-zinc-700">
			<div class="px-6 py-4">
				<p class="text-xs text-zinc-500 uppercase tracking-widest mb-1">Infracost</p>
				<p class="text-xs text-zinc-600">Cost estimation via <span class="font-mono">infracost breakdown</span> run post-plan. Set an API key to enable.</p>
			</div>
			<form class="px-6 py-5 space-y-4" onsubmit={saveInfracost}>
				<div class="space-y-1.5">
					<label class="field-label" for="infracost-api-key">
						API key <span class="font-normal text-zinc-500">(write-only — leave blank to keep current)</span>
					</label>
					<input id="infracost-api-key" type="password" class="field-input font-mono text-sm"
						bind:value={infracostForm.infracost_api_key}
						placeholder="ico-••••••••••••••••••••••••••••••••" autocomplete="off" />
				</div>
				<div class="space-y-1.5">
					<label class="field-label" for="infracost-endpoint">
						Pricing API endpoint <span class="font-normal text-zinc-500">(optional — for self-hosted)</span>
					</label>
					<input id="infracost-endpoint" class="field-input font-mono text-sm"
						bind:value={infracostForm.infracost_pricing_api_endpoint}
						placeholder="https://pricing.api.infracost.io" />
				</div>
				{#if infracostError}
					<p class="text-xs text-red-400">{infracostError}</p>
				{/if}
				<div class="flex items-center gap-3">
					<button type="submit" disabled={savingInfracost}
						class="bg-teal-600 hover:bg-teal-500 disabled:opacity-50 text-white text-sm px-4 py-1.5 rounded-lg transition-colors">
						{savingInfracost ? 'Saving…' : 'Save Infracost settings'}
					</button>
					{#if infracostSaved}
						<span class="text-xs text-green-400">Saved.</span>
					{/if}
				</div>
			</form>
		</div>
	{/if}

	<!-- AI -->
	{#if auth.isAdmin}
		<div class="bg-zinc-900 border border-zinc-800 rounded-xl divide-y divide-zinc-700">
			<div class="px-6 py-4 flex items-center justify-between">
				<div>
					<p class="text-xs text-zinc-500 uppercase tracking-widest mb-1">AI troubleshooting</p>
					<p class="text-xs text-zinc-600">Enables the "Explain failure" button on failed runs. Supports Anthropic, OpenAI, OpenRouter, OpenWebUI, and any OpenAI-compatible provider.</p>
				</div>
				{#if aiKeySet}
					<span class="text-xs px-2 py-0.5 rounded-full bg-teal-900/50 text-teal-400 border border-teal-800">Configured</span>
				{:else}
					<span class="text-xs px-2 py-0.5 rounded-full bg-zinc-800 text-zinc-500 border border-zinc-700">Not configured</span>
				{/if}
			</div>
			<form class="px-6 py-5 space-y-4" onsubmit={saveAI}>
				<div class="grid grid-cols-2 gap-4">
					<div class="space-y-1.5">
						<label class="field-label" for="ai-provider">Provider</label>
						<select id="ai-provider" class="field-input" bind:value={aiForm.ai_provider}>
							<option value="anthropic">Anthropic (Claude)</option>
							<option value="openai">OpenAI-compatible</option>
						</select>
					</div>
					<div class="space-y-1.5">
						<label class="field-label" for="ai-model">
							Model <span class="font-normal text-zinc-500">(optional — uses provider default if blank)</span>
						</label>
						<input id="ai-model" class="field-input font-mono text-sm" bind:value={aiForm.ai_model}
							placeholder={aiForm.ai_provider === 'anthropic' ? 'claude-haiku-4-5-20251001' : 'gpt-4o-mini'} />
					</div>
				</div>
				{#if aiForm.ai_provider === 'openai'}
					<div class="space-y-1.5">
						<label class="field-label" for="ai-base-url">
							Base URL <span class="font-normal text-zinc-500">(optional — leave blank for OpenAI; set for OpenRouter, OpenWebUI, Ollama, etc.)</span>
						</label>
						<input id="ai-base-url" class="field-input font-mono text-sm" bind:value={aiForm.ai_base_url}
							placeholder="https://openrouter.ai/api/v1" />
					</div>
				{/if}
				<div class="space-y-1.5">
					<label class="field-label" for="ai-api-key">
						API key <span class="font-normal text-zinc-500">(write-only — leave blank to keep current)</span>
					</label>
					<input id="ai-api-key" type="password" class="field-input font-mono text-sm"
						bind:value={aiKeyInput}
						placeholder={aiForm.ai_provider === 'anthropic' ? 'sk-ant-••••••••••••••••••••••••••••••••' : 'sk-••••••••••••••••••••••••••••••••'}
						autocomplete="off" />
				</div>
				{#if aiError}
					<p class="text-xs text-red-400">{aiError}</p>
				{/if}
				<div class="flex items-center gap-3">
					<button type="submit" disabled={savingAI}
						class="bg-teal-600 hover:bg-teal-500 disabled:opacity-50 text-white text-sm px-4 py-1.5 rounded-lg transition-colors">
						{savingAI ? 'Saving…' : 'Save AI settings'}
					</button>
					{#if aiSaved}
						<span class="text-xs text-green-400">Saved.</span>
					{/if}
				</div>
			</form>
		</div>
	{/if}

	<!-- Instance info -->
	{#if health}
		<div class="bg-zinc-900 border border-zinc-800 rounded-xl divide-y divide-zinc-700">
			<div class="px-6 py-4">
				<p class="text-xs text-zinc-500 uppercase tracking-widest mb-3">Instance</p>
				<dl class="space-y-1.5 text-sm">
					<div class="flex justify-between items-start">
						<dt class="text-zinc-500">Version</dt>
						<dd class="text-right">
							<span class="font-mono text-zinc-300">
								{health.version === 'dev' ? 'dev (local build)' : health.version}
							</span>
							{#if health.version === 'dev'}
								<p class="text-xs text-zinc-600 mt-0.5">Version injected at release build time.</p>
							{:else if !/^v\d/.test(health.version)}
								<p class="text-xs text-zinc-600 mt-0.5">Pre-release build — tag a version to get update checks.</p>
							{:else if health.update_available}
								<p class="text-xs text-yellow-400 mt-0.5">
									<a href="https://github.com/ponack/crucible-iap/releases/latest" target="_blank" rel="noopener" class="hover:underline">
										{health.latest_version} available ↗
									</a>
								</p>
							{:else if health.latest_version}
								<p class="text-xs text-green-500 mt-0.5">Up to date</p>
							{/if}
						</dd>
					</div>
					<div class="flex justify-between">
						<dt class="text-zinc-500">Uptime</dt>
						<dd class="text-zinc-400">{health.uptime}</dd>
					</div>
					<div class="flex justify-between">
						<dt class="text-zinc-500">Database</dt>
						<dd class="{health.db === 'ok' ? 'text-green-400' : 'text-red-400'}">{health.db}</dd>
					</div>
				</dl>
			</div>
		</div>
	{/if}
</div>
