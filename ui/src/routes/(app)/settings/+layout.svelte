<script lang="ts">
	import { page } from '$app/state';

	const { children } = $props();

	const navItems = [
		{ label: 'General', href: '/settings' },
		{ label: 'Notifications', href: '/settings/notifications' },
		{ label: 'Organization', href: '/settings/organization' },
		{ label: 'Integrations', href: '/settings/integrations' },
		{ label: 'GitHub App', href: '/settings/github-app' },
		{ label: 'API Tokens', href: '/settings/api-tokens' },
		{ label: 'Tags', href: '/settings/tags' },
		{ label: 'Export / Import', href: '/settings/export' },
		{ label: 'SIEM Streaming', href: '/settings/siem' },
		{ label: 'Resource Quotas', href: '/settings/quotas' },
		{ label: 'BYOK', href: '/settings/byok' }
	];

	const activePath = $derived(page.url.pathname);

	function isActive(href: string) {
		if (href === '/settings') return activePath === '/settings';
		return activePath.startsWith(href);
	}
</script>

<div class="flex min-h-[calc(100vh-4rem)]">
	<!-- Sidebar -->
	<aside class="w-52 shrink-0 border-r border-zinc-800 py-8 px-3">
		<p class="text-xs text-zinc-500 uppercase tracking-widest px-3 mb-3">Settings</p>
		<nav class="space-y-0.5">
			{#each navItems as item}
				{@const active = isActive(item.href)}
				<a
					href={item.href}
					class="flex items-center gap-2 px-3 py-2 rounded-lg text-sm transition-colors relative"
					style={active
						? 'color: var(--accent); background: var(--accent-muted); border-left: 2px solid var(--accent); padding-left: calc(0.75rem - 2px); font-weight: 500;'
						: 'color: var(--color-zinc-400);'}
					class:hover:bg-zinc-800={!active}
					class:hover:text-zinc-100={!active}
				>
					{item.label}
				</a>
			{/each}
		</nav>
	</aside>

	<!-- Page content -->
	<div class="flex-1 min-w-0 p-6">
		{@render children()}
	</div>
</div>