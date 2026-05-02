<script lang="ts">
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { providers, type RegistryProvider } from '$lib/api/client';
	import { auth } from '$lib/stores/auth.svelte';
	import { marked } from 'marked';

	let provider = $state<RegistryProvider | null>(null);
	let allPlatforms = $state<RegistryProvider[]>([]);
	let loading = $state(true);
	let error = $state<string | null>(null);
	let yanking = $state(false);
	let copied = $state(false);

	const id = $derived(page.params.id!);

	const usageSnippet = $derived(() => {
		if (!provider) return '';
		const host = typeof window !== 'undefined' ? window.location.hostname : 'crucible.example.com';
		return `terraform {\n  required_providers {\n    ${provider.type} = {\n      source  = "${host}/${provider.namespace}/${provider.type}"\n      version = "~> ${provider.version}"\n    }\n  }\n}`;
	});

	const readmeHtml = $derived(() => {
		if (!provider?.readme) return '';
		return marked.parse(provider.readme) as string;
	});

	onMount(async () => {
		try {
			provider = await providers.get(id);
			const all = await providers.list();
			allPlatforms = all.filter(
				(p) => p.namespace === provider!.namespace && p.type === provider!.type && p.version === provider!.version
			);
		} catch (e) {
			error = (e as Error).message;
		} finally {
			loading = false;
		}
	});

	async function yank() {
		if (!provider || !confirm(`Yank ${provider.namespace}/${provider.type} v${provider.version} (${provider.os}/${provider.arch})?`)) return;
		yanking = true;
		try {
			await providers.yank(provider.id);
			goto('/providers');
		} catch (e) {
			error = (e as Error).message;
			yanking = false;
		}
	}

	async function copySnippet() {
		await navigator.clipboard.writeText(usageSnippet());
		copied = true;
		setTimeout(() => (copied = false), 1500);
	}
</script>

<div class="p-6 max-w-4xl mx-auto space-y-6">
	{#if loading}
		<p class="text-zinc-500 text-sm">Loading…</p>
	{:else if error}
		<p class="text-red-400 text-sm">{error}</p>
	{:else if provider}
		<!-- Header -->
		<div class="flex items-start justify-between gap-4">
			<div>
				<div class="flex items-center gap-2 flex-wrap">
					<a href="/providers" class="text-zinc-400 hover:text-white text-sm">Providers</a>
					<span class="text-zinc-600">/</span>
					<h1 class="text-xl font-semibold text-white">{provider.namespace}/{provider.type}</h1>
					<span class="px-2 py-0.5 bg-emerald-900/50 text-emerald-400 text-sm rounded font-mono">v{provider.version}</span>
					{#if provider.yanked}
						<span class="px-2 py-0.5 bg-red-900/50 text-red-400 text-xs rounded">yanked</span>
					{/if}
				</div>
				<p class="text-sm text-zinc-500 mt-1">
					Published {new Date(provider.published_at).toLocaleString()}
					{#if provider.published_by}by {provider.published_by}{/if}
					&middot; {provider.download_count} download{provider.download_count === 1 ? '' : 's'}
				</p>
			</div>
			{#if auth.isAdmin && !provider.yanked}
				<button onclick={yank} disabled={yanking}
					class="px-3 py-1.5 border border-red-700 hover:bg-red-900/30 text-red-400 text-sm rounded disabled:opacity-50">
					{yanking ? 'Yanking…' : 'Yank'}
				</button>
			{/if}
		</div>

		<!-- Usage snippet -->
		<div class="bg-zinc-900 border border-zinc-700 rounded-lg p-4 space-y-2">
			<div class="flex items-center justify-between">
				<span class="text-xs text-zinc-400 font-medium">Usage</span>
				<button onclick={copySnippet} class="text-xs text-zinc-400 hover:text-white px-2 py-0.5 rounded border border-zinc-700 hover:border-zinc-500">
					{copied ? 'Copied!' : 'Copy'}
				</button>
			</div>
			<pre class="text-sm text-zinc-200 font-mono overflow-x-auto">{usageSnippet()}</pre>
		</div>

		<!-- Platforms for this version -->
		<div class="bg-zinc-900 border border-zinc-700 rounded-lg p-4">
			<h2 class="text-sm font-medium text-zinc-300 mb-3">Platform binaries for v{provider.version}</h2>
			<div class="space-y-1">
				{#each allPlatforms as p}
					<a href={`/providers/${p.id}`}
						class="flex items-center justify-between px-3 py-2 rounded hover:bg-zinc-800 transition-colors
						       {p.id === provider.id ? 'bg-zinc-800 ring-1 ring-zinc-600' : ''}">
						<span class="font-mono text-sm {p.id === provider.id ? 'text-white' : 'text-zinc-300'}">
							{p.os}/{p.arch}
						</span>
						<div class="flex items-center gap-3">
							<span class="font-mono text-xs text-zinc-500">{p.shasum.slice(0, 12)}…</span>
							{#if p.yanked}
								<span class="text-xs text-red-400">yanked</span>
							{/if}
							<span class="text-xs text-zinc-500">{p.download_count} dl</span>
						</div>
					</a>
				{/each}
			</div>
		</div>

		<!-- README -->
		{#if provider.readme}
			<div class="bg-zinc-900 border border-zinc-700 rounded-lg p-5">
				<h2 class="text-sm font-medium text-zinc-300 mb-3">README</h2>
				<div class="prose prose-invert prose-sm max-w-none text-zinc-300">
					{@html readmeHtml()}
				</div>
			</div>
		{/if}
	{/if}
</div>
