<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { projects, type Project } from '$lib/api/client';
	import { auth } from '$lib/stores/auth.svelte';
	import EmptyState from '$lib/components/EmptyState.svelte';

	let items = $state<Project[]>([]);
	let loading = $state(true);
	let error = $state<string | null>(null);

	let creating = $state(false);
	let saving = $state(false);
	let formError = $state<string | null>(null);
	let form = $state({ name: '', description: '', slug: '' });

	onMount(async () => {
		try {
			items = await projects.list();
		} catch (e) {
			error = (e as Error).message;
		} finally {
			loading = false;
		}
	});

	async function create(e: SubmitEvent) {
		e.preventDefault();
		saving = true;
		formError = null;
		try {
			const p = await projects.create(form);
			goto(`/projects/${p.id}`);
		} catch (e) {
			formError = (e as Error).message;
			saving = false;
		}
	}

	function cancelCreate() {
		creating = false;
		form = { name: '', description: '', slug: '' };
		formError = null;
	}
</script>

<div class="p-6 space-y-6">
	<div class="flex items-center justify-between">
		<div>
			<h1 class="text-lg font-semibold text-white">Projects</h1>
			<p class="text-sm text-zinc-500 mt-0.5">Group stacks by product, team, or domain with per-project access control.</p>
		</div>
		{#if auth.orgRole === 'admin' || auth.orgRole === 'member'}
			<button
				onclick={() => { creating = !creating; if (!creating) cancelCreate(); }}
				class="rounded-lg bg-teal-600 px-3 py-1.5 text-sm text-white transition-colors hover:bg-teal-500">
				{creating ? 'Cancel' : 'New project'}
			</button>
		{/if}
	</div>

	{#if creating}
		<div class="space-y-4 rounded-xl border border-zinc-800 p-5">
			<h2 class="text-sm font-medium text-zinc-300">New project</h2>
			{#if formError}
				<div class="rounded-lg border border-red-800 bg-red-950 px-4 py-3 text-sm text-red-300">{formError}</div>
			{/if}
			<form onsubmit={create} class="space-y-4">
				<div class="grid grid-cols-2 gap-4">
					<div class="space-y-1.5">
						<label class="field-label" for="p-name">Name <span class="text-red-400">*</span></label>
						<input id="p-name" class="field-input" bind:value={form.name} required placeholder="e.g. Platform" />
					</div>
					<div class="space-y-1.5">
						<label class="field-label" for="p-slug">Slug <span class="text-zinc-600 font-normal">(auto-generated if blank)</span></label>
						<input id="p-slug" class="field-input" bind:value={form.slug} placeholder="e.g. platform" />
					</div>
					<div class="col-span-2 space-y-1.5">
						<label class="field-label" for="p-desc">Description</label>
						<input id="p-desc" class="field-input" bind:value={form.description} placeholder="Optional description" />
					</div>
				</div>
				<div class="flex justify-end gap-3">
					<button type="button" onclick={cancelCreate} class="rounded-lg border border-zinc-700 px-4 py-1.5 text-sm text-zinc-300 hover:bg-zinc-800">Cancel</button>
					<button type="submit" disabled={saving} class="rounded-lg bg-teal-600 px-4 py-1.5 text-sm text-white hover:bg-teal-500 disabled:opacity-50">
						{saving ? 'Creating…' : 'Create project'}
					</button>
				</div>
			</form>
		</div>
	{/if}

	{#if loading}
		<div class="flex items-center justify-center py-20">
			<span class="text-zinc-500 text-sm">Loading…</span>
		</div>
	{:else if error}
		<div class="rounded-lg border border-red-800 bg-red-950 px-4 py-3 text-sm text-red-300">{error}</div>
	{:else if items.length === 0}
		<EmptyState
			icon="M2.25 12.75V12A2.25 2.25 0 0 1 4.5 9.75h15A2.25 2.25 0 0 1 21.75 12v.75m-8.69-6.44-2.12-2.12a1.5 1.5 0 0 0-1.061-.44H4.5A2.25 2.25 0 0 0 2.25 6v12a2.25 2.25 0 0 0 2.25 2.25h15A2.25 2.25 0 0 0 21.75 18V9a2.25 2.25 0 0 0-2.25-2.25h-5.379a1.5 1.5 0 0 1-1.06-.44Z"
			heading="No projects yet"
			sub="Projects group stacks by team or domain and let you set per-project access control."
		/>
	{:else}
		<div class="grid gap-4 grid-cols-1 sm:grid-cols-2 xl:grid-cols-3">
			{#each items as p (p.id)}
				<button
					onclick={() => goto(`/projects/${p.id}`)}
					class="text-left rounded-xl border border-zinc-800 bg-zinc-900/50 p-5 hover:border-zinc-700 hover:bg-zinc-800/60 transition-colors space-y-3">
					<div class="flex items-start justify-between gap-3">
						<div class="min-w-0">
							<div class="font-medium text-white truncate">{p.name}</div>
							{#if p.description}
								<div class="text-xs text-zinc-500 mt-0.5 truncate">{p.description}</div>
							{/if}
						</div>
						<span class="shrink-0 text-[10px] font-mono text-zinc-600 bg-zinc-800 rounded px-1.5 py-0.5">{p.slug}</span>
					</div>
					<div class="flex items-center gap-4 text-xs text-zinc-500">
						<span class="flex items-center gap-1">
							<svg class="h-3.5 w-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">
								<path d="M6.429 9.75 2.25 12l4.179 2.25m0-4.5 5.571 3 5.571-3m-11.142 0L2.25 7.5 12 2.25l9.75 5.25-4.179 2.25m0 0L21.75 12l-4.179 2.25m0 0 4.179 2.25L12 21.75 2.25 16.5l4.179-2.25m11.142 0-5.571 3-5.571-3"/>
							</svg>
							{p.stack_count} {p.stack_count === 1 ? 'stack' : 'stacks'}
						</span>
						<span class="flex items-center gap-1">
							<svg class="h-3.5 w-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">
								<path d="M15 19.128a9.38 9.38 0 0 0 2.625.372 9.337 9.337 0 0 0 4.121-.952 4.125 4.125 0 0 0-7.533-2.493M15 19.128v-.003c0-1.113-.285-2.16-.786-3.07M15 19.128v.106A12.318 12.318 0 0 1 8.624 21c-2.331 0-4.512-.645-6.374-1.766l-.001-.109a6.375 6.375 0 0 1 11.964-3.07M12 6.375a3.375 3.375 0 1 1-6.75 0 3.375 3.375 0 0 1 6.75 0Zm8.25 2.25a2.625 2.625 0 1 1-5.25 0 2.625 2.625 0 0 1 5.25 0Z"/>
							</svg>
							{p.member_count} {p.member_count === 1 ? 'member' : 'members'}
						</span>
					</div>
				</button>
			{/each}
		</div>
	{/if}
</div>
