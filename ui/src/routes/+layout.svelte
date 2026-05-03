<script lang="ts">
	import '../app.css';
	import Toasts from '$lib/components/Toasts.svelte';
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { auth, type OrgRole } from '$lib/stores/auth.svelte';
	import { org, tryRefresh, system } from '$lib/api/client';
	import { orgListStore } from '$lib/stores/orgs.svelte';
	import { decodeJWTPayload } from '$lib/jwt';
	import { page } from '$app/state';

	const { children } = $props();

	let mounted = $state(false);
	const myOrgs = orgListStore;
	let switchingOrg = $state(false);
	let theme = $state<'dark' | 'light'>('dark');
	let forge = $state<'cold' | 'hot' | 'neutral'>('cold');
	let appVersion = $state('');

	function applyTheme(t: 'dark' | 'light') {
		theme = t;
		document.documentElement.classList.remove('dark', 'light');
		document.documentElement.classList.add(t);
		localStorage.setItem('theme', t);
	}

	function toggleTheme() {
		applyTheme(theme === 'dark' ? 'light' : 'dark');
	}

	function switchForge(f: 'cold' | 'hot' | 'neutral') {
		forge = f;
		document.documentElement.classList.remove('hot-forge', 'neutral-forge');
		if (f === 'hot') document.documentElement.classList.add('hot-forge');
		if (f === 'neutral') document.documentElement.classList.add('neutral-forge');
		localStorage.setItem('forge', f === 'cold' ? '' : f);
	}

	const isAuthRoute = $derived(
		page.url.pathname.startsWith('/login') || page.url.pathname.startsWith('/auth')
	);

	const currentOrgID = $derived(
		auth.accessToken ? (() => { try { return decodeJWTPayload(auth.accessToken!).org as string; } catch { return ''; } })() : ''
	);

	onMount(async () => {
		theme = document.documentElement.classList.contains('light') ? 'light' : 'dark';
		forge = document.documentElement.classList.contains('hot-forge') ? 'hot'
			: document.documentElement.classList.contains('neutral-forge') ? 'neutral'
			: 'cold';
		system.health().then((h) => { appVersion = h.version; }).catch(() => {});
		if (!isAuthRoute && !auth.isAuthenticated) {
			await tryRefresh();
		}
		mounted = true;
	});

	$effect(() => {
		if (mounted && !isAuthRoute && !auth.isAuthenticated) {
			goto('/login', { replaceState: true });
		}
		if (mounted && !isAuthRoute && auth.isAuthenticated && !auth.orgRole) {
			org.me().then((r) => auth.setOrgRole(r.role as OrgRole)).catch(() => {});
		}
		if (mounted && !isAuthRoute && auth.isAuthenticated && myOrgs.list.length === 0) {
			org.list().then((r) => { myOrgs.set(r); }).catch(() => {});
		}
	});

	function isActive(prefix: string) {
		return page.url.pathname.startsWith(prefix);
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
			myOrgs.clear();
			goto('/stacks', { replaceState: true });
		} catch {
			// stay on current org
		} finally {
			switchingOrg = false;
		}
	}

	const navSections = [
		{
			items: [
				{
					href: '/dashboard',
					label: 'Dashboard',
					path: 'M3.75 6A2.25 2.25 0 0 1 6 3.75h2.25A2.25 2.25 0 0 1 10.5 6v2.25a2.25 2.25 0 0 1-2.25 2.25H6a2.25 2.25 0 0 1-2.25-2.25V6ZM3.75 15.75A2.25 2.25 0 0 1 6 13.5h2.25a2.25 2.25 0 0 1 2.25 2.25V18a2.25 2.25 0 0 1-2.25 2.25H6A2.25 2.25 0 0 1 3.75 18v-2.25ZM13.5 6a2.25 2.25 0 0 1 2.25-2.25H18A2.25 2.25 0 0 1 20.25 6v2.25A2.25 2.25 0 0 1 18 10.5h-2.25a2.25 2.25 0 0 1-2.25-2.25V6ZM13.5 15.75a2.25 2.25 0 0 1 2.25-2.25H18a2.25 2.25 0 0 1 2.25 2.25V18A2.25 2.25 0 0 1 18 20.25h-2.25A2.25 2.25 0 0 1 13.5 18v-2.25Z'
				},
				{
					href: '/stacks',
					label: 'Stacks',
					path: 'M6.429 9.75 2.25 12l4.179 2.25m0-4.5 5.571 3 5.571-3m-11.142 0L2.25 7.5 12 2.25l9.75 5.25-4.179 2.25m0 0L21.75 12l-4.179 2.25m0 0 4.179 2.25L12 21.75 2.25 16.5l4.179-2.25m11.142 0-5.571 3-5.571-3'
				},
				{
					href: '/runs',
					label: 'Runs',
					path: 'M5.25 5.653c0-.856.917-1.398 1.667-.986l11.54 6.347a1.125 1.125 0 0 1 0 1.972l-11.54 6.347a1.125 1.125 0 0 1-1.667-.986V5.653Z'
				}
			]
		},
		{
			label: 'Config',
			items: [
				{
					href: '/policies',
					label: 'Policies',
					path: 'M9 12.75 11.25 15 15 9.75m-3-7.036A11.959 11.959 0 0 1 3.598 6 11.99 11.99 0 0 0 3 9.749c0 5.592 3.824 10.29 9 11.623 5.176-1.332 9-6.03 9-11.622 0-1.31-.21-2.571-.598-3.751h-.152c-3.196 0-6.1-1.248-8.25-3.285Z'
				},
				{
					href: '/registry',
					label: 'Registry',
					path: 'M21 7.5l-9-5.25L3 7.5m18 0-9 5.25m9-5.25v9l-9 5.25M3 7.5l9 5.25M3 7.5v9l9 5.25m0-9v9'
				},
				{
					href: '/providers',
					label: 'Providers',
					path: 'M14.25 6.087c0-.355.186-.676.401-.959.221-.29.349-.634.349-1.003 0-1.036-1.007-1.875-2.25-1.875s-2.25.84-2.25 1.875c0 .369.128.713.349 1.003.215.283.401.604.401.959v0a.64.64 0 0 1-.657.643 48.39 48.39 0 0 1-4.163-.3c.186 1.613.293 3.25.315 4.907a.656.656 0 0 1-.658.663v0c-.355 0-.676-.186-.959-.401a1.647 1.647 0 0 0-1.003-.349c-1.035 0-1.875 1.007-1.875 2.25s.84 2.25 1.875 2.25c.369 0 .713-.128 1.003-.349.283-.215.604-.401.959-.401v0c.31 0 .555.26.532.57a48.039 48.039 0 0 1-.642 5.056c1.518.19 3.058.309 4.616.354a.64.64 0 0 0 .657-.643v0c0-.355-.186-.676-.401-.959a1.647 1.647 0 0 1-.349-1.003c0-1.035 1.007-1.875 2.25-1.875 1.243 0 2.25.84 2.25 1.875 0 .369-.128.713-.349 1.003-.215.283-.401.604-.401.959v0c0 .333.277.599.61.58a48.1 48.1 0 0 0 5.427-.63 48.05 48.05 0 0 0 .582-4.717.532.532 0 0 0-.533-.57v0c-.355 0-.676.186-.959.401-.29.221-.634.349-1.003.349-1.035 0-1.875-1.007-1.875-2.25s.84-2.25 1.875-2.25c.37 0 .713.128 1.003.349.283.215.604.401.959.401v0a.656.656 0 0 0 .658-.663 48.422 48.422 0 0 0-.37-5.36c-1.886.342-3.81.574-5.766.689a.578.578 0 0 1-.61-.58v0Z'
				},
				{
					href: '/variable-sets',
					label: 'Variable Sets',
					path: 'M10.5 6h9.75M10.5 6a1.5 1.5 0 1 1-3 0m3 0a1.5 1.5 0 1 0-3 0M3.75 6H7.5m3 12h9.75m-9.75 0a1.5 1.5 0 0 1-3 0m3 0a1.5 1.5 0 0 0-3 0m-3.75 0H7.5m9-6h3.75m-3.75 0a1.5 1.5 0 0 1-3 0m3 0a1.5 1.5 0 0 0-3 0m-9.75 0h9.75'
				},
				{
					href: '/blueprints',
					label: 'Blueprints',
					path: 'M9 6.75V15m6-6v8.25m.503 3.498 4.875-2.437c.381-.19.622-.58.622-1.006V4.82c0-.836-.88-1.38-1.628-1.006l-3.869 1.934c-.317.159-.69.159-1.006 0L9.503 3.252a1.125 1.125 0 0 0-1.006 0L3.622 5.689C3.24 5.88 3 6.27 3 6.695V19.18c0 .836.88 1.38 1.628 1.006l3.869-1.934c.317-.159.69-.159 1.006 0l4.994 2.497c.317.158.69.158 1.006 0Z'
				},
				{
					href: '/stack-templates',
					label: 'Templates',
					path: 'M15.75 17.25v3.375c0 .621-.504 1.125-1.125 1.125h-9.75a1.125 1.125 0 0 1-1.125-1.125V7.875c0-.621.504-1.125 1.125-1.125H6.75a9.06 9.06 0 0 1 1.5.124m7.5 10.376h3.375c.621 0 1.125-.504 1.125-1.125V11.25c0-4.46-3.243-8.161-7.5-8.876a9.06 9.06 0 0 0-1.5-.124H9.375c-.621 0-1.125.504-1.125 1.125v3.5m7.5 10.375H9.375a1.125 1.125 0 0 1-1.125-1.125v-9.25m12 6.625v-1.875a3.375 3.375 0 0 0-3.375-3.375h-1.5a1.125 1.125 0 0 1-1.125-1.125v-1.5a3.375 3.375 0 0 0-3.375-3.375H9.75'
				}
			]
		},
		{
			label: 'Ops',
			items: [
				{
					href: '/worker-pools',
					label: 'Worker Pools',
					path: 'M5.25 14.25h13.5m-13.5 0a3 3 0 0 1-3-3m3 3a3 3 0 1 0 6 0m-6 0H3m16.5 0H21m-1.5 0a3 3 0 0 0 3-3m-3 3a3 3 0 1 1-6 0m6 0h1.5m-7.5 0H12m0 0a3 3 0 0 1-3-3m3 3a3 3 0 0 0 3-3m-3 0V3m0 11.25'
				},
				{
					href: '/audit',
					label: 'Audit Log',
					path: 'M9 12h3.75M9 15h3.75M9 18h3.75m3 .75H18a2.25 2.25 0 0 0 2.25-2.25V6.108c0-1.135-.845-2.098-1.976-2.192a48.424 48.424 0 0 0-1.123-.08m-5.801 0c-.065.21-.1.433-.1.664 0 .414.336.75.75.75h4.5a.75.75 0 0 0 .75-.75 2.25 2.25 0 0 0-.1-.664m-5.8 0A2.251 2.251 0 0 1 13.5 2.25H15c1.012 0 1.867.668 2.15 1.586m-5.8 0c-.376.023-.75.05-1.124.08C9.095 4.01 8.25 4.973 8.25 6.108V8.25m0 0H4.875c-.621 0-1.125.504-1.125 1.125v11.25c0 .621.504 1.125 1.125 1.125h9.75c.621 0 1.125-.504 1.125-1.125V9.375c0-.621-.504-1.125-1.125-1.125H8.25ZM6.75 12h.008v.008H6.75V12Zm0 3h.008v.008H6.75V15Zm0 3h.008v.008H6.75V18Z'
				},
				{
					href: '/monitoring',
					label: 'Monitoring',
					path: 'M3 13.125C3 12.504 3.504 12 4.125 12h2.25c.621 0 1.125.504 1.125 1.125v6.75C7.5 20.496 6.996 21 6.375 21h-2.25A1.125 1.125 0 0 1 3 19.875v-6.75ZM9.75 8.625c0-.621.504-1.125 1.125-1.125h2.25c.621 0 1.125.504 1.125 1.125v11.25c0 .621-.504 1.125-1.125 1.125h-2.25a1.125 1.125 0 0 1-1.125-1.125V8.625ZM16.5 4.125c0-.621.504-1.125 1.125-1.125h2.25C20.496 3 21 3.504 21 4.125v15.75c0 .621-.504 1.125-1.125 1.125h-2.25a1.125 1.125 0 0 1-1.125-1.125V4.125Z'
				},
				{
					href: '/settings',
					label: 'Settings',
					path: 'M9.594 3.94c.09-.542.56-.94 1.11-.94h2.593c.55 0 1.02.398 1.11.94l.213 1.281c.063.374.313.686.645.87.074.04.147.083.22.127.325.196.72.257 1.075.124l1.217-.456a1.125 1.125 0 0 1 1.37.49l1.296 2.247a1.125 1.125 0 0 1-.26 1.431l-1.003.827c-.293.241-.438.613-.43.992a7.723 7.723 0 0 1 0 .255c-.008.378.137.75.43.991l1.004.827c.424.35.534.955.26 1.43l-1.298 2.247a1.125 1.125 0 0 1-1.369.491l-1.217-.456c-.355-.133-.75-.072-1.076.124a6.47 6.47 0 0 1-.22.128c-.331.183-.581.495-.644.869l-.213 1.281c-.09.543-.56.94-1.11.94h-2.594c-.55 0-1.019-.398-1.11-.94l-.213-1.281c-.062-.374-.312-.686-.644-.87a6.52 6.52 0 0 1-.22-.127c-.325-.196-.72-.257-1.076-.124l-1.217.456a1.125 1.125 0 0 1-1.369-.49l-1.297-2.247a1.125 1.125 0 0 1 .26-1.431l1.004-.827c.292-.24.437-.613.43-.991a6.932 6.932 0 0 1 0-.255c.007-.38-.138-.751-.43-.992l-1.004-.827a1.125 1.125 0 0 1-.26-1.43l1.297-2.247a1.125 1.125 0 0 1 1.37-.491l1.216.456c.356.133.751.072 1.076-.124.072-.044.146-.086.22-.128.332-.183.582-.495.644-.869l.214-1.28Z M15 12a3 3 0 1 1-6 0 3 3 0 0 1 6 0Z'
				}
			]
		}
	];
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
		<aside class="w-56 flex-shrink-0 flex flex-col" style="background: var(--color-zinc-900); border-right: 1px solid var(--color-zinc-800);">

			<!-- Logo + controls -->
			<div class="px-4 py-4 flex items-center gap-3" style="border-bottom: 1px solid var(--color-zinc-800);">
				<img src="/mark.png" alt="" class="h-10 w-10 flex-shrink-0" />
				<div class="flex flex-col leading-none">
					<span class="font-semibold text-white tracking-tight text-sm">Crucible</span>
					<span class="text-[10px] text-zinc-500 uppercase tracking-widest">IAP</span>
				</div>
				<div class="ml-auto flex items-center gap-2">
					{#if appVersion}
						<span class="text-[10px] text-zinc-600">{appVersion}</span>
					{/if}
					<button onclick={toggleTheme} title="Toggle theme" class="text-zinc-500 hover:text-zinc-300 transition-colors">
						{#if theme === 'dark'}
							<svg class="h-3.5 w-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
								<circle cx="12" cy="12" r="4"/><path d="M12 2v2M12 20v2M4.93 4.93l1.41 1.41M17.66 17.66l1.41 1.41M2 12h2M20 12h2M4.93 19.07l1.41-1.41M17.66 6.34l1.41-1.41"/>
							</svg>
						{:else}
							<svg class="h-3.5 w-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
								<path d="M21 12.79A9 9 0 1 1 11.21 3 7 7 0 0 0 21 12.79z"/>
							</svg>
						{/if}
					</button>
				</div>
			</div>

			<!-- Org switcher -->
			{#if myOrgs.list.length > 1}
				<div class="px-2 py-2" style="border-bottom: 1px solid var(--color-zinc-800);">
					<div class="relative">
						<select
							onchange={(e) => switchOrg((e.target as HTMLSelectElement).value)}
							value={currentOrgID}
							disabled={switchingOrg}
							class="w-full text-zinc-200 text-xs rounded-lg px-2.5 py-1.5 pr-7 appearance-none cursor-pointer focus:outline-none disabled:opacity-50 truncate"
							style="background: var(--color-zinc-800); border: 1px solid var(--color-zinc-700);"
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

			<!-- Nav -->
			<nav class="flex-1 overflow-y-auto px-2 py-3 space-y-5">
				{#each navSections as section}
					<div>
						{#if section.label}
							<p class="px-3 mb-1 text-[10px] font-medium uppercase tracking-widest text-zinc-600">{section.label}</p>
						{/if}
						<ul class="space-y-0.5">
							{#each section.items as item}
								{@const active = isActive(item.href)}
								<li>
									<a
										href={item.href}
										class="flex items-center gap-2.5 px-3 py-2 rounded-lg text-sm transition-colors duration-100 relative"
										style={active
											? 'color: var(--accent); background: var(--accent-muted); border-left: 2px solid var(--accent); padding-left: calc(0.75rem - 2px);'
											: 'color: var(--color-zinc-400);'}
										class:hover:bg-zinc-800={!active}
										class:hover:text-zinc-200={!active}
									>
										<svg class="h-4 w-4 flex-shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">
											<path d={item.path}/>
										</svg>
										{item.label}
									</a>
								</li>
							{/each}
						</ul>
					</div>
				{/each}
			</nav>

			<!-- Forge theme switcher -->
			<div class="px-3 py-2.5 flex items-center gap-1.5" style="border-top: 1px solid var(--color-zinc-800);">
				<span class="text-[10px] text-zinc-600 uppercase tracking-widest mr-1">Forge</span>
				<button
					onclick={() => switchForge('cold')}
					title="Cold Forge"
					class="flex items-center gap-1.5 px-2 py-1 rounded-md text-[11px] font-medium transition-colors"
					style={forge === 'cold'
						? 'background: var(--accent-muted); color: var(--accent); border: 1px solid var(--accent-border);'
						: 'color: var(--color-zinc-500); border: 1px solid transparent;'}
				>
					<span class="h-2 w-2 rounded-full flex-shrink-0" style="background: #2DD4BF;"></span>
					Cold
				</button>
				<button
					onclick={() => switchForge('hot')}
					title="Hot Forge"
					class="flex items-center gap-1.5 px-2 py-1 rounded-md text-[11px] font-medium transition-colors"
					style={forge === 'hot'
						? 'background: rgba(212,136,60,0.08); color: #D4883C; border: 1px solid rgba(212,136,60,0.18);'
						: 'color: var(--color-zinc-500); border: 1px solid transparent;'}
				>
					<span class="h-2 w-2 rounded-full flex-shrink-0" style="background: #D4883C;"></span>
					Hot
				</button>
				<button
					onclick={() => switchForge('neutral')}
					title="Neutral Forge"
					class="flex items-center gap-1.5 px-2 py-1 rounded-md text-[11px] font-medium transition-colors"
					style={forge === 'neutral'
						? 'background: rgba(129,140,248,0.08); color: #818cf8; border: 1px solid rgba(129,140,248,0.18);'
						: 'color: var(--color-zinc-500); border: 1px solid transparent;'}
				>
					<span class="h-2 w-2 rounded-full flex-shrink-0" style="background: #818cf8;"></span>
					Neutral
				</button>
			</div>

			<!-- Footer -->
			<div class="px-4 py-3 flex items-center gap-2" style="border-top: 1px solid var(--color-zinc-800);">
				<span class="text-xs text-zinc-500 truncate flex-1" title={auth.user?.email}>{auth.user?.email}</span>
				<button onclick={logout} class="text-xs text-zinc-500 hover:text-zinc-300 flex-shrink-0 transition-colors">Sign out</button>
			</div>
		</aside>

		<!-- Main content -->
		<main class="flex-1 overflow-auto">
			{@render children()}
		</main>
	</div>
	<Toasts />
{/if}
