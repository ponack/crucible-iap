<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { auth } from '$lib/stores/auth.svelte';
	import { decodeJWTPayload } from '$lib/jwt';

	let error = $state<string | null>(null);

	onMount(() => {
		const params = new URLSearchParams(window.location.hash.slice(1));
		const accessToken = params.get('access_token');

		if (!accessToken) {
			error = 'No access token received. Please try signing in again.';
			return;
		}

		try {
			const payload = decodeJWTPayload(accessToken);
			auth.setTokens(accessToken, {
				id: payload.uid,
				email: payload.email,
				name: payload.name,
				is_admin: false
			});
			goto('/stacks', { replaceState: true });
		} catch {
			error = 'Failed to parse auth token. Please try again.';
		}
	});
</script>

<div class="flex h-screen items-center justify-center bg-zinc-950">
	{#if error}
		<div class="text-center space-y-3">
			<p class="text-red-400 text-sm">{error}</p>
			<a href="/login" class="text-teal-400 text-sm hover:underline">Back to login</a>
		</div>
	{:else}
		<p class="text-zinc-400 text-sm">Signing in…</p>
	{/if}
</div>
