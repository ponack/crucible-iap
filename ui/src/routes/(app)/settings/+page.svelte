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
		}).catch(() => {});
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
	<div class="bg-zinc-900 border border-zinc-800 rounded-xl divide-y divide-zinc-800">
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
						class="bg-indigo-600 hover:bg-indigo-500 disabled:opacity-50 text-white text-sm px-4 py-1.5 rounded-lg transition-colors">
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
						class="bg-indigo-600 hover:bg-indigo-500 disabled:opacity-50 text-white text-sm px-4 py-1.5 rounded-lg transition-colors">
						{savingRetention ? 'Saving…' : 'Save retention policy'}
					</button>
					{#if retentionSaved}
						<span class="text-xs text-green-400">Saved.</span>
					{/if}
				</div>
			</form>
		</div>
	{/if}

	<!-- Instance info -->
	{#if health}
		<div class="bg-zinc-900 border border-zinc-800 rounded-xl divide-y divide-zinc-800">
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
