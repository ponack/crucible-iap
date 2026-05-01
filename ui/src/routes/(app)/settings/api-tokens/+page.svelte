<script lang="ts">
	import { onMount } from 'svelte';
	import { serviceAccountTokens, type ServiceAccountToken } from '$lib/api/client';

	let tokens = $state<ServiceAccountToken[]>([]);
	let loading = $state(true);
	let error = $state<string | null>(null);

	// Create form
	let creating = $state(false);
	let saving = $state(false);
	let formError = $state<string | null>(null);
	let form = $state({ name: '', role: 'member' });

	// One-time reveal
	let newToken = $state<ServiceAccountToken | null>(null);

	onMount(async () => {
		try {
			tokens = await serviceAccountTokens.list();
		} catch (e) {
			error = (e as Error).message;
		} finally {
			loading = false;
		}
	});

	async function create(e: SubmitEvent) {
		e.preventDefault();
		saving = true;
		formError = null;
		try {
			const t = await serviceAccountTokens.create(form.name, form.role);
			newToken = t;
			tokens = [...tokens, { ...t, token: undefined }];
			form = { name: '', role: 'member' };
			creating = false;
		} catch (e) {
			formError = (e as Error).message;
		} finally {
			saving = false;
		}
	}

	async function revoke(id: string, name: string) {
		if (!confirm(`Revoke token "${name}"? Any automation using it will stop working immediately.`)) return;
		try {
			await serviceAccountTokens.revoke(id);
			tokens = tokens.filter((t) => t.id !== id);
		} catch (e) {
			alert((e as Error).message);
		}
	}
</script>

<div class="max-w-2xl space-y-6">
	<div class="flex items-start justify-between gap-4">
		<div>
			<h1 class="text-base font-semibold text-white">API Tokens</h1>
			<p class="text-sm text-zinc-500 mt-0.5">
				Long-lived tokens for CI pipelines and automation. Tokens are shown once at creation.
				Use them as <code class="text-zinc-300 text-xs">Authorization: Bearer ciap_…</code> headers.
			</p>
		</div>
		<button
			onclick={() => (creating = !creating)}
			class="shrink-0 rounded-lg bg-indigo-600 px-3 py-1.5 text-sm text-white transition-colors hover:bg-indigo-500">
			{creating ? 'Cancel' : 'New token'}
		</button>
	</div>

	{#if newToken}
		<div class="rounded-xl border border-yellow-800 bg-yellow-950 p-4 space-y-3">
			<p class="text-yellow-300 text-sm font-medium">
				Token created — copy it now. You won't be able to see it again.
			</p>
			<div class="flex items-center gap-2">
				<code class="flex-1 text-xs text-yellow-200 break-all font-mono bg-yellow-900/30 rounded px-2 py-1.5">{newToken.token}</code>
				<button
					onclick={() => navigator.clipboard.writeText(newToken!.token!)}
					class="shrink-0 text-xs text-zinc-400 hover:text-zinc-200 border border-zinc-700 hover:border-zinc-500 px-2 py-1 rounded transition-colors">
					Copy
				</button>
			</div>
			<button onclick={() => (newToken = null)} class="text-xs text-yellow-600 hover:text-yellow-400">
				Dismiss
			</button>
		</div>
	{/if}

	{#if creating}
		<div class="rounded-xl border border-zinc-800 p-5 space-y-4">
			<h2 class="text-sm font-medium text-zinc-300">New API token</h2>
			{#if formError}
				<div class="rounded-lg border border-red-800 bg-red-950 px-4 py-3 text-sm text-red-300">{formError}</div>
			{/if}
			<form onsubmit={create} class="space-y-4">
				<div class="grid grid-cols-2 gap-4">
					<div class="space-y-1.5">
						<label class="field-label" for="sat-name">Name</label>
						<input id="sat-name" class="field-input" bind:value={form.name} required placeholder="e.g. github-actions" />
					</div>
					<div class="space-y-1.5">
						<label class="field-label" for="sat-role">Role</label>
						<select id="sat-role" class="field-input" bind:value={form.role}>
							<option value="viewer">Viewer — read-only access</option>
							<option value="member">Member — can trigger runs</option>
							<option value="admin">Admin — full access</option>
						</select>
					</div>
				</div>
				<div class="flex justify-end">
					<button type="submit" disabled={saving}
						class="rounded-lg bg-indigo-600 px-4 py-1.5 text-sm text-white transition-colors hover:bg-indigo-500 disabled:opacity-50">
						{saving ? 'Creating…' : 'Create token'}
					</button>
				</div>
			</form>
		</div>
	{/if}

	{#if loading}
		<p class="text-zinc-500 text-sm">Loading…</p>
	{:else if error}
		<div class="rounded-lg border border-red-800 bg-red-950 px-4 py-3 text-sm text-red-300">{error}</div>
	{:else if tokens.length === 0}
		<div class="rounded-xl border border-zinc-800 p-8 text-center space-y-2">
			<p class="text-zinc-400 text-sm font-medium">No API tokens yet</p>
			<p class="text-zinc-600 text-xs">Create a token to authenticate CI pipelines or automation scripts.</p>
		</div>
	{:else}
		<div class="rounded-xl border border-zinc-800 overflow-hidden">
			<table class="w-full text-sm">
				<thead class="bg-zinc-900 text-zinc-500 text-xs uppercase tracking-wide">
					<tr>
						<th class="text-left px-4 py-2">Name</th>
						<th class="text-left px-4 py-2">Role</th>
						<th class="text-left px-4 py-2">Last used</th>
						<th class="text-left px-4 py-2">Created</th>
						<th class="px-4 py-2"></th>
					</tr>
				</thead>
				<tbody class="divide-y divide-zinc-700">
					{#each tokens as t (t.id)}
						<tr>
							<td class="px-4 py-2.5 text-zinc-200 font-medium">{t.name}</td>
							<td class="px-4 py-2.5">
								<span class="text-xs px-1.5 py-0.5 rounded {t.role === 'admin' ? 'bg-red-900/50 text-red-300' : t.role === 'member' ? 'bg-indigo-900/50 text-indigo-300' : 'bg-zinc-800 text-zinc-400'}">
									{t.role}
								</span>
							</td>
							<td class="px-4 py-2.5 text-zinc-500 text-xs">
								{t.last_used_at ? new Date(t.last_used_at).toLocaleString() : '—'}
							</td>
							<td class="px-4 py-2.5 text-zinc-600 text-xs">{new Date(t.created_at).toLocaleString()}</td>
							<td class="px-4 py-2.5 text-right">
								<button onclick={() => revoke(t.id, t.name)} class="text-xs text-red-500 hover:text-red-300">Revoke</button>
							</td>
						</tr>
					{/each}
				</tbody>
			</table>
		</div>
	{/if}
</div>

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
