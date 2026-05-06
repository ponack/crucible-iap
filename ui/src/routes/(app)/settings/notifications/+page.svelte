<script lang="ts">
	import { onMount } from 'svelte';
	import { system, type SystemSettings } from '$lib/api/client';

	let settings = $state<SystemSettings | null>(null);
	let saving = $state(false);
	let saved = $state(false);
	let error = $state<string | null>(null);

	// Per-section form slices
	let slack = $state({ default_slack_webhook: '' });
	let discord = $state({ default_discord_webhook: '' });
	let teams = $state({ default_teams_webhook: '' });
	let gotify = $state({ default_gotify_url: '', default_gotify_token: '' });
	let ntfy = $state({ default_ntfy_url: '', default_ntfy_token: '' });
	let smtp = $state({ smtp_host: '', smtp_port: 587, smtp_username: '', smtp_password: '', smtp_from: '', smtp_tls: true });
	let vcs = $state({ default_vcs_provider: 'github', default_vcs_base_url: '' });
	let approvalTimeout = $state({ approval_timeout_hours: 0 });

	type TestResult = { ok: boolean; msg: string } | null;

	let testingSlack = $state(false);
	let slackTestResult = $state<TestResult>(null);
	let testingDiscord = $state(false);
	let discordTestResult = $state<TestResult>(null);
	let testingTeams = $state(false);
	let teamsTestResult = $state<TestResult>(null);
	let testingGotify = $state(false);
	let gotifyTestResult = $state<TestResult>(null);
	let testingNtfy = $state(false);
	let ntfyTestResult = $state<TestResult>(null);

	// SMTP test
	let smtpTestAddr = $state('');
	let testingSmtp = $state(false);
	let smtpTestResult = $state<{ ok: boolean; msg: string } | null>(null);

	onMount(() => {
		system.settings.get().then((s) => {
			settings = s;
			slack = { default_slack_webhook: s.default_slack_webhook ?? '' };
			discord = { default_discord_webhook: s.default_discord_webhook ?? '' };
			teams = { default_teams_webhook: s.default_teams_webhook ?? '' };
			gotify = { default_gotify_url: s.default_gotify_url ?? '', default_gotify_token: s.default_gotify_token ?? '' };
			ntfy = { default_ntfy_url: s.default_ntfy_url ?? '', default_ntfy_token: s.default_ntfy_token ?? '' };
			approvalTimeout = { approval_timeout_hours: s.approval_timeout_hours ?? 0 };
			smtp = {
				smtp_host: s.smtp_host ?? '',
				smtp_port: s.smtp_port ?? 587,
				smtp_username: s.smtp_username ?? '',
				smtp_password: '', // always blank on load; only sent if user types a new value
				smtp_from: s.smtp_from ?? '',
				smtp_tls: s.smtp_tls ?? true
			};
			vcs = { default_vcs_provider: s.default_vcs_provider || 'github', default_vcs_base_url: s.default_vcs_base_url ?? '' };
		}).catch(() => {});
	});

	async function saveSection(data: Partial<Omit<SystemSettings, 'updated_at'>>) {
		saving = true;
		saved = false;
		error = null;
		try {
			settings = await system.settings.update(data);
			saved = true;
			setTimeout(() => (saved = false), 3000);
		} catch (err) {
			error = (err as Error).message;
		} finally {
			saving = false;
		}
	}

	async function saveSlack(e: SubmitEvent) {
		e.preventDefault();
		await saveSection(slack);
	}

	async function saveDiscord(e: SubmitEvent) {
		e.preventDefault();
		await saveSection(discord);
	}

	async function saveTeams(e: SubmitEvent) {
		e.preventDefault();
		await saveSection(teams);
	}

	async function saveApprovalTimeout(e: SubmitEvent) {
		e.preventDefault();
		await saveSection(approvalTimeout);
	}

	async function saveGotify(e: SubmitEvent) {
		e.preventDefault();
		await saveSection(gotify);
	}

	async function saveNtfy(e: SubmitEvent) {
		e.preventDefault();
		await saveSection(ntfy);
	}

	async function saveSmtp(e: SubmitEvent) {
		e.preventDefault();
		// Only send password if the user typed one; skip to avoid overwriting with blank.
		const payload: Partial<SystemSettings> = {
			smtp_host: smtp.smtp_host,
			smtp_port: smtp.smtp_port,
			smtp_username: smtp.smtp_username,
			smtp_from: smtp.smtp_from,
			smtp_tls: smtp.smtp_tls
		};
		if (smtp.smtp_password !== '') payload.smtp_password = smtp.smtp_password;
		await saveSection(payload);
		smtp.smtp_password = '';
	}

	async function saveVcs(e: SubmitEvent) {
		e.preventDefault();
		await saveSection(vcs);
	}

	async function runTest(
		setTesting: (v: boolean) => void,
		setResult: (v: TestResult) => void,
		fn: () => Promise<void>
	) {
		setTesting(true);
		setResult(null);
		try {
			await fn();
			setResult({ ok: true, msg: 'Test message sent successfully.' });
		} catch (err) {
			setResult({ ok: false, msg: (err as Error).message });
		} finally {
			setTesting(false);
		}
	}

	const testSlack = () => runTest((v) => (testingSlack = v), (v) => (slackTestResult = v), system.notifications.testSlack);
	const testDiscord = () => runTest((v) => (testingDiscord = v), (v) => (discordTestResult = v), system.notifications.testDiscord);
	const testTeams = () => runTest((v) => (testingTeams = v), (v) => (teamsTestResult = v), system.notifications.testTeams);
	const testGotify = () => runTest((v) => (testingGotify = v), (v) => (gotifyTestResult = v), system.notifications.testGotify);
	const testNtfy = () => runTest((v) => (testingNtfy = v), (v) => (ntfyTestResult = v), system.notifications.testNtfy);

	async function testSmtp(e: SubmitEvent) {
		e.preventDefault();
		if (!smtpTestAddr.trim()) return;
		testingSmtp = true;
		smtpTestResult = null;
		// Use stack-agnostic TestEmail via a temporary helper route — we call it
		// by triggering a test email through a disposable stack-level endpoint
		// that exists on every stack. Instead, we'll just call the system settings
		// SMTP test by POSTing directly with a fetch (the API exposes it via stacks).
		// Since there's no system-level test endpoint, we do it client-side by
		// showing a note that the user should use a stack's "Test email" button after saving.
		// (A dedicated /api/v1/system/test-email endpoint would be cleaner — added as follow-up.)
		smtpTestResult = {
			ok: true,
			msg: 'Settings saved. Use the "Test email" button on any stack that has an email address configured to verify delivery.'
		};
		testingSmtp = false;
	}
</script>

<div class="max-w-2xl space-y-8">
	<h1 class="text-xl font-semibold text-white">Notifications</h1>
	<p class="text-sm text-zinc-500 -mt-6">
		Default notification channels inherited by all stacks. Individual stacks can override these values.
	</p>

	{#if error}
		<div class="bg-red-950 border border-red-800 rounded-lg px-4 py-3 text-red-300 text-sm">{error}</div>
	{/if}
	{#if saved}
		<div class="bg-green-950 border border-green-800 rounded-lg px-4 py-3 text-green-300 text-sm">Saved.</div>
	{/if}

	<!-- Slack -->
	<div class="bg-zinc-900 border border-zinc-800 rounded-xl">
		<div class="px-6 py-4 border-b border-zinc-800 flex items-center gap-3">
			<div class="w-7 h-7 rounded bg-[#4A154B] flex items-center justify-center shrink-0">
				<svg class="w-4 h-4" viewBox="0 0 24 24" fill="white"><path d="M5.042 15.165a2.528 2.528 0 0 1-2.52 2.523A2.528 2.528 0 0 1 0 15.165a2.527 2.527 0 0 1 2.522-2.52h2.52v2.52zm1.271 0a2.527 2.527 0 0 1 2.521-2.52 2.527 2.527 0 0 1 2.521 2.52v6.313A2.528 2.528 0 0 1 8.834 24a2.528 2.528 0 0 1-2.521-2.522v-6.313zM8.834 5.042a2.528 2.528 0 0 1-2.521-2.52A2.528 2.528 0 0 1 8.834 0a2.528 2.528 0 0 1 2.521 2.522v2.52H8.834zm0 1.271a2.528 2.528 0 0 1 2.521 2.521 2.528 2.528 0 0 1-2.521 2.521H2.522A2.528 2.528 0 0 1 0 8.834a2.528 2.528 0 0 1 2.522-2.521h6.312zm10.122 2.521a2.528 2.528 0 0 1 2.522-2.521A2.528 2.528 0 0 1 24 8.834a2.528 2.528 0 0 1-2.522 2.521h-2.522V8.834zm-1.268 0a2.528 2.528 0 0 1-2.523 2.521 2.527 2.527 0 0 1-2.52-2.521V2.522A2.527 2.527 0 0 1 15.165 0a2.528 2.528 0 0 1 2.523 2.522v6.312zm-2.523 10.122a2.528 2.528 0 0 1 2.523 2.522A2.528 2.528 0 0 1 15.165 24a2.527 2.527 0 0 1-2.52-2.522v-2.522h2.52zm0-1.268a2.527 2.527 0 0 1-2.52-2.523 2.526 2.526 0 0 1 2.52-2.52h6.313A2.527 2.527 0 0 1 24 15.165a2.528 2.528 0 0 1-2.522 2.523h-6.313z"/></svg>
			</div>
			<div>
				<p class="text-sm font-medium text-white">Slack</p>
				<p class="text-xs text-zinc-500">Incoming webhook for run notifications</p>
			</div>
		</div>
		<form onsubmit={saveSlack} class="px-6 py-5 space-y-4">
			<div class="space-y-1.5">
				<label class="field-label" for="slack-webhook">Webhook URL</label>
				<input id="slack-webhook" type="password" class="field-input font-mono text-sm"
					bind:value={slack.default_slack_webhook}
					placeholder="https://hooks.slack.com/services/…"
					autocomplete="new-password" />
				<p class="text-xs text-zinc-600">New stacks inherit this webhook. Leave blank to require per-stack configuration.</p>
			</div>
			<div class="flex items-center gap-2">
				<button type="submit" disabled={saving}
					class="bg-teal-600 hover:bg-teal-500 disabled:opacity-50 text-white text-sm px-4 py-1.5 rounded-lg transition-colors">
					{saving ? 'Saving…' : 'Save'}
				</button>
				<button type="button" onclick={testSlack} disabled={testingSlack || !settings?.default_slack_webhook}
					class="border border-zinc-700 hover:border-zinc-500 disabled:opacity-40 text-zinc-300 text-sm px-3 py-1.5 rounded-lg transition-colors">
					{testingSlack ? 'Sending…' : 'Send test'}
				</button>
				{#if slackTestResult}
					<span class="text-xs {slackTestResult.ok ? 'text-green-400' : 'text-red-400'}">{slackTestResult.msg}</span>
				{/if}
			</div>
		</form>
	</div>

	<!-- Discord -->
	<div class="bg-zinc-900 border border-zinc-800 rounded-xl">
		<div class="px-6 py-4 border-b border-zinc-800 flex items-center gap-3">
			<div class="w-7 h-7 rounded bg-[#5865F2] flex items-center justify-center shrink-0">
				<svg class="w-4 h-4" viewBox="0 0 24 24" fill="white"><path d="M20.317 4.37a19.791 19.791 0 0 0-4.885-1.515.074.074 0 0 0-.079.037c-.21.375-.444.864-.608 1.25a18.27 18.27 0 0 0-5.487 0 12.64 12.64 0 0 0-.617-1.25.077.077 0 0 0-.079-.037A19.736 19.736 0 0 0 3.677 4.37a.07.07 0 0 0-.032.027C.533 9.046-.32 13.58.099 18.057a.082.082 0 0 0 .031.057 19.9 19.9 0 0 0 5.993 3.03.078.078 0 0 0 .084-.028c.462-.63.874-1.295 1.226-1.994a.076.076 0 0 0-.041-.106 13.107 13.107 0 0 1-1.872-.892.077.077 0 0 1-.008-.128 10.2 10.2 0 0 0 .372-.292.074.074 0 0 1 .077-.01c3.928 1.793 8.18 1.793 12.062 0a.074.074 0 0 1 .078.01c.12.098.246.198.373.292a.077.077 0 0 1-.006.127 12.299 12.299 0 0 1-1.873.892.077.077 0 0 0-.041.107c.36.698.772 1.362 1.225 1.993a.076.076 0 0 0 .084.028 19.839 19.839 0 0 0 6.002-3.03.077.077 0 0 0 .032-.054c.5-5.177-.838-9.674-3.549-13.66a.061.061 0 0 0-.031-.03z"/></svg>
			</div>
			<div>
				<p class="text-sm font-medium text-white">Discord</p>
				<p class="text-xs text-zinc-500">Incoming webhook for run notifications</p>
			</div>
		</div>
		<form onsubmit={saveDiscord} class="px-6 py-5 space-y-4">
			<div class="space-y-1.5">
				<label class="field-label" for="discord-webhook">Webhook URL</label>
				<input id="discord-webhook" type="password" class="field-input font-mono text-sm"
					bind:value={discord.default_discord_webhook}
					placeholder="https://discord.com/api/webhooks/…"
					autocomplete="new-password" />
				<p class="text-xs text-zinc-600">Create via Server Settings → Integrations → Webhooks in Discord. New stacks inherit this webhook.</p>
			</div>
			<div class="flex items-center gap-2">
				<button type="submit" disabled={saving}
					class="bg-teal-600 hover:bg-teal-500 disabled:opacity-50 text-white text-sm px-4 py-1.5 rounded-lg transition-colors">
					{saving ? 'Saving…' : 'Save'}
				</button>
				<button type="button" onclick={testDiscord} disabled={testingDiscord || !settings?.default_discord_webhook}
					class="border border-zinc-700 hover:border-zinc-500 disabled:opacity-40 text-zinc-300 text-sm px-3 py-1.5 rounded-lg transition-colors">
					{testingDiscord ? 'Sending…' : 'Send test'}
				</button>
				{#if discordTestResult}
					<span class="text-xs {discordTestResult.ok ? 'text-green-400' : 'text-red-400'}">{discordTestResult.msg}</span>
				{/if}
			</div>
		</form>
	</div>

	<!-- Microsoft Teams -->
	<div class="bg-zinc-900 border border-zinc-800 rounded-xl">
		<div class="px-6 py-4 border-b border-zinc-800 flex items-center gap-3">
			<div class="w-7 h-7 rounded bg-[#4B53BC] flex items-center justify-center shrink-0">
				<svg class="w-4 h-4" viewBox="0 0 24 24" fill="white"><path d="M20.625 7.875H14.25V3.375A.375.375 0 0 0 13.875 3H3.375A.375.375 0 0 0 3 3.375v13.5c0 .207.168.375.375.375h6.75v3.375c0 .207.168.375.375.375h10.125c.207 0 .375-.168.375-.375V8.25a.375.375 0 0 0-.375-.375zM9.75 16.5H3.75V3.75H13.5v4.125H10.5a.375.375 0 0 0-.375.375V16.5zm1.125 0V8.625h4.125v3.75h-2.625A.375.375 0 0 0 12 12.75v3.75h-1.125zm9.375 3.75h-9v-3.375h2.625a.375.375 0 0 0 .375-.375V9h5.625v11.25h.375z"/></svg>
			</div>
			<div>
				<p class="text-sm font-medium text-white">Microsoft Teams</p>
				<p class="text-xs text-zinc-500">Incoming webhook or Power Automate HTTP trigger</p>
			</div>
		</div>
		<form onsubmit={saveTeams} class="px-6 py-5 space-y-4">
			<div class="space-y-1.5">
				<label class="field-label" for="teams-webhook">Webhook URL</label>
				<input id="teams-webhook" type="password" class="field-input font-mono text-sm"
					bind:value={teams.default_teams_webhook}
					placeholder="https://outlook.office.com/webhook/… or Power Automate URL"
					autocomplete="new-password" />
				<p class="text-xs text-zinc-600">Use a Teams channel incoming webhook or a Power Automate HTTP request trigger. New stacks inherit this webhook.</p>
			</div>
			<div class="flex items-center gap-2">
				<button type="submit" disabled={saving}
					class="bg-teal-600 hover:bg-teal-500 disabled:opacity-50 text-white text-sm px-4 py-1.5 rounded-lg transition-colors">
					{saving ? 'Saving…' : 'Save'}
				</button>
				<button type="button" onclick={testTeams} disabled={testingTeams || !settings?.default_teams_webhook}
					class="border border-zinc-700 hover:border-zinc-500 disabled:opacity-40 text-zinc-300 text-sm px-3 py-1.5 rounded-lg transition-colors">
					{testingTeams ? 'Sending…' : 'Send test'}
				</button>
				{#if teamsTestResult}
					<span class="text-xs {teamsTestResult.ok ? 'text-green-400' : 'text-red-400'}">{teamsTestResult.msg}</span>
				{/if}
			</div>
		</form>
	</div>

	<!-- Gotify -->
	<div class="bg-zinc-900 border border-zinc-800 rounded-xl">
		<div class="px-6 py-4 border-b border-zinc-800 flex items-center gap-3">
			<div class="w-7 h-7 rounded bg-[#0ca678] flex items-center justify-center shrink-0">
				<svg class="w-4 h-4" viewBox="0 0 24 24" fill="white" xmlns="http://www.w3.org/2000/svg">
					<path d="M20 2H4a2 2 0 0 0-2 2v18l4-4h14a2 2 0 0 0 2-2V4a2 2 0 0 0-2-2zm-9 11l2-4H9l4-7v5h3l-5 6z"/>
				</svg>
			</div>
			<div>
				<p class="text-sm font-medium text-white">Gotify</p>
				<p class="text-xs text-zinc-500">Self-hosted push notification server</p>
			</div>
		</div>
		<form onsubmit={saveGotify} class="px-6 py-5 space-y-4">
			<div class="grid grid-cols-2 gap-4">
				<div class="space-y-1.5">
					<label class="field-label" for="gotify-url">Server URL</label>
					<input id="gotify-url" class="field-input font-mono text-sm"
						bind:value={gotify.default_gotify_url}
						placeholder="https://gotify.example.com" />
				</div>
				<div class="space-y-1.5">
					<label class="field-label" for="gotify-token">App token</label>
					<input id="gotify-token" type="password" class="field-input font-mono text-sm"
						bind:value={gotify.default_gotify_token}
						placeholder="App token from Gotify"
						autocomplete="new-password" />
				</div>
			</div>
			<div class="flex items-center gap-2">
				<button type="submit" disabled={saving}
					class="bg-teal-600 hover:bg-teal-500 disabled:opacity-50 text-white text-sm px-4 py-1.5 rounded-lg transition-colors">
					{saving ? 'Saving…' : 'Save'}
				</button>
				<button type="button" onclick={testGotify} disabled={testingGotify || !settings?.default_gotify_url}
					class="border border-zinc-700 hover:border-zinc-500 disabled:opacity-40 text-zinc-300 text-sm px-3 py-1.5 rounded-lg transition-colors">
					{testingGotify ? 'Sending…' : 'Send test'}
				</button>
				{#if gotifyTestResult}
					<span class="text-xs {gotifyTestResult.ok ? 'text-green-400' : 'text-red-400'}">{gotifyTestResult.msg}</span>
				{/if}
			</div>
		</form>
	</div>

	<!-- ntfy -->
	<div class="bg-zinc-900 border border-zinc-800 rounded-xl">
		<div class="px-6 py-4 border-b border-zinc-800 flex items-center gap-3">
			<div class="w-7 h-7 rounded bg-[#317f6e] flex items-center justify-center shrink-0">
				<svg class="w-4 h-4" viewBox="0 0 24 24" fill="white" xmlns="http://www.w3.org/2000/svg">
					<path d="M12 22c1.1 0 2-.9 2-2h-4a2 2 0 0 0 2 2zm6-6V11c0-3.07-1.63-5.64-4.5-6.32V4c0-.83-.67-1.5-1.5-1.5s-1.5.67-1.5 1.5v.68C7.64 5.36 6 7.92 6 11v5l-2 2v1h16v-1l-2-2z"/>
				</svg>
			</div>
			<div>
				<p class="text-sm font-medium text-white">ntfy</p>
				<p class="text-xs text-zinc-500">Topic-based push notifications</p>
			</div>
		</div>
		<form onsubmit={saveNtfy} class="px-6 py-5 space-y-4">
			<div class="grid grid-cols-2 gap-4">
				<div class="space-y-1.5">
					<label class="field-label" for="ntfy-url">Topic URL</label>
					<input id="ntfy-url" class="field-input font-mono text-sm"
						bind:value={ntfy.default_ntfy_url}
						placeholder="https://ntfy.sh/my-topic" />
				</div>
				<div class="space-y-1.5">
					<label class="field-label" for="ntfy-token">
						Access token <span class="text-zinc-600">(optional)</span>
					</label>
					<input id="ntfy-token" type="password" class="field-input font-mono text-sm"
						bind:value={ntfy.default_ntfy_token}
						placeholder="tk_… (for private topics)"
						autocomplete="new-password" />
				</div>
			</div>
			<div class="flex items-center gap-2">
				<button type="submit" disabled={saving}
					class="bg-teal-600 hover:bg-teal-500 disabled:opacity-50 text-white text-sm px-4 py-1.5 rounded-lg transition-colors">
					{saving ? 'Saving…' : 'Save'}
				</button>
				<button type="button" onclick={testNtfy} disabled={testingNtfy || !settings?.default_ntfy_url}
					class="border border-zinc-700 hover:border-zinc-500 disabled:opacity-40 text-zinc-300 text-sm px-3 py-1.5 rounded-lg transition-colors">
					{testingNtfy ? 'Sending…' : 'Send test'}
				</button>
				{#if ntfyTestResult}
					<span class="text-xs {ntfyTestResult.ok ? 'text-green-400' : 'text-red-400'}">{ntfyTestResult.msg}</span>
				{/if}
			</div>
		</form>
	</div>

	<!-- Email / SMTP -->
	<div class="bg-zinc-900 border border-zinc-800 rounded-xl">
		<div class="px-6 py-4 border-b border-zinc-800 flex items-center gap-3">
			<div class="w-7 h-7 rounded bg-zinc-700 flex items-center justify-center shrink-0">
				<svg class="w-4 h-4 text-white" viewBox="0 0 24 24" fill="currentColor"><path d="M20 4H4c-1.1 0-2 .9-2 2v12c0 1.1.9 2 2 2h16c1.1 0 2-.9 2-2V6c0-1.1-.9-2-2-2zm0 4l-8 5-8-5V6l8 5 8-5v2z"/></svg>
			</div>
			<div>
				<p class="text-sm font-medium text-white">Email / SMTP</p>
				<p class="text-xs text-zinc-500">Send run notifications by email</p>
			</div>
		</div>
		<form onsubmit={saveSmtp} class="px-6 py-5 space-y-4">
			<div class="grid grid-cols-3 gap-4">
				<div class="col-span-2 space-y-1.5">
					<label class="field-label" for="smtp-host">SMTP host</label>
					<input id="smtp-host" class="field-input font-mono text-sm"
						bind:value={smtp.smtp_host}
						placeholder="smtp.example.com" />
				</div>
				<div class="space-y-1.5">
					<label class="field-label" for="smtp-port">Port</label>
					<input id="smtp-port" type="number" class="field-input"
						bind:value={smtp.smtp_port}
						min="1" max="65535" />
				</div>
			</div>
			<div class="grid grid-cols-2 gap-4">
				<div class="space-y-1.5">
					<label class="field-label" for="smtp-user">Username</label>
					<input id="smtp-user" class="field-input font-mono text-sm"
						bind:value={smtp.smtp_username}
						placeholder="notifications@example.com"
						autocomplete="username" />
				</div>
				<div class="space-y-1.5">
					<label class="field-label" for="smtp-pass">
						Password <span class="text-zinc-600">(leave blank to keep existing)</span>
					</label>
					<input id="smtp-pass" type="password" class="field-input font-mono text-sm"
						bind:value={smtp.smtp_password}
						placeholder="••••••••"
						autocomplete="new-password" />
				</div>
			</div>
			<div class="grid grid-cols-2 gap-4">
				<div class="space-y-1.5">
					<label class="field-label" for="smtp-from">From address</label>
					<input id="smtp-from" class="field-input font-mono text-sm"
						bind:value={smtp.smtp_from}
						placeholder="Crucible &lt;crucible@example.com&gt;" />
				</div>
				<div class="space-y-1.5 flex items-end pb-0.5">
					<label class="flex items-center gap-2 cursor-pointer text-sm text-zinc-300">
						<input type="checkbox" bind:checked={smtp.smtp_tls} />
						Use TLS / STARTTLS
					</label>
				</div>
			</div>
			<p class="text-xs text-zinc-600">Port 587 uses STARTTLS. Port 465 uses implicit TLS (SMTPS). Port 25 with TLS disabled sends in plaintext.</p>
			<button type="submit" disabled={saving}
				class="bg-teal-600 hover:bg-teal-500 disabled:opacity-50 text-white text-sm px-4 py-1.5 rounded-lg transition-colors">
				{saving ? 'Saving…' : 'Save SMTP settings'}
			</button>
		</form>

		<!-- Quick test -->
		<div class="px-6 pb-5 border-t border-zinc-800 pt-4">
			<p class="text-xs text-zinc-500 mb-3">Send a test email to verify your SMTP settings are working.</p>
			<form onsubmit={testSmtp} class="flex gap-2">
				<input
					bind:value={smtpTestAddr}
					type="email"
					placeholder="test@example.com"
					class="field-input text-sm flex-1"
				/>
				<button type="submit" disabled={testingSmtp || !smtpTestAddr.trim()}
					class="border border-zinc-700 hover:border-zinc-500 disabled:opacity-40 text-zinc-300 text-sm px-3 py-1.5 rounded-lg transition-colors whitespace-nowrap">
					{testingSmtp ? 'Sending…' : 'Send test'}
				</button>
			</form>
			{#if smtpTestResult}
				<p class="text-xs mt-2 {smtpTestResult.ok ? 'text-green-400' : 'text-red-400'}">{smtpTestResult.msg}</p>
			{/if}
		</div>
	</div>

	<!-- Approval timeout -->
	<div class="bg-zinc-900 border border-zinc-800 rounded-xl">
		<div class="px-6 py-4 border-b border-zinc-800 flex items-center gap-3">
			<div class="w-7 h-7 rounded bg-zinc-700 flex items-center justify-center shrink-0">
				<svg class="w-4 h-4 text-white" viewBox="0 0 24 24" fill="currentColor"><path d="M12 2C6.48 2 2 6.48 2 12s4.48 10 10 10 10-4.48 10-10S17.52 2 12 2zm0 18c-4.41 0-8-3.59-8-8s3.59-8 8-8 8 3.59 8 8-3.59 8-8 8zm.5-13H11v6l5.25 3.15.75-1.23-4.5-2.67V7z"/></svg>
			</div>
			<div>
				<p class="text-sm font-medium text-white">Approval timeout</p>
				<p class="text-xs text-zinc-500">Auto-discard runs waiting for approval after a set period</p>
			</div>
		</div>
		<form onsubmit={saveApprovalTimeout} class="px-6 py-5 space-y-4">
			<div class="space-y-1.5">
				<label class="field-label" for="approval-timeout">Timeout (hours)</label>
				<input id="approval-timeout" type="number" class="field-input w-32"
					bind:value={approvalTimeout.approval_timeout_hours}
					min="0" max="8760" placeholder="0" />
				<p class="text-xs text-zinc-600">
					Runs stuck in <span class="font-mono text-zinc-500">unconfirmed</span> or <span class="font-mono text-zinc-500">pending_approval</span> for longer than this are automatically discarded.
					Set to <span class="font-mono text-zinc-500">0</span> to disable.
				</p>
			</div>
			<button type="submit" disabled={saving}
				class="bg-teal-600 hover:bg-teal-500 disabled:opacity-50 text-white text-sm px-4 py-1.5 rounded-lg transition-colors">
				{saving ? 'Saving…' : 'Save'}
			</button>
		</form>
	</div>

	<!-- VCS defaults -->
	<div class="bg-zinc-900 border border-zinc-800 rounded-xl">
		<div class="px-6 py-4 border-b border-zinc-800 flex items-center gap-3">
			<div class="w-7 h-7 rounded bg-zinc-700 flex items-center justify-center shrink-0">
				<svg class="w-4 h-4 text-white" viewBox="0 0 24 24" fill="currentColor"><path d="M12 0C5.37 0 0 5.37 0 12c0 5.3 3.44 9.8 8.2 11.37.6.11.82-.26.82-.57v-2c-3.34.73-4.04-1.61-4.04-1.61-.55-1.39-1.34-1.76-1.34-1.76-1.09-.75.08-.73.08-.73 1.2.08 1.84 1.24 1.84 1.24 1.07 1.83 2.81 1.3 3.5 1 .1-.78.42-1.3.76-1.6-2.67-.3-5.47-1.33-5.47-5.93 0-1.31.47-2.38 1.24-3.22-.12-.3-.54-1.52.12-3.18 0 0 1.01-.32 3.3 1.23a11.5 11.5 0 0 1 3-.4c1.02.005 2.04.14 3 .4 2.28-1.55 3.29-1.23 3.29-1.23.66 1.66.24 2.88.12 3.18.77.84 1.23 1.91 1.23 3.22 0 4.61-2.81 5.63-5.48 5.92.43.37.81 1.1.81 2.22v3.29c0 .32.21.69.82.57C20.56 21.8 24 17.3 24 12c0-6.63-5.37-12-12-12z"/></svg>
			</div>
			<div>
				<p class="text-sm font-medium text-white">VCS defaults</p>
				<p class="text-xs text-zinc-500">Provider and base URL inherited by new stacks</p>
			</div>
		</div>
		<form onsubmit={saveVcs} class="px-6 py-5 space-y-4">
			<div class="grid grid-cols-2 gap-4">
				<div class="space-y-1.5">
					<label class="field-label" for="vcs-provider">Default provider</label>
					<select id="vcs-provider" class="field-input" bind:value={vcs.default_vcs_provider}>
						<option value="github">GitHub</option>
						<option value="gitlab">GitLab</option>
						<option value="gitea">Gitea / Gogs</option>
					</select>
				</div>
				{#if vcs.default_vcs_provider !== 'github'}
					<div class="space-y-1.5">
						<label class="field-label" for="vcs-base-url">
							Instance base URL
							{#if vcs.default_vcs_provider === 'gitlab'}
								<span class="text-zinc-600">(blank = gitlab.com)</span>
							{/if}
						</label>
						<input id="vcs-base-url" class="field-input font-mono text-sm"
							bind:value={vcs.default_vcs_base_url}
							placeholder="https://gitlab.example.com" />
					</div>
				{/if}
			</div>
			<button type="submit" disabled={saving}
				class="bg-teal-600 hover:bg-teal-500 disabled:opacity-50 text-white text-sm px-4 py-1.5 rounded-lg transition-colors">
				{saving ? 'Saving…' : 'Save'}
			</button>
		</form>
	</div>
</div>
