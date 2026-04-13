<script lang="ts">
	import '../app.css';
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { auth, type OrgRole } from '$lib/stores/auth.svelte';
	import { org } from '$lib/api/client';
	import { page } from '$app/state';

	const { children } = $props();

	let mounted = $state(false);

	const isAuthRoute = $derived(
		page.url.pathname.startsWith('/login') || page.url.pathname.startsWith('/auth')
	);

	onMount(() => {
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
	});

	function navClass(prefix: string) {
		return 'nav-link' + (page.url.pathname.startsWith(prefix) ? ' active' : '');
	}
</script>

{#if isAuthRoute}
	{@render children()}
{:else if !mounted || auth.loading}
	<div class="flex h-screen items-center justify-center bg-[#1a2e2a]">
		<span class="text-zinc-400 text-sm">Loading…</span>
	</div>
{:else}
	<div class="flex h-screen overflow-hidden">
		<!-- Sidebar -->
		<aside class="w-56 flex-shrink-0 border-r border-zinc-800 bg-zinc-900 flex flex-col">
			<div class="px-4 py-4 border-b border-zinc-800 flex items-center gap-2.5">
				<img src="/mark-dark.png" alt="" class="h-7 w-7 flex-shrink-0" />
				<div class="flex flex-col leading-none">
					<span class="font-semibold text-white tracking-tight text-sm">Crucible</span>
					<span class="text-[10px] text-zinc-500 uppercase tracking-widest">IAP</span>
				</div>
			</div>
			<nav class="flex-1 px-2 py-4 space-y-1 text-sm">
				<a href="/dashboard" class={navClass('/dashboard')}>Dashboard</a>
				<a href="/stacks" class={navClass('/stacks')}>Stacks</a>
				<a href="/runs" class={navClass('/runs')}>Runs</a>
				<a href="/policies" class={navClass('/policies')}>Policies</a>
				<a href="/variable-sets" class={navClass('/variable-sets')}>Variable Sets</a>
				<a href="/stack-templates" class={navClass('/stack-templates')}>Templates</a>
				<a href="/audit" class={navClass('/audit')}>Audit Log</a>
				<a href="/settings" class={navClass('/settings')}>Settings</a>
			</nav>
			<div class="px-4 py-3 border-t border-zinc-800 text-xs text-zinc-500 truncate" title={auth.user?.email}>
				{auth.user?.email}
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
