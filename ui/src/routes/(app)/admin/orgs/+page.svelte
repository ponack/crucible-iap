<script lang="ts">
	import { onMount } from 'svelte';
	import { adminApi, type AdminOrg } from '$lib/api/admin';
	import { toast } from '$lib/stores/toasts.svelte';

	let orgs = $state<AdminOrg[]>([]);
	let loading = $state(true);
	let tab = $state<'active' | 'archived'>('active');

	// Create modal
	let showCreate = $state(false);
	let formName = $state('');
	let formSlug = $state('');
	let formAdminEmail = $state('');
	let creating = $state(false);
	let createError = $state<string | null>(null);

	// Archive/unarchive in-flight
	let actingID = $state<string | null>(null);

	onMount(load);

	async function load() {
		loading = true;
		try {
			orgs = await adminApi.listOrgs(tab === 'archived');
		} catch (e) {
			toast.error((e as Error).message);
		} finally {
			loading = false;
		}
	}

	async function switchTab(t: 'active' | 'archived') {
		tab = t;
		await load();
	}

	function slugify(name: string) {
		return name.toLowerCase().replace(/\s+/g, '-').replace(/[^a-z0-9-]/g, '').replace(/^-+|-+$/g, '');
	}

	function openCreate() {
		formName = '';
		formSlug = '';
		formAdminEmail = '';
		createError = null;
		showCreate = true;
	}

	async function create() {
		creating = true;
		createError = null;
		try {
			await adminApi.createOrg(formName.trim(), formSlug.trim(), formAdminEmail.trim() || undefined);
			showCreate = false;
			toast.success(`Organization "${formName.trim()}" created.`);
			await load();
		} catch (e) {
			createError = (e as Error).message;
		} finally {
			creating = false;
		}
	}

	async function archive(org: AdminOrg) {
		actingID = org.id;
		try {
			await adminApi.archiveOrg(org.id);
			toast.success(`${org.name} archived.`);
			await load();
		} catch (e) {
			toast.error((e as Error).message);
		} finally {
			actingID = null;
		}
	}

	async function unarchive(org: AdminOrg) {
		actingID = org.id;
		try {
			await adminApi.unarchiveOrg(org.id);
			toast.success(`${org.name} unarchived.`);
			await load();
		} catch (e) {
			toast.error((e as Error).message);
		} finally {
			actingID = null;
		}
	}
</script>

<div class="p-6 max-w-4xl">
	<div class="mb-6 flex items-center justify-between">
		<div>
			<h1 class="text-xl font-semibold text-white">Organizations</h1>
			<p class="text-sm text-zinc-400 mt-0.5">Instance-wide organization management.</p>
		</div>
		{#if tab === 'active'}
			<button onclick={openCreate}
				class="px-3 py-1.5 text-sm bg-teal-600 hover:bg-teal-500 text-white rounded-lg transition-colors">
				New organization
			</button>
		{/if}
	</div>

	<!-- Tabs -->
	<div class="flex gap-1 mb-4 border-b border-zinc-800">
		{#each (['active', 'archived'] as const) as t}
			<button onclick={() => switchTab(t)}
				class="px-4 py-2 text-sm font-medium transition-colors border-b-2 -mb-px
					{tab === t ? 'border-teal-500 text-teal-400' : 'border-transparent text-zinc-500 hover:text-zinc-300'}">
				{t === 'active' ? 'Active' : 'Archived'}
			</button>
		{/each}
	</div>

	{#if loading}
		<p class="text-zinc-500 text-sm">Loading…</p>
	{:else if orgs.length === 0}
		<p class="text-zinc-500 text-sm">{tab === 'active' ? 'No organizations yet.' : 'No archived organizations.'}</p>
	{:else}
		<div class="border border-zinc-800 rounded-xl overflow-hidden">
			<table class="w-full text-sm">
				<thead>
					<tr class="border-b border-zinc-800">
						<th class="px-4 py-3 text-left text-xs text-zinc-500 font-medium">Name</th>
						<th class="px-4 py-3 text-left text-xs text-zinc-500 font-medium">Slug</th>
						<th class="px-4 py-3 text-left text-xs text-zinc-500 font-medium">Members</th>
						<th class="px-4 py-3 text-left text-xs text-zinc-500 font-medium">Created</th>
						<th class="px-4 py-3"></th>
					</tr>
				</thead>
				<tbody>
					{#each orgs as o (o.id)}
						<tr class="border-b border-zinc-800/50 last:border-0 hover:bg-zinc-800/30 transition-colors">
							<td class="px-4 py-3">
								<a href="/admin/orgs/{o.id}" class="text-white hover:text-teal-400 font-medium transition-colors">{o.name}</a>
							</td>
							<td class="px-4 py-3 text-zinc-400 font-mono text-xs">{o.slug}</td>
							<td class="px-4 py-3 text-zinc-400">{o.member_count}</td>
							<td class="px-4 py-3 text-zinc-500 text-xs">{new Date(o.created_at).toLocaleDateString()}</td>
							<td class="px-4 py-3 text-right">
								{#if tab === 'active'}
									<button onclick={() => archive(o)} disabled={actingID === o.id}
										class="text-xs text-zinc-500 hover:text-amber-400 disabled:opacity-50 transition-colors">
										{actingID === o.id ? 'Archiving…' : 'Archive'}
									</button>
								{:else}
									<button onclick={() => unarchive(o)} disabled={actingID === o.id}
										class="text-xs text-zinc-500 hover:text-teal-400 disabled:opacity-50 transition-colors">
										{actingID === o.id ? 'Restoring…' : 'Restore'}
									</button>
								{/if}
							</td>
						</tr>
					{/each}
				</tbody>
			</table>
		</div>
	{/if}
</div>

<!-- Create modal -->
{#if showCreate}
	<div class="fixed inset-0 bg-black/60 flex items-center justify-center z-50 p-4">
		<div class="bg-zinc-900 border border-zinc-700 rounded-xl w-full max-w-md shadow-2xl">
			<div class="px-6 py-4 border-b border-zinc-800">
				<h2 class="text-white font-semibold">New organization</h2>
			</div>
			<div class="px-6 py-4 space-y-4">
				{#if createError}
					<p class="text-red-400 text-sm bg-red-950 border border-red-800 rounded px-3 py-2">{createError}</p>
				{/if}
				<div>
					<label class="block text-xs text-zinc-400 mb-1" for="org-name">Name</label>
					<input id="org-name" type="text" bind:value={formName}
						oninput={() => { if (!formSlug || formSlug === slugify(formName.slice(0, -1))) formSlug = slugify(formName); }}
						placeholder="Acme Corp"
						class="w-full bg-zinc-800 border border-zinc-700 rounded-lg px-3 py-2 text-sm text-white placeholder-zinc-500 focus:outline-none focus:border-teal-500" />
				</div>
				<div>
					<label class="block text-xs text-zinc-400 mb-1" for="org-slug">Slug</label>
					<input id="org-slug" type="text" bind:value={formSlug}
						placeholder="acme-corp"
						class="w-full bg-zinc-800 border border-zinc-700 rounded-lg px-3 py-2 text-sm text-white placeholder-zinc-500 font-mono focus:outline-none focus:border-teal-500" />
					<p class="text-xs text-zinc-600 mt-1">Lowercase, alphanumeric and hyphens only.</p>
				</div>
				<div>
					<label class="block text-xs text-zinc-400 mb-1" for="org-admin-email">First admin email <span class="text-zinc-600">(optional)</span></label>
					<input id="org-admin-email" type="email" bind:value={formAdminEmail}
						placeholder="admin@acme.com"
						class="w-full bg-zinc-800 border border-zinc-700 rounded-lg px-3 py-2 text-sm text-white placeholder-zinc-500 focus:outline-none focus:border-teal-500" />
					<p class="text-xs text-zinc-600 mt-1">User must already exist in the system.</p>
				</div>
			</div>
			<div class="px-6 py-4 border-t border-zinc-800 flex justify-end gap-2">
				<button onclick={() => (showCreate = false)} disabled={creating}
					class="px-3 py-1.5 text-sm text-zinc-400 hover:text-white transition-colors">Cancel</button>
				<button onclick={create} disabled={!formName.trim() || !formSlug.trim() || creating}
					class="px-4 py-1.5 text-sm bg-teal-600 hover:bg-teal-500 disabled:opacity-50 text-white rounded-lg transition-colors">
					{creating ? 'Creating…' : 'Create'}
				</button>
			</div>
		</div>
	</div>
{/if}
