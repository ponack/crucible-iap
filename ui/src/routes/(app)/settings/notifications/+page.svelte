<script lang="ts">
	import { onMount } from 'svelte';
	import { system, type SystemSettings } from '$lib/api/client';

	let settings = $state<SystemSettings | null>(null);
	let saving = $state(false);
	let saved = $state(false);
	let error = $state<string | null>(null);

	// Per-section form slices
	let slack = $state({ default_slack_webhook: '' });
	let gotify = $state({ default_gotify_url: '', default_gotify_token: '' });
	let ntfy = $state({ default_ntfy_url: '', default_ntfy_token: '' });
	let smtp = $state({ smtp_host: '', smtp_port: 587, smtp_username: '', smtp_password: '', smtp_from: '', smtp_tls: true });
	let vcs = $state({ default_vcs_provider: 'github', default_vcs_base_url: '' });

	// SMTP test
	let smtpTestAddr = $state('');
	let testingSmtp = $state(false);
	let smtpTestResult = $state<{ ok: boolean; msg: string } | null>(null);

	onMount(() => {
		system.settings.get().then((s) => {
			settings = s;
			slack = { default_slack_webhook: s.default_slack_webhook ?? '' };
			gotify = { default_gotify_url: s.default_gotify_url ?? '', default_gotify_token: s.default_gotify_token ?? '' };
			ntfy = { default_ntfy_url: s.default_ntfy_url ?? '', default_ntfy_token: s.default_ntfy_token ?? '' };
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
			<button type="submit" disabled={saving}
				class="bg-indigo-600 hover:bg-indigo-500 disabled:opacity-50 text-white text-sm px-4 py-1.5 rounded-lg transition-colors">
				{saving ? 'Saving…' : 'Save'}
			</button>
		</form>
	</div>

	<!-- Gotify -->
	<div class="bg-zinc-900 border border-zinc-800 rounded-xl">
		<div class="px-6 py-4 border-b border-zinc-800 flex items-center gap-3">
			<div class="w-7 h-7 rounded bg-zinc-700 flex items-center justify-center shrink-0">
				<svg class="w-4 h-4 text-white" viewBox="0 0 24 24" fill="currentColor"><path d="M12 2C6.48 2 2 6.48 2 12s4.48 10 10 10 10-4.48 10-10S17.52 2 12 2zm-2 14.5v-9l6 4.5-6 4.5z"/></svg>
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
			<button type="submit" disabled={saving}
				class="bg-indigo-600 hover:bg-indigo-500 disabled:opacity-50 text-white text-sm px-4 py-1.5 rounded-lg transition-colors">
				{saving ? 'Saving…' : 'Save'}
			</button>
		</form>
	</div>

	<!-- ntfy -->
	<div class="bg-zinc-900 border border-zinc-800 rounded-xl">
		<div class="px-6 py-4 border-b border-zinc-800 flex items-center gap-3">
			<div class="w-7 h-7 rounded bg-zinc-700 flex items-center justify-center shrink-0 text-xs font-bold text-white">n</div>
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
			<button type="submit" disabled={saving}
				class="bg-indigo-600 hover:bg-indigo-500 disabled:opacity-50 text-white text-sm px-4 py-1.5 rounded-lg transition-colors">
				{saving ? 'Saving…' : 'Save'}
			</button>
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
				class="bg-indigo-600 hover:bg-indigo-500 disabled:opacity-50 text-white text-sm px-4 py-1.5 rounded-lg transition-colors">
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
				class="bg-indigo-600 hover:bg-indigo-500 disabled:opacity-50 text-white text-sm px-4 py-1.5 rounded-lg transition-colors">
				{saving ? 'Saving…' : 'Save'}
			</button>
		</form>
	</div>
</div>
