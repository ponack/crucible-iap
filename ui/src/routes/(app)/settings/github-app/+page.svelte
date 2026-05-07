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
			// Strip the query param so a refresh doesn't re-fire the toast
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
			// Refetch the view so we get webhook_url, setup_url, installations
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
			webhook secrets. Short-lived installation tokens are minted automatically; one webhook URL
			covers every connected repo.
		</p>
	</div>

	{#if loading}
		<p class="text-sm text-zinc-500">Loading…</p>
	{:else if !app && !showForm}
		<div class="rounded-xl border border-zinc-800 bg-zinc-900/50 p-6">
			<p class="text-sm text-zinc-300 mb-4">No GitHub App registered.</p>
			<ol class="list-decimal list-inside text-sm text-zinc-400 space-y-1 mb-4">
				<li>
					Create a GitHub App at
					<a
						href="https://github.com/settings/apps/new"
						target="_blank"
						rel="noopener"
						class="text-teal-400 hover:underline">github.com/settings/apps/new</a
					> (or your enterprise instance).
				</li>
				<li>
					Generate a private key and copy the App ID, Client ID, Client Secret, and Webhook
					Secret.
				</li>
				<li>Paste them below and click Register.</li>
				<li>
					After registering, copy the webhook URL and setup callback URL Crucible shows you back
					into your GitHub App settings.
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

		<div class="rounded-xl border border-zinc-800 bg-zinc-900/50 p-6 space-y-4 mb-6">
			<h3 class="text-base font-medium text-white">Wire these URLs into your GitHub App</h3>
			<p class="text-sm text-zinc-400">
				In your app's settings on github.com, set the following two URLs. The webhook URL and setup
				URL are derived from CRUCIBLE_BASE_URL.
			</p>

			<div>
				<label class="block text-xs text-zinc-500 mb-1" for="webhook-url">Webhook URL</label>
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
				<label class="block text-xs text-zinc-500 mb-1" for="setup-url">Setup URL (post-install callback)</label>
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
				<p class="text-xs text-zinc-500 mt-1">
					Enable “Redirect on update” in the GitHub App settings so reinstalls return here.
				</p>
			</div>
		</div>

		<div class="rounded-xl border border-zinc-800 bg-zinc-900/50 p-6">
			<div class="flex items-center justify-between mb-4">
				<h3 class="text-base font-medium text-white">Installations</h3>
				<button
					onclick={startInstall}
					disabled={installing}
					class="bg-teal-600 hover:bg-teal-500 disabled:opacity-50 text-white text-sm px-4 py-2 rounded-lg transition-colors"
				>
					{installing ? 'Redirecting…' : 'Install on GitHub'}
				</button>
			</div>
			{#if app.installations.length === 0}
				<p class="text-sm text-zinc-500">
					No installations yet. Click <em>Install on GitHub</em> to add one. Stacks can pick an
					installation in the next release.
				</p>
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
		<div class="rounded-xl border border-zinc-800 bg-zinc-900/50 p-6 space-y-4">
			<h2 class="text-base font-medium text-white">
				{app ? 'Replace GitHub App credentials' : 'Register GitHub App'}
			</h2>

			{#if formError}
				<div class="rounded-lg bg-red-950 border border-red-900 px-4 py-2 text-sm text-red-300">
					{formError}
				</div>
			{/if}

			<div class="grid grid-cols-2 gap-4">
				<label class="block text-sm">
					<span class="text-zinc-400">App ID</span>
					<input
						bind:value={appID}
						placeholder="123456"
						class="mt-1 w-full bg-zinc-800 border border-zinc-700 rounded-lg px-3 py-1.5 text-sm text-white placeholder-zinc-500 focus:outline-none focus:border-teal-500"
					/>
				</label>
				<label class="block text-sm">
					<span class="text-zinc-400">Slug</span>
					<input
						bind:value={slug}
						placeholder="my-crucible-app"
						class="mt-1 w-full bg-zinc-800 border border-zinc-700 rounded-lg px-3 py-1.5 text-sm text-white placeholder-zinc-500 focus:outline-none focus:border-teal-500"
					/>
				</label>
				<label class="block text-sm col-span-2">
					<span class="text-zinc-400">Name</span>
					<input
						bind:value={name}
						placeholder="My Crucible App"
						class="mt-1 w-full bg-zinc-800 border border-zinc-700 rounded-lg px-3 py-1.5 text-sm text-white placeholder-zinc-500 focus:outline-none focus:border-teal-500"
					/>
				</label>
				<label class="block text-sm col-span-2">
					<span class="text-zinc-400">Client ID</span>
					<input
						bind:value={clientID}
						placeholder="Iv1.xxxxxxxxxxxxxxxx"
						class="mt-1 w-full bg-zinc-800 border border-zinc-700 rounded-lg px-3 py-1.5 text-sm text-white placeholder-zinc-500 focus:outline-none focus:border-teal-500 font-mono"
					/>
				</label>
				<label class="block text-sm col-span-2">
					<span class="text-zinc-400">Client Secret</span>
					<input
						type="password"
						bind:value={clientSecret}
						placeholder="(write-only — never shown after save)"
						class="mt-1 w-full bg-zinc-800 border border-zinc-700 rounded-lg px-3 py-1.5 text-sm text-white placeholder-zinc-500 focus:outline-none focus:border-teal-500 font-mono"
					/>
				</label>
				<label class="block text-sm col-span-2">
					<span class="text-zinc-400">Webhook Secret</span>
					<input
						type="password"
						bind:value={webhookSecret}
						placeholder="(write-only — never shown after save)"
						class="mt-1 w-full bg-zinc-800 border border-zinc-700 rounded-lg px-3 py-1.5 text-sm text-white placeholder-zinc-500 focus:outline-none focus:border-teal-500 font-mono"
					/>
				</label>
				<label class="block text-sm col-span-2">
					<span class="text-zinc-400">Private Key (PEM)</span>
					<textarea
						bind:value={privateKey}
						placeholder="-----BEGIN RSA PRIVATE KEY-----&#10;...&#10;-----END RSA PRIVATE KEY-----"
						rows="8"
						class="mt-1 w-full bg-zinc-800 border border-zinc-700 rounded-lg px-3 py-1.5 text-xs text-white placeholder-zinc-500 focus:outline-none focus:border-teal-500 font-mono"
					></textarea>
				</label>
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
