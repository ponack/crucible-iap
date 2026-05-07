<script lang="ts">
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { githubApp, type GitHubAppView } from '$lib/api/client';
	import { toast } from '$lib/stores/toasts.svelte';

	let app = $state<GitHubAppView | null>(null);
	let loading = $state(true);

	let showForm = $state(false);
	let saving = $state(false);
	let formError = $state<string | null>(null);

	let appID = $state('');
	let slug = $state('');
	let name = $state('');
	let clientID = $state('');
	let clientSecret = $state('');
	let privateKey = $state('');
	let webhookSecret = $state('');

	let installing = $state(false);

	onMount(async () => {
		try {
			app = await githubApp.get();
		} catch (e) {
			toast.error((e as Error).message);
		} finally {
			loading = false;
		}
		if (page.url.searchParams.get('installed') === '1') {
			toast.success('GitHub App installed');
			const url = new URL(page.url);
			url.searchParams.delete('installed');
			window.history.replaceState({}, '', url.toString());
		}
	});

	function startNew() {
		appID = '';
		slug = '';
		name = '';
		clientID = '';
		clientSecret = '';
		privateKey = '';
		webhookSecret = '';
		formError = null;
		showForm = true;
	}

	function startReplace() {
		appID = String(app?.app_id ?? '');
		slug = app?.slug ?? '';
		name = app?.name ?? '';
		clientID = app?.client_id ?? '';
		clientSecret = '';
		privateKey = '';
		webhookSecret = '';
		formError = null;
		showForm = true;
	}

	async function save() {
		formError = null;
		const numAppID = Number(appID);
		if (!numAppID || !Number.isInteger(numAppID) || numAppID <= 0) {
			formError = 'App ID must be a positive integer';
			return;
		}
		if (!slug || !name || !clientID || !clientSecret || !privateKey || !webhookSecret) {
			formError = 'All fields are required';
			return;
		}
		saving = true;
		try {
			await githubApp.register({
				app_id: numAppID,
				slug,
				name,
				client_id: clientID,
				client_secret: clientSecret,
				private_key: privateKey,
				webhook_secret: webhookSecret
			});
			app = await githubApp.get();
			toast.success('GitHub App registered');
			showForm = false;
		} catch (e) {
			formError = (e as Error).message;
		} finally {
			saving = false;
		}
	}

	async function remove() {
		if (!confirm('Delete the GitHub App registration? Stacks using it will fall back to PAT auth.'))
			return;
		try {
			await githubApp.delete();
			app = null;
			toast.success('GitHub App removed');
		} catch (e) {
			toast.error((e as Error).message);
		}
	}

	async function startInstall() {
		installing = true;
		try {
			const { install_url } = await githubApp.startInstall();
			window.location.href = install_url;
		} catch (e) {
			toast.error((e as Error).message);
			installing = false;
		}
	}

	async function deleteInstallation(installID: string, login: string) {
		if (!confirm(`Remove the installation on ${login}? Stacks using it will detach.`)) return;
		try {
			await githubApp.deleteInstallation(installID);
			app = await githubApp.get();
			toast.success('Installation removed');
		} catch (e) {
			toast.error((e as Error).message);
		}
	}

	async function copy(text: string, label: string) {
		try {
			await navigator.clipboard.writeText(text);
			toast.success(`${label} copied`);
		} catch {
			toast.error('Copy failed — your browser may block clipboard access');
		}
	}
</script>

<div class="max-w-3xl">
	<div class="mb-6">
		<h1 class="text-xl font-semibold text-white">GitHub App</h1>
		<p class="text-sm text-zinc-400 mt-1">
			Register one GitHub App per organization to replace per-stack personal access tokens and
			webhook secrets. Crucible mints short-lived installation tokens automatically — no per-stack
			secrets to rotate, and a single global webhook URL covers every connected repository.
		</p>
	</div>

	{#if loading}
		<p class="text-sm text-zinc-500">Loading…</p>
	{:else if !app && !showForm}
		<!-- Setup guide -->
		<div class="rounded-xl border border-zinc-800 bg-zinc-900/50 p-6 space-y-6">
			<div>
				<p class="text-sm font-medium text-zinc-200 mb-1">Setup overview</p>
				<p class="text-sm text-zinc-400">
					Complete these steps once. After setup, every stack can use App-based authentication
					without any per-stack webhook or token configuration.
				</p>
			</div>

			<ol class="space-y-5 text-sm">
				<!-- Step 1 -->
				<li class="flex gap-3">
					<span class="flex-shrink-0 mt-0.5 w-5 h-5 rounded-full bg-zinc-700 text-zinc-300 text-xs flex items-center justify-center font-semibold">1</span>
					<div class="space-y-2 min-w-0">
						<p class="text-zinc-200 font-medium">Create the GitHub App on GitHub</p>
						<p class="text-zinc-400">
							Go to
							<a
								href="https://github.com/settings/apps/new"
								target="_blank"
								rel="noopener"
								class="text-teal-400 hover:underline">github.com/settings/apps/new</a
							>
							(or <code class="text-zinc-300 bg-zinc-800 px-1 rounded text-xs">https://[host]/settings/apps/new</code>
							on GitHub Enterprise).
						</p>
						<div class="rounded-lg bg-zinc-800/60 border border-zinc-700/50 px-4 py-3 space-y-3">
							<div>
								<p class="text-xs font-semibold text-zinc-300 mb-1">Repository permissions required</p>
								<ul class="text-xs text-zinc-400 space-y-1">
									<li>• Contents → <span class="text-zinc-200 font-medium">Read-only</span> — clone repositories during runs</li>
									<li>• Metadata → <span class="text-zinc-200 font-medium">Read-only</span> — required for all GitHub Apps</li>
									<li>• Pull requests → <span class="text-zinc-200 font-medium">Read & write</span> — post plan summary comments on PRs</li>
									<li>• Commit statuses → <span class="text-zinc-200 font-medium">Read & write</span> — report plan/apply result as a commit status check</li>
								</ul>
							</div>
							<div>
								<p class="text-xs font-semibold text-zinc-300 mb-1">Webhook events to subscribe</p>
								<p class="text-xs text-zinc-400">
									<span class="text-zinc-200 font-medium">Push</span> ·
									<span class="text-zinc-200 font-medium">Pull request</span> ·
									<span class="text-zinc-200 font-medium">Create</span>
								</p>
							</div>
							<div>
								<p class="text-xs font-semibold text-zinc-300 mb-1">Webhook section</p>
								<p class="text-xs text-zinc-400">
									Check <span class="text-zinc-200 font-medium">Active</span>. Leave the Webhook URL
									blank for now — you will paste it from Crucible after registering in step 3.
								</p>
							</div>
						</div>
					</div>
				</li>

				<!-- Step 2 -->
				<li class="flex gap-3">
					<span class="flex-shrink-0 mt-0.5 w-5 h-5 rounded-full bg-zinc-700 text-zinc-300 text-xs flex items-center justify-center font-semibold">2</span>
					<div class="space-y-1.5 min-w-0">
						<p class="text-zinc-200 font-medium">Copy credentials from GitHub</p>
						<p class="text-zinc-400">
							After creating the App, on its <em>General</em> settings page: note the
							<span class="text-zinc-200">App ID</span> and
							<span class="text-zinc-200">Client ID</span> shown near the top.
						</p>
						<ul class="text-xs text-zinc-400 space-y-1 mt-1 ml-0.5">
							<li>
								• Under <em>Client secrets</em> — click
								<em>Generate a new client secret</em> and copy it immediately (shown once).
							</li>
							<li>
								• Under <em>Private keys</em> — click
								<em>Generate a private key</em> and save the downloaded <code class="text-zinc-300 bg-zinc-800 px-1 rounded text-xs">.pem</code> file.
							</li>
							<li>
								• Choose a random string as your <em>Webhook secret</em> (e.g.
								<code class="text-zinc-300 bg-zinc-800 px-1 rounded text-xs">openssl rand -hex 32</code>).
								Save it — you will paste it into both GitHub and Crucible.
							</li>
						</ul>
					</div>
				</li>

				<!-- Step 3 -->
				<li class="flex gap-3">
					<span class="flex-shrink-0 mt-0.5 w-5 h-5 rounded-full bg-zinc-700 text-zinc-300 text-xs flex items-center justify-center font-semibold">3</span>
					<div class="space-y-1 min-w-0">
						<p class="text-zinc-200 font-medium">Register in Crucible</p>
						<p class="text-zinc-400">
							Click <em>Register GitHub App</em> below and paste the credentials into the form.
						</p>
					</div>
				</li>

				<!-- Step 4 -->
				<li class="flex gap-3">
					<span class="flex-shrink-0 mt-0.5 w-5 h-5 rounded-full bg-zinc-700 text-zinc-300 text-xs flex items-center justify-center font-semibold">4</span>
					<div class="space-y-1 min-w-0">
						<p class="text-zinc-200 font-medium">Wire the URLs back into your GitHub App</p>
						<p class="text-zinc-400">
							Crucible will show you two URLs. Go back to your GitHub App's settings and paste them
							into the correct fields (detailed on the next screen after registering).
						</p>
					</div>
				</li>

				<!-- Step 5 -->
				<li class="flex gap-3">
					<span class="flex-shrink-0 mt-0.5 w-5 h-5 rounded-full bg-zinc-700 text-zinc-300 text-xs flex items-center justify-center font-semibold">5</span>
					<div class="space-y-1 min-w-0">
						<p class="text-zinc-200 font-medium">Install on a GitHub account or organization</p>
						<p class="text-zinc-400">
							Click <em>Install on GitHub</em> to grant Crucible access to repositories under a
							GitHub user or organization. You can install on multiple accounts.
						</p>
					</div>
				</li>

				<!-- Step 6 -->
				<li class="flex gap-3">
					<span class="flex-shrink-0 mt-0.5 w-5 h-5 rounded-full bg-zinc-700 text-zinc-300 text-xs flex items-center justify-center font-semibold">6</span>
					<div class="space-y-1 min-w-0">
						<p class="text-zinc-200 font-medium">Connect stacks</p>
						<p class="text-zinc-400">
							On any GitHub stack, open its <em>Settings</em> tab and scroll to
							<em>GitHub App authentication</em>. Select the installation that has access to that
							stack's repository. The stack will use App tokens instead of a PAT from then on.
						</p>
					</div>
				</li>
			</ol>

			<button
				onclick={startNew}
				class="bg-teal-600 hover:bg-teal-500 text-white text-sm px-4 py-2 rounded-lg transition-colors"
			>
				Register GitHub App
			</button>
		</div>
	{:else if app && !showForm}
		<!-- Registered app details -->
		<div class="rounded-xl border border-zinc-800 bg-zinc-900/50 p-6 space-y-4 mb-6">
			<div class="flex items-start justify-between">
				<div>
					<h2 class="text-base font-medium text-white">{app.name}</h2>
					<p class="text-xs text-zinc-500 mt-0.5">slug: {app.slug}</p>
				</div>
				<div class="flex gap-2">
					<button
						onclick={startReplace}
						class="text-sm px-3 py-1.5 rounded-lg bg-zinc-800 hover:bg-zinc-700 text-zinc-200 transition-colors"
					>
						Replace credentials
					</button>
					<button
						onclick={remove}
						class="text-sm px-3 py-1.5 rounded-lg bg-red-950 hover:bg-red-900 text-red-300 border border-red-900 transition-colors"
					>
						Delete
					</button>
				</div>
			</div>
			<dl class="grid grid-cols-2 gap-x-6 gap-y-2 text-sm pt-2">
				<dt class="text-zinc-500">App ID</dt>
				<dd class="text-zinc-200 font-mono">{app.app_id}</dd>
				<dt class="text-zinc-500">Client ID</dt>
				<dd class="text-zinc-200 font-mono">{app.client_id}</dd>
				<dt class="text-zinc-500">Registered</dt>
				<dd class="text-zinc-200">{new Date(app.created_at).toLocaleString()}</dd>
				<dt class="text-zinc-500">Updated</dt>
				<dd class="text-zinc-200">{new Date(app.updated_at).toLocaleString()}</dd>
			</dl>
		</div>

		<!-- URL wiring -->
		<div class="rounded-xl border border-zinc-800 bg-zinc-900/50 p-6 space-y-5 mb-6">
			<div>
				<h3 class="text-base font-medium text-white">Wire these URLs into your GitHub App</h3>
				<p class="text-sm text-zinc-400 mt-1">
					In your GitHub App's <em>General</em> settings, paste each URL into the field indicated
					below. Both are derived from <code class="text-zinc-300 bg-zinc-800 px-1 rounded text-xs">CRUCIBLE_BASE_URL</code>.
				</p>
			</div>

			<div class="space-y-4">
				<div>
					<div class="flex items-center gap-2 mb-1">
						<label class="text-xs text-zinc-500" for="webhook-url">Webhook URL</label>
						<span class="text-xs text-zinc-600">→ paste into the <em>Webhook URL</em> field on GitHub</span>
					</div>
					<div class="flex gap-2">
						<input
							id="webhook-url"
							readonly
							value={app.webhook_url}
							class="flex-1 bg-zinc-800 border border-zinc-700 rounded-lg px-3 py-1.5 text-sm text-zinc-200 font-mono"
						/>
						<button
							onclick={() => copy(app!.webhook_url, 'Webhook URL')}
							class="text-sm px-3 py-1.5 rounded-lg bg-zinc-800 hover:bg-zinc-700 text-zinc-200 transition-colors"
						>
							Copy
						</button>
					</div>
				</div>

				<div>
					<div class="flex items-center gap-2 mb-1">
						<label class="text-xs text-zinc-500" for="setup-url">Setup URL</label>
						<span class="text-xs text-zinc-600">→ paste into the <em>Setup URL (post-install callback URL)</em> field on GitHub</span>
					</div>
					<div class="flex gap-2">
						<input
							id="setup-url"
							readonly
							value={app.setup_url}
							class="flex-1 bg-zinc-800 border border-zinc-700 rounded-lg px-3 py-1.5 text-sm text-zinc-200 font-mono"
						/>
						<button
							onclick={() => copy(app!.setup_url, 'Setup URL')}
							class="text-sm px-3 py-1.5 rounded-lg bg-zinc-800 hover:bg-zinc-700 text-zinc-200 transition-colors"
						>
							Copy
						</button>
					</div>
					<p class="text-xs text-zinc-500 mt-1.5">
						Also tick <span class="text-zinc-300">Redirect on update</span> in GitHub App settings so
						reinstallations return to Crucible.
					</p>
				</div>
			</div>

			<!-- Permissions + events reminder -->
			<div class="rounded-lg bg-zinc-800/60 border border-zinc-700/50 px-4 py-3 space-y-3">
				<p class="text-xs font-semibold text-zinc-300">Verify your App has these settings</p>
				<div class="grid grid-cols-2 gap-4">
					<div>
						<p class="text-xs font-medium text-zinc-400 mb-1">Repository permissions</p>
						<ul class="text-xs text-zinc-500 space-y-0.5">
							<li>• Contents — Read-only</li>
							<li>• Metadata — Read-only</li>
							<li>• Pull requests — Read & write</li>
							<li>• Commit statuses — Read & write</li>
						</ul>
					</div>
					<div>
						<p class="text-xs font-medium text-zinc-400 mb-1">Webhook events</p>
						<ul class="text-xs text-zinc-500 space-y-0.5">
							<li>• Push</li>
							<li>• Pull request</li>
							<li>• Create</li>
						</ul>
					</div>
				</div>
			</div>
		</div>

		<!-- Installations -->
		<div class="rounded-xl border border-zinc-800 bg-zinc-900/50 p-6">
			<div class="flex items-center justify-between mb-4">
				<div>
					<h3 class="text-base font-medium text-white">Installations</h3>
					<p class="text-xs text-zinc-500 mt-0.5">
						Each installation grants Crucible access to repositories under a GitHub user or
						organization. A stack can only use an installation that has access to its repository.
					</p>
				</div>
				<button
					onclick={startInstall}
					disabled={installing}
					class="flex-shrink-0 bg-teal-600 hover:bg-teal-500 disabled:opacity-50 text-white text-sm px-4 py-2 rounded-lg transition-colors"
				>
					{installing ? 'Redirecting…' : 'Install on GitHub'}
				</button>
			</div>
			{#if app.installations.length === 0}
				<div class="rounded-lg bg-zinc-800/40 border border-zinc-700/50 px-4 py-4 text-sm text-zinc-400">
					<p class="font-medium text-zinc-300 mb-1">No installations yet</p>
					<p>
						Click <em>Install on GitHub</em> to install this App on a GitHub user account or
						organization. GitHub will redirect back here when done.
					</p>
					<p class="mt-2 text-zinc-500">
						After installing, select the installation from the
						<em>GitHub App authentication</em> section on any stack's Settings tab to replace its
						PAT with a short-lived App token.
					</p>
				</div>
			{:else}
				<table class="w-full text-sm">
					<thead>
						<tr class="text-left text-xs text-zinc-500 uppercase tracking-widest">
							<th class="pb-2 font-medium">Account</th>
							<th class="pb-2 font-medium">Type</th>
							<th class="pb-2 font-medium">Installation ID</th>
							<th class="pb-2 font-medium">Installed</th>
							<th class="pb-2"></th>
						</tr>
					</thead>
					<tbody>
						{#each app.installations as inst (inst.id)}
							<tr class="border-t border-zinc-800">
								<td class="py-2 text-zinc-200">{inst.account_login || '—'}</td>
								<td class="py-2 text-zinc-400">{inst.account_type}</td>
								<td class="py-2 text-zinc-400 font-mono text-xs">{inst.installation_id}</td>
								<td class="py-2 text-zinc-400">{new Date(inst.created_at).toLocaleDateString()}</td>
								<td class="py-2 text-right">
									<button
										onclick={() => deleteInstallation(inst.id, inst.account_login || 'this account')}
										class="text-xs text-red-400 hover:text-red-300"
									>
										Remove
									</button>
								</td>
							</tr>
						{/each}
					</tbody>
				</table>
			{/if}
		</div>
	{/if}

	{#if showForm}
		<div class="rounded-xl border border-zinc-800 bg-zinc-900/50 p-6 space-y-5">
			<div>
				<h2 class="text-base font-medium text-white">
					{app ? 'Replace GitHub App credentials' : 'Register GitHub App'}
				</h2>
				{#if !app}
					<p class="text-xs text-zinc-500 mt-1">
						All values are available on your GitHub App's <em>General</em> settings page at
						<code class="text-zinc-300 bg-zinc-800 px-1 rounded">github.com/settings/apps/{'{your-slug}'}</code>.
					</p>
				{/if}
			</div>

			{#if formError}
				<div class="rounded-lg bg-red-950 border border-red-900 px-4 py-2 text-sm text-red-300">
					{formError}
				</div>
			{/if}

			<div class="grid grid-cols-2 gap-4">
				<div class="space-y-1">
					<label class="block text-sm text-zinc-400" for="f-app-id">App ID</label>
					<input
						id="f-app-id"
						bind:value={appID}
						placeholder="123456"
						class="w-full bg-zinc-800 border border-zinc-700 rounded-lg px-3 py-1.5 text-sm text-white placeholder-zinc-500 focus:outline-none focus:border-teal-500"
					/>
					<p class="text-xs text-zinc-500">Numeric ID shown near the top of the App's General settings page.</p>
				</div>

				<div class="space-y-1">
					<label class="block text-sm text-zinc-400" for="f-slug">Slug</label>
					<input
						id="f-slug"
						bind:value={slug}
						placeholder="my-crucible-app"
						class="w-full bg-zinc-800 border border-zinc-700 rounded-lg px-3 py-1.5 text-sm text-white placeholder-zinc-500 focus:outline-none focus:border-teal-500"
					/>
					<p class="text-xs text-zinc-500">URL-safe name from the App's URL: <code class="text-zinc-400">github.com/apps/{'{slug}'}</code></p>
				</div>

				<div class="col-span-2 space-y-1">
					<label class="block text-sm text-zinc-400" for="f-name">Name</label>
					<input
						id="f-name"
						bind:value={name}
						placeholder="My Crucible App"
						class="w-full bg-zinc-800 border border-zinc-700 rounded-lg px-3 py-1.5 text-sm text-white placeholder-zinc-500 focus:outline-none focus:border-teal-500"
					/>
					<p class="text-xs text-zinc-500">Display name — the <em>GitHub App name</em> field from the General settings page.</p>
				</div>

				<div class="col-span-2 space-y-1">
					<label class="block text-sm text-zinc-400" for="f-client-id">Client ID</label>
					<input
						id="f-client-id"
						bind:value={clientID}
						placeholder="Iv1.xxxxxxxxxxxxxxxx"
						class="w-full bg-zinc-800 border border-zinc-700 rounded-lg px-3 py-1.5 text-sm text-white placeholder-zinc-500 focus:outline-none focus:border-teal-500 font-mono"
					/>
					<p class="text-xs text-zinc-500">Labeled <em>Client ID</em> on the General settings page — different from the numeric App ID.</p>
				</div>

				<div class="col-span-2 space-y-1">
					<label class="block text-sm text-zinc-400" for="f-client-secret">Client Secret</label>
					<input
						id="f-client-secret"
						type="password"
						bind:value={clientSecret}
						placeholder="(write-only — never shown after save)"
						class="w-full bg-zinc-800 border border-zinc-700 rounded-lg px-3 py-1.5 text-sm text-white placeholder-zinc-500 focus:outline-none focus:border-teal-500 font-mono"
					/>
					<p class="text-xs text-zinc-500">Generate under <em>Client secrets</em> on the General settings page. Shown only once — copy it immediately.</p>
				</div>

				<div class="col-span-2 space-y-1">
					<label class="block text-sm text-zinc-400" for="f-webhook-secret">Webhook Secret</label>
					<input
						id="f-webhook-secret"
						type="password"
						bind:value={webhookSecret}
						placeholder="(write-only — never shown after save)"
						class="w-full bg-zinc-800 border border-zinc-700 rounded-lg px-3 py-1.5 text-sm text-white placeholder-zinc-500 focus:outline-none focus:border-teal-500 font-mono"
					/>
					<p class="text-xs text-zinc-500">Any random string — must match the Webhook secret field in your GitHub App settings. Generate one with <code class="text-zinc-400">openssl rand -hex 32</code>.</p>
				</div>

				<div class="col-span-2 space-y-1">
					<label class="block text-sm text-zinc-400" for="f-private-key">Private Key (PEM)</label>
					<textarea
						id="f-private-key"
						bind:value={privateKey}
						placeholder="-----BEGIN RSA PRIVATE KEY-----&#10;...&#10;-----END RSA PRIVATE KEY-----"
						rows="8"
						class="w-full bg-zinc-800 border border-zinc-700 rounded-lg px-3 py-1.5 text-xs text-white placeholder-zinc-500 focus:outline-none focus:border-teal-500 font-mono"
					></textarea>
					<p class="text-xs text-zinc-500">Paste the full contents of the <code class="text-zinc-400">.pem</code> file downloaded from <em>Private keys</em> on the General settings page — including the <code class="text-zinc-400">-----BEGIN / END-----</code> lines.</p>
				</div>
			</div>

			<div class="flex gap-2 justify-end pt-2">
				<button
					onclick={() => (showForm = false)}
					class="text-sm px-4 py-2 rounded-lg bg-zinc-800 hover:bg-zinc-700 text-zinc-200 transition-colors"
				>
					Cancel
				</button>
				<button
					onclick={save}
					disabled={saving}
					class="bg-teal-600 hover:bg-teal-500 disabled:opacity-50 text-white text-sm px-4 py-2 rounded-lg transition-colors"
				>
					{saving ? 'Saving…' : app ? 'Replace' : 'Register'}
				</button>
			</div>
		</div>
	{/if}
</div>
