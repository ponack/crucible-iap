<script lang="ts">
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { adminApi, type AdminOrg, type AdminOrgMember } from '$lib/api/admin';
	import { toast } from '$lib/stores/toasts.svelte';

	const orgID = $derived(page.params['id']!);

	let org = $state<AdminOrg | null>(null);
	let members = $state<AdminOrgMember[]>([]);
	let loading = $state(true);
	let error = $state<string | null>(null);

	// Add member modal
	let showAddMember = $state(false);
	let formEmail = $state('');
	let formRole = $state('member');
	let adding = $state(false);
	let addError = $state<string | null>(null);

	// Archive confirm
	let showArchiveConfirm = $state(false);
	let archiving = $state(false);

	onMount(load);

	async function load() {
		loading = true;
		error = null;
		try {
			[org, members] = await Promise.all([
				adminApi.getOrg(orgID),
				adminApi.listOrgMembers(orgID)
			]);
		} catch (e) {
			error = (e as Error).message;
		} finally {
			loading = false;
		}
	}

	function openAddMember() {
		formEmail = '';
		formRole = 'member';
		addError = null;
		showAddMember = true;
	}

	async function addMember() {
		adding = true;
		addError = null;
		try {
			await adminApi.addOrgMember(orgID, formEmail.trim(), formRole);
			showAddMember = false;
			toast.success(`${formEmail.trim()} added to ${org?.name}.`);
			members = await adminApi.listOrgMembers(orgID);
		} catch (e) {
			addError = (e as Error).message;
		} finally {
			adding = false;
		}
	}

	async function archive() {
		archiving = true;
		try {
			await adminApi.archiveOrg(orgID);
			toast.success(`${org?.name} archived.`);
			goto('/admin/orgs');
		} catch (e) {
			toast.error((e as Error).message);
			archiving = false;
		}
	}

	async function unarchive() {
		try {
			await adminApi.unarchiveOrg(orgID);
			toast.success(`${org?.name} restored.`);
			await load();
		} catch (e) {
			toast.error((e as Error).message);
		}
	}
</script>

<div class="p-6 max-w-3xl">
	<!-- Back link -->
	<a href="/admin/orgs" class="text-xs text-zinc-500 hover:text-zinc-300 flex items-center gap-1 mb-5 transition-colors">
		<svg class="h-3 w-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
			<path d="M15 18l-6-6 6-6"/>
		</svg>
		Organizations
	</a>

	{#if loading}
		<p class="text-zinc-500 text-sm">Loading…</p>
	{:else if error}
		<p class="text-red-400 text-sm">{error}</p>
	{:else if org}
		<div class="mb-6 flex items-start justify-between">
			<div>
				<div class="flex items-center gap-2 mb-0.5">
					<h1 class="text-xl font-semibold text-white">{org.name}</h1>
					{#if org.archived_at}
						<span class="text-xs bg-amber-950 text-amber-400 border border-amber-800 rounded px-2 py-0.5">Archived</span>
					{/if}
				</div>
				<p class="text-xs text-zinc-500 font-mono">{org.slug}</p>
				<p class="text-xs text-zinc-600 mt-1">Created {new Date(org.created_at).toLocaleDateString()}</p>
			</div>
			<div class="flex items-center gap-2">
				{#if org.archived_at}
					<button onclick={unarchive}
						class="px-3 py-1.5 text-sm bg-zinc-800 hover:bg-zinc-700 text-teal-400 rounded-lg transition-colors">
						Restore
					</button>
				{:else}
					<button onclick={() => (showArchiveConfirm = true)}
						class="px-3 py-1.5 text-sm bg-zinc-800 hover:bg-zinc-700 text-amber-400 rounded-lg transition-colors">
						Archive
					</button>
				{/if}
			</div>
		</div>

		<!-- Members -->
		<div class="mb-4 flex items-center justify-between">
			<h2 class="text-sm font-medium text-zinc-300">Members <span class="text-zinc-600">({members.length})</span></h2>
			{#if !org.archived_at}
				<button onclick={openAddMember}
					class="px-3 py-1 text-xs bg-zinc-800 hover:bg-zinc-700 text-zinc-200 rounded-lg transition-colors">
					Add member
				</button>
			{/if}
		</div>

		{#if members.length === 0}
			<p class="text-zinc-500 text-sm">No members yet.</p>
		{:else}
			<div class="border border-zinc-800 rounded-xl overflow-hidden">
				<table class="w-full text-sm">
					<thead>
						<tr class="border-b border-zinc-800">
							<th class="px-4 py-3 text-left text-xs text-zinc-500 font-medium">User</th>
							<th class="px-4 py-3 text-left text-xs text-zinc-500 font-medium">Role</th>
							<th class="px-4 py-3 text-left text-xs text-zinc-500 font-medium">Joined</th>
						</tr>
					</thead>
					<tbody>
						{#each members as m (m.user_id)}
							<tr class="border-b border-zinc-800/50 last:border-0">
								<td class="px-4 py-3">
									<div class="text-white">{m.name}</div>
									<div class="text-xs text-zinc-500">{m.email}</div>
								</td>
								<td class="px-4 py-3">
									<span class="text-xs rounded px-2 py-0.5
										{m.role === 'admin' ? 'bg-teal-950 text-teal-400 border border-teal-800'
										: m.role === 'member' ? 'bg-zinc-800 text-zinc-300'
										: 'bg-zinc-800 text-zinc-500'}">
										{m.role}
									</span>
								</td>
								<td class="px-4 py-3 text-xs text-zinc-500">{new Date(m.joined_at).toLocaleDateString()}</td>
							</tr>
						{/each}
					</tbody>
				</table>
			</div>
		{/if}
	{/if}
</div>

<!-- Add member modal -->
{#if showAddMember}
	<div class="fixed inset-0 bg-black/60 flex items-center justify-center z-50 p-4">
		<div class="bg-zinc-900 border border-zinc-700 rounded-xl w-full max-w-sm shadow-2xl">
			<div class="px-6 py-4 border-b border-zinc-800">
				<h2 class="text-white font-semibold">Add member</h2>
			</div>
			<div class="px-6 py-4 space-y-4">
				{#if addError}
					<p class="text-red-400 text-sm bg-red-950 border border-red-800 rounded px-3 py-2">{addError}</p>
				{/if}
				<div>
					<label class="block text-xs text-zinc-400 mb-1" for="add-email">Email</label>
					<input id="add-email" type="email" bind:value={formEmail}
						placeholder="user@example.com"
						class="w-full bg-zinc-800 border border-zinc-700 rounded-lg px-3 py-2 text-sm text-white placeholder-zinc-500 focus:outline-none focus:border-teal-500" />
					<p class="text-xs text-zinc-600 mt-1">User must already exist in the system.</p>
				</div>
				<div>
					<label class="block text-xs text-zinc-400 mb-1" for="add-role">Role</label>
					<select id="add-role" bind:value={formRole}
						class="w-full bg-zinc-800 border border-zinc-700 rounded-lg px-3 py-2 text-sm text-white focus:outline-none focus:border-teal-500">
						<option value="admin">Admin</option>
						<option value="member">Member</option>
						<option value="viewer">Viewer</option>
					</select>
				</div>
			</div>
			<div class="px-6 py-4 border-t border-zinc-800 flex justify-end gap-2">
				<button onclick={() => (showAddMember = false)} disabled={adding}
					class="px-3 py-1.5 text-sm text-zinc-400 hover:text-white transition-colors">Cancel</button>
				<button onclick={addMember} disabled={!formEmail.trim() || adding}
					class="px-4 py-1.5 text-sm bg-teal-600 hover:bg-teal-500 disabled:opacity-50 text-white rounded-lg transition-colors">
					{adding ? 'Adding…' : 'Add'}
				</button>
			</div>
		</div>
	</div>
{/if}

<!-- Archive confirm -->
{#if showArchiveConfirm}
	<div class="fixed inset-0 bg-black/60 flex items-center justify-center z-50 p-4">
		<div class="bg-zinc-900 border border-zinc-700 rounded-xl w-full max-w-sm shadow-2xl">
			<div class="px-6 py-4 border-b border-zinc-800">
				<h2 class="text-white font-semibold">Archive organization</h2>
			</div>
			<div class="px-6 py-4 space-y-2">
				<p class="text-sm text-zinc-300">
					This hides <span class="text-white font-medium">{org?.name}</span> from all member dashboards
					and prevents login into it. Members are not removed — the org can be restored later.
				</p>
				<p class="text-xs text-amber-400">
					Any in-progress runs will complete. No new runs can be triggered while archived.
				</p>
			</div>
			<div class="px-6 py-4 border-t border-zinc-800 flex justify-end gap-2">
				<button onclick={() => (showArchiveConfirm = false)} disabled={archiving}
					class="px-3 py-1.5 text-sm text-zinc-400 hover:text-white transition-colors">Cancel</button>
				<button onclick={archive} disabled={archiving}
					class="px-4 py-1.5 text-sm bg-amber-600 hover:bg-amber-500 disabled:opacity-50 text-white rounded-lg transition-colors">
					{archiving ? 'Archiving…' : 'Archive'}
				</button>
			</div>
		</div>
	</div>
{/if}
