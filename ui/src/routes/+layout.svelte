<script lang="ts">
	import '../app.css';
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { auth, type OrgRole } from '$lib/stores/auth.svelte';
	import { org, tryRefresh } from '$lib/api/client';
	import { orgListStore } from '$lib/stores/orgs.svelte';
	import { decodeJWTPayload } from '$lib/jwt';
	import { page } from '$app/state';

	const { children } = $props();

	let mounted = $state(false);
	const myOrgs = orgListStore;
	let switchingOrg = $state(false);
	let theme = $state<'dark' | 'light'>('dark');

	function applyTheme(t: 'dark' | 'light') {
		theme = t;
		document.documentElement.classList.remove('dark', 'light');
		document.documentElement.classList.add(t);
		localStorage.setItem('theme', t);
	}

	function toggleTheme() {
		applyTheme(theme === 'dark' ? 'light' : 'dark');
	}

	const isAuthRoute = $derived(
		page.url.pathname.startsWith('/login') || page.url.pathname.startsWith('/auth')
	);

	// Current org ID from the JWT — used to mark the active org in the switcher.
	const currentOrgID = $derived(
		auth.accessToken ? (() => { try { return decodeJWTPayload(auth.accessToken!).org as string; } catch { return ''; } })() : ''
	);

	onMount(async () => {
		// Sync theme state from the class already set by the anti-FOUC script.
		theme = document.documentElement.classList.contains('light') ? 'light' : 'dark';
		// Silently restore session from the httpOnly refresh cookie.
		// Must complete before setting mounted so the loading spinner covers the round-trip.
		if (!isAuthRoute && !auth.isAuthenticated) {
			await tryRefresh();
		}
		mounted = true;
	});

	$effect(() => {
		if (mounted && !isAuthRoute && !auth.isAuthenticated) {
			goto('/login', { replaceState: true });
		}
		// Fetch org role once authenticated and not yet known.
		if (mounted && !isAuthRoute && auth.isAuthenticated && !auth.orgRole) {
			org.me().then((r) => auth.setOrgRole(r.role as OrgRole)).catch(() => {});
		}
		// Load orgs for the switcher once authenticated.
		if (mounted && !isAuthRoute && auth.isAuthenticated && myOrgs.list.length === 0) {
			org.list().then((r) => { myOrgs.set(r); }).catch(() => {});
		}
	});

	function navClass(prefix: string) {
		return 'nav-link' + (page.url.pathname.startsWith(prefix) ? ' active' : '');
	}

	async function logout() {
		try { await fetch('/auth/logout', { method: 'POST' }); } catch {}
		auth.clear();
		goto('/login', { replaceState: true });
	}

	async function switchOrg(orgID: string) {
		if (orgID === currentOrgID || switchingOrg) return;
		switchingOrg = true;
		try {
			const { access_token } = await org.switchOrg(orgID);
			const payload = decodeJWTPayload(access_token);
			auth.setTokens(access_token, {
				id: payload.uid,
				email: payload.email,
				name: payload.name,
				is_admin: false
			});
			const switched = myOrgs.list.find(o => o.id === orgID);
			if (switched) auth.setOrgRole(switched.role as OrgRole);
			myOrgs.clear(); // triggers reload on next effect tick
			goto('/stacks', { replaceState: true });
		} catch {
			// silently ignore — user stays on current org
		} finally {
			switchingOrg = false;
		}
	}
</script>

{#if isAuthRoute}
	{@render children()}
{:else if !mounted || auth.loading}
	<div class="flex h-screen items-center justify-center bg-page">
		<span class="text-zinc-400 text-sm">Loading…</span>
	</div>
{:else}
	<div class="flex h-screen overflow-hidden">
		<!-- Sidebar -->
		<aside class="w-56 flex-shrink-0 border-r border-zinc-800 bg-zinc-900 flex flex-col">
			<div class="px-4 py-4 border-b border-zinc-800 flex items-center gap-2.5">
				<img src="/mark.png" alt="" class="h-7 w-7 flex-shrink-0" />
				<div class="flex flex-col leading-none">
					<span class="font-semibold text-white tracking-tight text-sm">Crucible</span>
					<span class="text-[10px] text-zinc-500 uppercase tracking-widest">IAP</span>
				</div>
			</div>

			<!-- Org switcher — only shown when user belongs to more than one org -->
			{#if myOrgs.list.length > 1}
				<div class="px-2 py-2 border-b border-zinc-800">
					<div class="relative">
						<select
							onchange={(e) => switchOrg((e.target as HTMLSelectElement).value)}
							value={currentOrgID}
							disabled={switchingOrg}
							class="w-full bg-zinc-800 border border-zinc-700 text-zinc-200 text-xs rounded-lg px-2.5 py-1.5 pr-7 appearance-none cursor-pointer focus:outline-none focus:ring-1 focus:ring-indigo-500 disabled:opacity-50 truncate"
						>
							{#each myOrgs.list as o (o.id)}
								<option value={o.id}>{o.name}</option>
							{/each}
						</select>
						<div class="pointer-events-none absolute inset-y-0 right-2 flex items-center">
							<svg class="h-3 w-3 text-zinc-500" viewBox="0 0 12 12" fill="none">
								<path d="M3 4.5L6 7.5L9 4.5" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"/>
							</svg>
						</div>
					</div>
				</div>
			{/if}
			<nav class="flex-1 px-2 py-4 space-y-1 text-sm">
				<a href="/dashboard" class={navClass('/dashboard')}>Dashboard</a>
				<a href="/stacks" class={navClass('/stacks')}>Stacks</a>
				<a href="/runs" class={navClass('/runs')}>Runs</a>
				<a href="/policies" class={navClass('/policies')}>Policies</a>
				<a href="/registry" class={navClass('/registry')}>Registry</a>
				<a href="/variable-sets" class={navClass('/variable-sets')}>Variable Sets</a>
				<a href="/stack-templates" class={navClass('/stack-templates')}>Templates</a>
				<a href="/worker-pools" class={navClass('/worker-pools')}>Worker Pools</a>
				<a href="/audit" class={navClass('/audit')}>Audit Log</a>
				<a href="/monitoring" class={navClass('/monitoring')}>Monitoring</a>
				<a href="/settings" class={navClass('/settings')}>Settings</a>
			</nav>
			<div class="px-4 py-3 border-t border-zinc-800 flex items-center gap-2">
				<span class="text-xs text-zinc-500 truncate flex-1" title={auth.user?.email}>{auth.user?.email}</span>
				<button onclick={toggleTheme} title="Toggle theme" class="text-zinc-500 hover:text-zinc-300 flex-shrink-0 transition-colors">
					{#if theme === 'dark'}
						<!-- Sun: switch to light -->
						<svg class="h-3.5 w-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
							<circle cx="12" cy="12" r="4"/><path d="M12 2v2M12 20v2M4.93 4.93l1.41 1.41M17.66 17.66l1.41 1.41M2 12h2M20 12h2M4.93 19.07l1.41-1.41M17.66 6.34l1.41-1.41"/>
						</svg>
					{:else}
						<!-- Moon: switch to dark -->
						<svg class="h-3.5 w-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
							<path d="M21 12.79A9 9 0 1 1 11.21 3 7 7 0 0 0 21 12.79z"/>
						</svg>
					{/if}
				</button>
				<button onclick={logout} class="text-xs text-zinc-500 hover:text-zinc-300 flex-shrink-0 transition-colors">Sign out</button>
			</div>
		</aside>

		<!-- Main content -->
		<main class="flex-1 overflow-auto">
			{@render children()}
		</main>
	</div>
{/if}

<style>
	:global(.nav-link) {
		display: block;
		padding: 0.375rem 0.75rem;
		border-radius: 0.375rem;
		color: var(--color-zinc-400);
		transition: background-color 0.1s, color 0.1s;
	}
	:global(.nav-link:hover) {
		background-color: var(--color-zinc-800);
		color: #fff;
	}
	:global(.nav-link.active) {
		background-color: var(--color-zinc-800);
		color: #fff;
	}
</style>
