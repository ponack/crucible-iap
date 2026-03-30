<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { auth } from '$lib/stores/auth.svelte';
	import { decodeJWTPayload } from '$lib/jwt';

	let authConfig = $state({ oidc: false, local: false });
	let email = $state('');
	let password = $state('');
	let error = $state('');
	let loading = $state(false);

	onMount(async () => {
		try {
			const res = await fetch('/auth/config');
			if (res.ok) {
				authConfig = await res.json();
			} else {
				// API unavailable or old image — show both options as a safe fallback
				authConfig = { oidc: true, local: true };
			}
		} catch {
			authConfig = { oidc: true, local: true };
		}
	});

	function loginSSO() {
		window.location.href = '/auth/login';
	}

	async function loginLocal(e: Event) {
		e.preventDefault();
		loading = true;
		error = '';
		try {
			const res = await fetch('/auth/local', {
				method: 'POST',
				headers: { 'Content-Type': 'application/json' },
				body: JSON.stringify({ email, password })
			});
			if (!res.ok) {
				error = 'Invalid email or password.';
				return;
			}
			const { access_token, refresh_token } = await res.json();
			const payload = decodeJWTPayload(access_token);
			auth.setTokens(access_token, refresh_token, {
				id: payload.uid,
				email: payload.email,
				name: payload.name,
				is_admin: false
			});
			goto('/stacks', { replaceState: true });
		} catch {
			error = 'Something went wrong. Please try again.';
		} finally {
			loading = false;
		}
	}
</script>

<div class="min-h-screen w-screen flex items-center justify-center bg-[#1a2e2a]">
	<div class="w-full max-w-sm space-y-8">
		<div class="text-center">
			<img src="/logo-dark.png" alt="Crucible IAP" class="mx-auto h-36 w-auto" />
		</div>

		<div class="bg-zinc-900/80 border border-zinc-700 rounded-xl p-8 space-y-4">
			{#if authConfig.local}
				<form onsubmit={loginLocal} class="space-y-3">
					<div class="space-y-2">
						<input
							type="email"
							bind:value={email}
							placeholder="Email"
							required
							class="w-full bg-zinc-800 border border-zinc-700 text-zinc-100 placeholder-zinc-500 text-sm rounded-lg px-3 py-2.5 focus:outline-none focus:ring-2 focus:ring-indigo-500"
						/>
						<input
							type="password"
							bind:value={password}
							placeholder="Password"
							required
							class="w-full bg-zinc-800 border border-zinc-700 text-zinc-100 placeholder-zinc-500 text-sm rounded-lg px-3 py-2.5 focus:outline-none focus:ring-2 focus:ring-indigo-500"
						/>
					</div>
					{#if error}
						<p class="text-red-400 text-xs">{error}</p>
					{/if}
					<button
						type="submit"
						disabled={loading}
						class="w-full bg-indigo-600 hover:bg-indigo-500 disabled:opacity-50 text-white text-sm font-medium py-2.5 px-4 rounded-lg transition-colors"
					>
						{loading ? 'Signing in…' : 'Sign in'}
					</button>
				</form>
			{/if}

			{#if authConfig.oidc}
				{#if authConfig.local}
					<div class="flex items-center gap-3">
						<div class="flex-1 h-px bg-zinc-700"></div>
						<span class="text-xs text-zinc-500">or</span>
						<div class="flex-1 h-px bg-zinc-700"></div>
					</div>
				{/if}
				<p class="text-sm text-zinc-300 text-center">
					Sign in with your identity provider to continue.
				</p>
				<button
					onclick={loginSSO}
					class="w-full bg-zinc-700 hover:bg-zinc-600 text-white text-sm font-medium py-2.5 px-4 rounded-lg transition-colors"
				>
					Sign in with SSO
				</button>
			{/if}

			{#if !authConfig.oidc && !authConfig.local}
				<p class="text-sm text-zinc-400 text-center">Loading…</p>
			{/if}
		</div>

		<p class="text-center text-xs text-zinc-600">
			Crucible IAP — self-hosted infrastructure automation
		</p>
	</div>
</div>
