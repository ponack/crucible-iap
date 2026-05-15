<script lang="ts">
	import { onMount } from 'svelte';
	import { page } from '$app/stores';
	import { goto } from '$app/navigation';
	import { auth, type OrgRole } from '$lib/stores/auth.svelte';
	import { org } from '$lib/api/client';
	import { decodeJWTPayload } from '$lib/jwt';

	let token = $derived($page.params.token!);

	interface InviteMeta {
		org_name: string;
		email: string;
		role: string;
	}

	let meta = $state<InviteMeta | null>(null);
	let notFound = $state(false);
	let accepting = $state(false);
	let error = $state<string | null>(null);

	onMount(async () => {
		try {
			const res = await fetch(`/api/v1/invites/${token}`);
			if (!res.ok) {
				notFound = true;
				return;
			}
			meta = await res.json();
		} catch {
			notFound = true;
		}
	});

	async function accept() {
		if (!auth.isAuthenticated) {
			// Redirect to login, return here after
			goto(`/login?next=/invite/${token}`);
			return;
		}
		accepting = true;
		error = null;
		try {
			const { org_id, role } = await org.invites.accept(token);
			// Switch the active session to the newly joined org.
			const { access_token } = await org.switchOrg(org_id);
			const payload = decodeJWTPayload(access_token);
			auth.setTokens(access_token, {
				id: payload.uid,
				email: payload.email,
				name: payload.name,
				is_admin: false,
				is_instance_admin: payload.iadm ?? false
			});
			auth.setOrgRole(role as OrgRole);
			goto('/stacks', { replaceState: true });
		} catch (err) {
			error = (err as Error).message;
			accepting = false;
		}
	}
</script>

<div class="min-h-screen bg-zinc-950 flex items-center justify-center p-4">
	<div class="w-full max-w-sm space-y-6">
		<div class="text-center">
			<img src="/logo.png" alt="Crucible" class="h-8 mx-auto mb-6" />
		</div>

		{#if notFound}
			<div class="bg-zinc-900 border border-zinc-800 rounded-xl px-6 py-8 text-center space-y-3">
				<p class="text-zinc-100 font-medium">Invite not found</p>
				<p class="text-sm text-zinc-500">This invite link has expired, already been used, or is invalid.</p>
				<a href="/" class="block mt-4 text-sm text-teal-400 hover:text-teal-300">Go home</a>
			</div>
		{:else if meta}
			<div class="bg-zinc-900 border border-zinc-800 rounded-xl px-6 py-8 space-y-5">
				<div class="text-center space-y-1">
					<p class="text-zinc-100 font-medium">You're invited to join</p>
					<p class="text-xl font-semibold text-white">{meta.org_name}</p>
					<p class="text-sm text-zinc-500">as <span class="text-zinc-300">{meta.role}</span></p>
				</div>

				{#if meta.email}
					<p class="text-xs text-zinc-500 text-center">Invite sent to {meta.email}</p>
				{/if}

				{#if error}
					<p class="text-xs text-red-400 text-center">{error}</p>
				{/if}

				<button
					onclick={accept}
					disabled={accepting}
					class="w-full bg-teal-600 hover:bg-teal-500 disabled:opacity-50 text-white font-medium py-2.5 rounded-lg transition-colors"
				>
					{#if accepting}
						Joining…
					{:else if !auth.isAuthenticated}
						Sign in to accept
					{:else}
						Accept invite
					{/if}
				</button>
			</div>
		{:else}
			<div class="text-center">
				<p class="text-sm text-zinc-500">Loading…</p>
			</div>
		{/if}
	</div>
</div>
