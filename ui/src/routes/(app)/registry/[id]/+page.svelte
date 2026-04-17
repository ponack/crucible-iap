<script lang="ts">
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { registry, type RegistryModule } from '$lib/api/client';
	import { auth } from '$lib/stores/auth.svelte';
	import { marked } from 'marked';

	let mod = $state<RegistryModule | null>(null);
	let allVersions = $state<RegistryModule[]>([]);
	let loading = $state(true);
	let error = $state<string | null>(null);
	let yanking = $state(false);
	let copied = $state(false);

	const id = $derived(page.params.id!);

	const usageSnippet = $derived(() => {
		if (!mod) return '';
		const host = typeof window !== 'undefined' ? window.location.hostname : 'crucible.example.com';
		return `module "${mod.name}" {\n  source  = "${host}/${mod.namespace}/${mod.name}/${mod.provider}"\n  version = "~> ${mod.version}"\n}`;
	});

	const readmeHtml = $derived(() => {
		if (!mod?.readme) return '';
		return marked.parse(mod.readme) as string;
	});

	onMount(async () => {
		try {
			mod = await registry.get(id);
			const all = await registry.list();
			allVersions = all.filter(
				(m) => m.namespace === mod!.namespace && m.name === mod!.name && m.provider === mod!.provider
			);
		} catch (e) {
			error = (e as Error).message;
		} finally {
			loading = false;
		}
	});

	async function yank() {
		if (!mod || !confirm(`Yank v${mod.version}? Existing references will stop downloading.`)) return;
		yanking = true;
		try {
			await registry.yank(mod.id);
			goto('/registry');
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
	{:else if mod}
		<!-- Header -->
		<div class="flex items-start justify-between gap-4">
			<div>
				<div class="flex items-center gap-2 flex-wrap">
					<a href="/registry" class="text-zinc-400 hover:text-white text-sm">Registry</a>
					<span class="text-zinc-600">/</span>
					<h1 class="text-xl font-semibold text-white">
						{mod.namespace}/{mod.name}
					</h1>
					<span class="px-2 py-0.5 bg-zinc-700 text-zinc-300 text-sm rounded">{mod.provider}</span>
					<span class="px-2 py-0.5 bg-emerald-900/50 text-emerald-400 text-sm rounded font-mono">v{mod.version}</span>
					{#if mod.yanked}
						<span class="px-2 py-0.5 bg-red-900/50 text-red-400 text-xs rounded">yanked</span>
					{/if}
				</div>
				<p class="text-sm text-zinc-500 mt-1">
					Published {new Date(mod.published_at).toLocaleString()}
					{#if mod.published_by}by {mod.published_by}{/if}
					&middot; {mod.download_count} download{mod.download_count === 1 ? '' : 's'}
				</p>
			</div>
			{#if auth.isAdmin && !mod.yanked}
				<button onclick={yank} disabled={yanking}
					class="px-3 py-1.5 border border-red-700 hover:bg-red-900/30 text-red-400 text-sm rounded disabled:opacity-50">
					{yanking ? 'Yanking…' : 'Yank version'}
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

		<!-- README -->
		{#if mod.readme}
			<div class="bg-zinc-900 border border-zinc-700 rounded-lg p-5">
				<h2 class="text-sm font-medium text-zinc-300 mb-3">README</h2>
				<div class="prose prose-invert prose-sm max-w-none text-zinc-300">
					{@html readmeHtml()}
				</div>
			</div>
		{/if}

		<!-- Other versions -->
		{#if allVersions.length > 1}
			<div class="bg-zinc-900 border border-zinc-700 rounded-lg p-4">
				<h2 class="text-sm font-medium text-zinc-300 mb-3">All versions</h2>
				<div class="space-y-1">
					{#each allVersions as v}
						<a href={`/registry/${v.id}`}
							class="flex items-center justify-between px-3 py-2 rounded hover:bg-zinc-800 transition-colors
							       {v.id === mod.id ? 'bg-zinc-800 ring-1 ring-zinc-600' : ''}">
							<span class="font-mono text-sm {v.id === mod.id ? 'text-white' : 'text-zinc-300'}">
								v{v.version}
							</span>
							<div class="flex items-center gap-3">
								{#if v.yanked}
									<span class="text-xs text-red-400">yanked</span>
								{/if}
								<span class="text-xs text-zinc-500">{new Date(v.published_at).toLocaleDateString()}</span>
							</div>
						</a>
					{/each}
				</div>
			</div>
		{/if}
	{/if}
</div>
