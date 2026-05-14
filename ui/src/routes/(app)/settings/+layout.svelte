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
		{ label: 'SIEM Streaming', href: '/settings/siem' }
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
				<a
					href={item.href}
					class="flex items-center gap-2 px-3 py-2 rounded-lg text-sm transition-colors
						{isActive(item.href)
							? 'bg-zinc-800 text-white font-medium'
							: 'text-zinc-400 hover:text-zinc-100 hover:bg-zinc-800/50'}"
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