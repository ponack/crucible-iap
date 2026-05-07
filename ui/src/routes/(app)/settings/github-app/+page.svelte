<script lang="ts">
	import { onMount } from 'svelte';
	import { githubApp, type GitHubApp } from '$lib/api/client';
	import { toast } from '$lib/stores/toasts.svelte';

	let app = $state<GitHubApp | null>(null);
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

	onMount(async () => {
		try {
			app = await githubApp.get();
		} catch (e) {
			toast.error((e as Error).message);
		} finally {
			loading = false;
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
			app = await githubApp.register({
				app_id: numAppID,
				slug,
				name,
				client_id: clientID,
				client_secret: clientSecret,
				private_key: privateKey,
				webhook_secret: webhookSecret
			});
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
				<li>Generate a private key and copy the App ID, Client ID, Client Secret, and Webhook Secret.</li>
				<li>Paste them below and click Register.</li>
			</ol>
			<button
				onclick={startNew}
				class="bg-teal-600 hover:bg-teal-500 text-white text-sm px-4 py-2 rounded-lg transition-colors"
			>
				Register GitHub App
			</button>
		</div>
	{:else if app && !showForm}
		<div class="rounded-xl border border-zinc-800 bg-zinc-900/50 p-6 space-y-3">
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
