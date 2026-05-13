<script lang="ts">
	import { onMount } from 'svelte';
	import { complianceApi, type CatalogEntry } from '$lib/api/client';

	let catalog = $state<CatalogEntry[]>([]);
	let loading = $state(true);
	let error = $state<string | null>(null);
	let working = $state<Record<string, boolean>>({});

	async function load() {
		loading = true;
		error = null;
		try {
			catalog = await complianceApi.getCatalog();
		} catch (e) {
			error = (e as Error).message;
		} finally {
			loading = false;
		}
	}

	onMount(load);

	async function install(slug: string) {
		working = { ...working, [slug]: true };
		try {
			await complianceApi.install(slug);
			await load();
		} catch (e) {
			error = (e as Error).message;
		} finally {
			working = { ...working, [slug]: false };
		}
	}

	async function sync(id: string, slug: string) {
		working = { ...working, [slug]: true };
		try {
			await complianceApi.sync(id);
			await load();
		} catch (e) {
			error = (e as Error).message;
		} finally {
			working = { ...working, [slug]: false };
		}
	}

	async function uninstall(id: string, slug: string) {
		if (!confirm('Uninstall this compliance pack? All attached stacks will stop evaluating its policies.')) return;
		working = { ...working, [slug]: true };
		try {
			await complianceApi.uninstall(id);
			await load();
		} catch (e) {
			error = (e as Error).message;
		} finally {
			working = { ...working, [slug]: false };
		}
	}

	const badgeColor: Record<string, string> = {
		'soc2':    'bg-sky-900/40 text-sky-300 ring-sky-700',
		'cis-aws': 'bg-orange-900/40 text-orange-300 ring-orange-700',
		'hipaa':   'bg-violet-900/40 text-violet-300 ring-violet-700',
		'pci-dss': 'bg-rose-900/40 text-rose-300 ring-rose-700',
	};
</script>

<div class="mx-auto max-w-5xl space-y-6 p-6">
	<div>
		<h1 class="text-xl font-semibold text-zinc-100">Compliance Policy Packs</h1>
		<p class="mt-1 text-sm text-zinc-400">
			Installable OPA policy bundles for common compliance frameworks. Install a pack to make it
			available for attachment to any stack.
		</p>
	</div>

	{#if error}
		<div class="rounded-md bg-red-900/30 px-4 py-3 text-sm text-red-300 ring-1 ring-red-700">{error}</div>
	{/if}

	{#if loading}
		<div class="text-sm text-zinc-500">Loading…</div>
	{:else}
		<div class="grid grid-cols-1 gap-4 sm:grid-cols-2">
			{#each catalog as entry (entry.slug)}
				{@const busy = working[entry.slug] ?? false}
				<div class="flex flex-col rounded-lg bg-zinc-900 ring-1 ring-zinc-800">
					<div class="flex items-start gap-3 p-5">
						<span class="mt-0.5 inline-flex items-center rounded px-2 py-0.5 text-xs font-medium ring-1 {badgeColor[entry.slug] ?? 'bg-zinc-800 text-zinc-300 ring-zinc-700'}">
							{entry.name}
						</span>
						{#if entry.installed}
							<span class="ml-auto inline-flex items-center gap-1 rounded px-2 py-0.5 text-xs font-medium bg-teal-900/40 text-teal-300 ring-1 ring-teal-700">
								<svg class="h-3 w-3" viewBox="0 0 20 20" fill="currentColor">
									<path fill-rule="evenodd" d="M16.704 4.153a.75.75 0 0 1 .143 1.052l-8 10.5a.75.75 0 0 1-1.127.075l-4.5-4.5a.75.75 0 0 1 1.06-1.06l3.894 3.893 7.48-9.817a.75.75 0 0 1 1.05-.143Z" clip-rule="evenodd" />
								</svg>
								Installed v{entry.installed.version}
							</span>
						{/if}
					</div>

					<p class="px-5 pb-4 text-sm text-zinc-400">{entry.description}</p>

					<div class="px-5 pb-4 text-xs text-zinc-500">
						{entry.policy_count} {entry.policy_count === 1 ? 'policy' : 'policies'}
						{#if entry.installed?.last_synced_at}
							· last synced {new Date(entry.installed.last_synced_at).toLocaleDateString()}
						{/if}
					</div>

					<div class="mt-auto flex items-center gap-2 border-t border-zinc-800 px-5 py-3">
						{#if entry.installed}
							<button
								onclick={() => sync(entry.installed!.id, entry.slug)}
								disabled={busy}
								class="rounded px-3 py-1.5 text-xs font-medium text-zinc-300 ring-1 ring-zinc-700 hover:bg-zinc-800 disabled:opacity-50"
							>
								{busy ? 'Syncing…' : 'Sync'}
							</button>
							<button
								onclick={() => uninstall(entry.installed!.id, entry.slug)}
								disabled={busy}
								class="rounded px-3 py-1.5 text-xs font-medium text-red-400 ring-1 ring-red-900 hover:bg-red-950 disabled:opacity-50"
							>
								Uninstall
							</button>
						{:else}
							<button
								onclick={() => install(entry.slug)}
								disabled={busy}
								class="rounded px-3 py-1.5 text-xs font-medium bg-teal-700 text-white hover:bg-teal-600 disabled:opacity-50"
							>
								{busy ? 'Installing…' : 'Install'}
							</button>
						{/if}
					</div>
				</div>
			{/each}
		</div>
	{/if}
</div>
