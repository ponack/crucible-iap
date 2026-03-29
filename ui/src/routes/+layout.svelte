<script lang="ts">
	import '../app.css';
	import { auth } from '$lib/stores/auth.svelte';
	import { page } from '$app/state';

	const { children } = $props();

	const isAuthRoute = $derived(page.url.pathname.startsWith('/login'));
</script>

{#if isAuthRoute}
	{@render children()}
{:else if auth.loading}
	<div class="flex h-screen items-center justify-center">
		<span class="text-zinc-400 text-sm">Loading…</span>
	</div>
{:else if !auth.isAuthenticated}
	{@html '<script>window.location.href="/login"</script>'}
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
				<a href="/stacks" class="nav-link">Stacks</a>
				<a href="/runs" class="nav-link">Runs</a>
				<a href="/audit" class="nav-link">Audit Log</a>
				<a href="/settings" class="nav-link">Settings</a>
			</nav>
			<div class="px-4 py-3 border-t border-zinc-800 text-xs text-zinc-500">
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
</style>
