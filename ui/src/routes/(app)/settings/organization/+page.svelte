<script lang="ts">
	import { onMount } from 'svelte';
	import { auth } from '$lib/stores/auth.svelte';
	import { org, type OrgMember, type OrgInvite, type OrgDetail, type OrgGroupMap } from '$lib/api/client';
	import { orgListStore } from '$lib/stores/orgs.svelte';
	import { decodeJWTPayload } from '$lib/jwt';

	let orgDetail = $state<OrgDetail | null>(null);
	let orgNameDraft = $state('');
	let savingName = $state(false);
	let nameError = $state<string | null>(null);
	let nameSaved = $state(false);

	let members = $state<OrgMember[]>([]);
	let invites = $state<OrgInvite[]>([]);
	let loading = $state(true);
	let error = $state<string | null>(null);

	// Invite form
	let inviteEmail = $state('');
	let inviteRole = $state('member');
	let inviting = $state(false);
	let newToken = $state<string | null>(null);
	let inviteError = $state<string | null>(null);
	let actionError = $state<string | null>(null);

	// SSO group maps
	let groupMaps = $state<OrgGroupMap[]>([]);
	let newGroupClaim = $state('');
	let newGroupRole = $state('member');
	let addingGroupMap = $state(false);
	let groupMapError = $state<string | null>(null);

	const isAdmin = $derived(
		auth.isAdmin || members.find((m) => m.user_id === auth.user?.id)?.role === 'admin'
	);

	onMount(async () => {
		try {
			const [detailRes, membersRes, invitesRes, groupMapsRes] = await Promise.all([
				org.get(),
				org.members.list(),
				org.invites.list(),
				org.groupMaps.list()
			]);
			orgDetail = detailRes;
			orgNameDraft = detailRes.name;
			members = membersRes;
			invites = invitesRes;
			groupMaps = groupMapsRes;
		} catch {
			// non-admins can't list invites or group maps — try without
			try {
				const [detailRes, membersRes] = await Promise.all([org.get(), org.members.list()]);
				orgDetail = detailRes;
				orgNameDraft = detailRes.name;
				members = membersRes;
			} catch (e) {
				error = (e as Error).message;
			}
		} finally {
			loading = false;
		}
	});

	async function saveName(e: Event) {
		e.preventDefault();
		if (!orgNameDraft.trim() || orgNameDraft === orgDetail?.name) return;
		savingName = true;
		nameError = null;
		nameSaved = false;
		try {
			const trimmed = orgNameDraft.trim();
			await org.update(trimmed);
			if (orgDetail) orgDetail = { ...orgDetail, name: trimmed };
			// Keep the sidebar org switcher in sync without a full reload.
			try {
				const orgID = decodeJWTPayload(auth.accessToken!).org as string;
				orgListStore.updateName(orgID, trimmed);
			} catch {}
			nameSaved = true;
			setTimeout(() => { nameSaved = false; }, 2500);
		} catch (err) {
			nameError = (err as Error).message;
		} finally {
			savingName = false;
		}
	}

	async function sendInvite(e: Event) {
		e.preventDefault();
		inviting = true;
		inviteError = null;
		newToken = null;
		try {
			const inv = await org.invites.create(inviteEmail, inviteRole);
			newToken = inv.token ?? null;
			invites = await org.invites.list();
			inviteEmail = '';
		} catch (err) {
			inviteError = (err as Error).message;
		} finally {
			inviting = false;
		}
	}

	async function revokeInvite(id: string) {
		await org.invites.revoke(id);
		invites = invites.filter((i) => i.id !== id);
	}

	async function removeMember(userID: string) {
		actionError = null;
		try {
			await org.members.remove(userID);
			members = members.filter((m) => m.user_id !== userID);
		} catch (err) {
			actionError = (err as Error).message;
		}
	}

	async function changeRole(userID: string, role: string) {
		actionError = null;
		try {
			await org.members.update(userID, role);
			members = members.map((m) => m.user_id === userID ? { ...m, role: role as OrgMember['role'] } : m);
		} catch (err) {
			actionError = (err as Error).message;
		}
	}

	async function addGroupMap(e: Event) {
		e.preventDefault();
		if (!newGroupClaim.trim()) return;
		addingGroupMap = true;
		groupMapError = null;
		try {
			const gm = await org.groupMaps.create(newGroupClaim.trim(), newGroupRole);
			groupMaps = [...groupMaps.filter((m) => m.id !== gm.id), gm];
			newGroupClaim = '';
		} catch (err) {
			groupMapError = (err as Error).message;
		} finally {
			addingGroupMap = false;
		}
	}

	async function deleteGroupMap(id: string) {
		await org.groupMaps.delete(id);
		groupMaps = groupMaps.filter((m) => m.id !== id);
	}
</script>

<div class="max-w-2xl space-y-8">
	<h1 class="text-xl font-semibold text-white">Organization</h1>

	<!-- Org name -->
	{#if isAdmin}
		<div class="bg-zinc-900 border border-zinc-800 rounded-xl overflow-hidden">
			<div class="px-6 py-4 border-b border-zinc-800">
				<p class="text-xs text-zinc-500 uppercase tracking-widest">Details</p>
			</div>
			<form onsubmit={saveName} class="px-6 py-4 space-y-3">
				<div class="space-y-1.5">
					<label class="text-xs text-zinc-400" for="org-name">Organization name</label>
					<div class="flex gap-2">
						<input
							id="org-name"
							type="text"
							bind:value={orgNameDraft}
							placeholder="My Organization"
							required
							class="flex-1 bg-zinc-800 border border-zinc-700 text-zinc-100 placeholder-zinc-500 text-sm rounded-lg px-3 py-2 focus:outline-none focus:ring-2 focus:ring-indigo-500"
						/>
						<button
							type="submit"
							disabled={savingName || !orgNameDraft.trim() || orgNameDraft === orgDetail?.name}
							class="bg-indigo-600 hover:bg-indigo-500 disabled:opacity-50 text-white text-sm px-4 py-2 rounded-lg transition-colors"
						>
							{savingName ? 'Saving…' : 'Save'}
						</button>
					</div>
					{#if orgDetail}
						<p class="text-xs text-zinc-600">Slug: <span class="font-mono">{orgDetail.slug}</span></p>
					{/if}
				</div>
				{#if nameError}
					<p class="text-xs text-red-400">{nameError}</p>
				{/if}
				{#if nameSaved}
					<p class="text-xs text-emerald-400">Saved.</p>
				{/if}
			</form>
		</div>
	{/if}

	<!-- Members list -->
	<div class="bg-zinc-900 border border-zinc-800 rounded-xl overflow-hidden">
		<div class="px-6 py-4 border-b border-zinc-800">
			<p class="text-xs text-zinc-500 uppercase tracking-widest">Members</p>
		</div>

		{#if actionError}
			<p class="px-6 py-4 text-sm text-red-400">{actionError}</p>
		{/if}
		{#if loading}
			<p class="px-6 py-4 text-sm text-zinc-500">Loading…</p>
		{:else if error}
			<p class="px-6 py-4 text-sm text-red-400">{error}</p>
		{:else}
			<table class="w-full text-sm">
				<tbody class="divide-y divide-zinc-800">
					{#each members as member (member.user_id)}
						<tr class="hover:bg-zinc-800/40 transition-colors">
							<td class="px-6 py-3">
								<p class="text-zinc-100">{member.name}</p>
								<p class="text-xs text-zinc-500">{member.email}</p>
							</td>
							<td class="px-6 py-3">
								{#if isAdmin && member.user_id !== auth.user?.id}
									<select
										value={member.role}
										onchange={(e) => changeRole(member.user_id, (e.target as HTMLSelectElement).value)}
										class="bg-zinc-800 border border-zinc-700 text-zinc-300 text-xs rounded px-2 py-1"
									>
										<option value="viewer">viewer</option>
										<option value="member">member</option>
										<option value="admin">admin</option>
									</select>
								{:else}
									<span class="text-xs text-zinc-500">{member.role}</span>
								{/if}
							</td>
							<td class="px-6 py-3 text-right">
								{#if isAdmin && member.user_id !== auth.user?.id}
									<button
										onclick={() => removeMember(member.user_id)}
										class="text-xs text-red-400 hover:text-red-300"
									>Remove</button>
								{/if}
							</td>
						</tr>
					{/each}
				</tbody>
			</table>
		{/if}
	</div>

	<!-- Invites (admins only) -->
	{#if isAdmin}
		<div class="bg-zinc-900 border border-zinc-800 rounded-xl overflow-hidden">
			<div class="px-6 py-4 border-b border-zinc-800">
				<p class="text-xs text-zinc-500 uppercase tracking-widest">Invite member</p>
			</div>
			<div class="px-6 py-4 space-y-3">
				<form onsubmit={sendInvite} class="flex gap-2">
					<input
						type="email"
						bind:value={inviteEmail}
						placeholder="email@example.com"
						required
						class="flex-1 bg-zinc-800 border border-zinc-700 text-zinc-100 placeholder-zinc-500 text-sm rounded-lg px-3 py-2 focus:outline-none focus:ring-2 focus:ring-indigo-500"
					/>
					<select
						bind:value={inviteRole}
						class="bg-zinc-800 border border-zinc-700 text-zinc-300 text-sm rounded-lg px-2 py-2"
					>
						<option value="viewer">viewer</option>
						<option value="member">member</option>
						<option value="admin">admin</option>
					</select>
					<button
						type="submit"
						disabled={inviting}
						class="bg-indigo-600 hover:bg-indigo-500 disabled:opacity-50 text-white text-sm px-4 py-2 rounded-lg transition-colors"
					>
						{inviting ? 'Sending…' : 'Invite'}
					</button>
				</form>

				{#if inviteError}
					<p class="text-xs text-red-400">{inviteError}</p>
				{/if}

				{#if newToken}
					<div class="bg-zinc-800 border border-zinc-700 rounded-lg px-4 py-3 space-y-1">
						<p class="text-xs text-zinc-400">Share this invite link — it expires in 7 days and can only be used once:</p>
						<p class="text-xs font-mono text-indigo-300 break-all">
							{window.location.origin}/invite/{newToken}
						</p>
					</div>
				{/if}
			</div>

			{#if invites.length > 0}
				<div class="border-t border-zinc-800">
					<table class="w-full text-sm">
						<tbody class="divide-y divide-zinc-800">
							{#each invites as invite (invite.id)}
								<tr class="hover:bg-zinc-800/40 transition-colors">
									<td class="px-6 py-3 text-zinc-300">{invite.email}</td>
									<td class="px-6 py-3 text-xs text-zinc-500">{invite.role}</td>
									<td class="px-6 py-3 text-xs text-zinc-600">
										expires {new Date(invite.expires_at).toLocaleDateString()}
									</td>
									<td class="px-6 py-3 text-right">
										<button
											onclick={() => revokeInvite(invite.id)}
											class="text-xs text-red-400 hover:text-red-300"
										>Revoke</button>
									</td>
								</tr>
							{/each}
						</tbody>
					</table>
				</div>
			{/if}
		</div>
	{/if}

	<!-- SSO Group Mapping (admins only, only meaningful when OIDC is configured) -->
	{#if isAdmin}
		<div class="bg-zinc-900 border border-zinc-800 rounded-xl overflow-hidden">
			<div class="px-6 py-4 border-b border-zinc-800">
				<p class="text-xs text-zinc-500 uppercase tracking-widest">SSO Group Mapping</p>
				<p class="text-xs text-zinc-600 mt-1">Map IdP group claims to org roles. Applied automatically on each login.</p>
			</div>
			<div class="px-6 py-4 space-y-3">
				<form onsubmit={addGroupMap} class="flex gap-2">
					<input
						type="text"
						bind:value={newGroupClaim}
						placeholder="idp-group-name"
						required
						class="flex-1 bg-zinc-800 border border-zinc-700 text-zinc-100 placeholder-zinc-500 text-sm rounded-lg px-3 py-2 font-mono focus:outline-none focus:ring-2 focus:ring-indigo-500"
					/>
					<select
						bind:value={newGroupRole}
						class="bg-zinc-800 border border-zinc-700 text-zinc-300 text-sm rounded-lg px-2 py-2"
					>
						<option value="viewer">viewer</option>
						<option value="member">member</option>
						<option value="admin">admin</option>
					</select>
					<button
						type="submit"
						disabled={addingGroupMap}
						class="bg-indigo-600 hover:bg-indigo-500 disabled:opacity-50 text-white text-sm px-4 py-2 rounded-lg transition-colors"
					>
						{addingGroupMap ? 'Adding…' : 'Add'}
					</button>
				</form>
				{#if groupMapError}
					<p class="text-xs text-red-400">{groupMapError}</p>
				{/if}
			</div>

			{#if groupMaps.length > 0}
				<div class="border-t border-zinc-800">
					<table class="w-full text-sm">
						<tbody class="divide-y divide-zinc-800">
							{#each groupMaps as gm (gm.id)}
								<tr class="hover:bg-zinc-800/40 transition-colors">
									<td class="px-6 py-3 font-mono text-zinc-300 text-xs">{gm.group_claim}</td>
									<td class="px-6 py-3 text-xs text-zinc-500">{gm.role}</td>
									<td class="px-6 py-3 text-right">
										<button
											onclick={() => deleteGroupMap(gm.id)}
											class="text-xs text-red-400 hover:text-red-300"
										>Remove</button>
									</td>
								</tr>
							{/each}
						</tbody>
					</table>
				</div>
			{/if}
		</div>
	{/if}
</div>
