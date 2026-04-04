<script lang="ts">
	import { onMount } from 'svelte';
	import { auth } from '$lib/stores/auth.svelte';
	import { org, system, type OrgMember, type OrgInvite, type HealthStatus } from '$lib/api/client';

	let members = $state<OrgMember[]>([]);
	let invites = $state<OrgInvite[]>([]);
	let loading = $state(true);
	let error = $state<string | null>(null);
	let health = $state<HealthStatus | null>(null);

	// Invite form
	let inviteEmail = $state('');
	let inviteRole = $state('member');
	let inviting = $state(false);
	let newToken = $state<string | null>(null);
	let inviteError = $state<string | null>(null);

	const isAdmin = $derived(
		auth.isAdmin || members.find((m) => m.user_id === auth.user?.id)?.role === 'admin'
	);

	onMount(async () => {
		try {
			members = await org.members.list();
			invites = await org.invites.list();
		} catch {
			// non-admin users can't list invites — members list is fine
			try { members = await org.members.list(); } catch {}
		} finally {
			loading = false;
		}
		system.health().then((h) => (health = h)).catch(() => {});
	});

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
		await org.members.remove(userID);
		members = members.filter((m) => m.user_id !== userID);
	}

	async function changeRole(userID: string, role: string) {
		await org.members.update(userID, role);
		members = members.map((m) => m.user_id === userID ? { ...m, role: role as OrgMember['role'] } : m);
	}
</script>

<div class="p-8 max-w-2xl space-y-8">
	<h1 class="text-xl font-semibold text-white">Settings</h1>

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

	<!-- Members -->
	<div class="bg-zinc-900 border border-zinc-800 rounded-xl overflow-hidden">
		<div class="px-6 py-4 border-b border-zinc-800">
			<p class="text-xs text-zinc-500 uppercase tracking-widest">Members</p>
		</div>

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

	<!-- Instance info -->
	{#if health}
		<div class="bg-zinc-900 border border-zinc-800 rounded-xl divide-y divide-zinc-800">
			<div class="px-6 py-4">
				<p class="text-xs text-zinc-500 uppercase tracking-widest mb-3">Instance</p>
				<dl class="space-y-1.5 text-sm">
					<div class="flex justify-between">
						<dt class="text-zinc-500">Version</dt>
						<dd class="font-mono text-zinc-300">{health.version}</dd>
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
