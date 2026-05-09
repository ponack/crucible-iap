<script lang="ts">
	import { stacks, orgTags, type Stack, type Tag, type PageMeta } from '$lib/api/client';
	import { onMount } from 'svelte';
	import { toast } from '$lib/stores/toasts.svelte';
	import EmptyState from '$lib/components/EmptyState.svelte';

	let items = $state<Stack[]>([]);
	let pagination = $state<PageMeta | null>(null);
	let offset = $state(0);
	let loading = $state(true);
	let error = $state<string | null>(null);
	let allTags = $state<Tag[]>([]);
	let tagDropdownOpen = $state(false);

	let filterQ = $state('');
	let filterTool = $state('');
	let filterStatus = $state('');
	let filterTags = $state<string[]>([]); // tag names

	async function load() {
		loading = true;
		error = null;
		try {
			const res = await stacks.list(offset, 50, {
				q: filterQ || undefined,
				tool: filterTool || undefined,
				status: filterStatus || undefined,
				tags: filterTags.length ? filterTags : undefined
			});
			items = res.data;
			pagination = res.pagination;
		} catch (e) {
			error = (e as Error).message;
		} finally {
			loading = false;
		}
	}

	onMount(async () => {
		const [, tagsRes] = await Promise.allSettled([load(), orgTags.list()]);
		if (tagsRes.status === 'fulfilled') allTags = tagsRes.value;
	});

	function prev() { offset = Math.max(0, offset - (pagination?.limit ?? 50)); load(); }
	function next() { offset += pagination?.limit ?? 50; load(); }
	function applyFilters() { offset = 0; load(); }
	function clearFilters() {
		filterQ = ''; filterTool = ''; filterStatus = ''; filterTags = [];
		offset = 0; load();
	}

	function toggleTagFilter(name: string) {
		filterTags = filterTags.includes(name)
			? filterTags.filter((t) => t !== name)
			: [...filterTags, name];
		offset = 0;
		load();
	}

	const hasFilters = $derived(
		filterQ !== '' || filterTool !== '' || filterStatus !== '' || filterTags.length > 0
	);

	async function togglePin(stack: Stack, e: MouseEvent) {
		e.preventDefault();
		try {
			if (stack.is_pinned) {
				await stacks.unpin(stack.id);
			} else {
				await stacks.pin(stack.id);
			}
			await load();
		} catch (err) {
			toast.error((err as Error).message);
		}
	}

	const toolBadge: Record<string, string> = {
		opentofu: 'bg-violet-900 text-violet-300',
		terraform: 'bg-purple-900 text-purple-300',
		ansible: 'bg-red-900 text-red-300',
		pulumi: 'bg-sky-900 text-sky-300'
	};

	const runStatusColour: Record<string, string> = {
		finished: 'text-green-400',
		failed: 'text-red-400',
		unconfirmed: 'text-yellow-400',
		applying: 'text-teal-400',
		planning: 'text-teal-400',
		preparing: 'text-teal-400',
		queued: 'text-zinc-400',
		canceled: 'text-zinc-500',
		discarded: 'text-zinc-500'
	};
</script>

<div class="p-6 space-y-4">
	<div class="flex items-center justify-between">
		<h1 class="text-lg font-semibold text-white">Stacks</h1>
		<a href="/stacks/new"
			class="text-sm font-medium px-3 py-1.5 rounded-lg transition-colors"
			style="background: var(--accent-muted); color: var(--accent); border: 1px solid var(--accent-border);">
			+ New stack
		</a>
	</div>

	<!-- Filter bar -->
	<div class="flex items-center gap-2 flex-wrap">
		<div class="w-52">
			<input
				type="search" placeholder="Search stacks…"
				bind:value={filterQ}
				onkeydown={(e) => e.key === 'Enter' && applyFilters()}
				class="field-input py-1.5"
			/>
		</div>
		<div class="w-36">
			<select bind:value={filterTool} onchange={applyFilters} class="field-input py-1.5">
				<option value="">All tools</option>
				<option value="opentofu">OpenTofu</option>
				<option value="terraform">Terraform</option>
				<option value="ansible">Ansible</option>
				<option value="pulumi">Pulumi</option>
			</select>
		</div>
		<div class="w-40">
			<select bind:value={filterStatus} onchange={applyFilters} class="field-input py-1.5">
				<option value="">Any status</option>
				<option value="finished">Finished</option>
				<option value="failed">Failed</option>
				<option value="unconfirmed">Needs approval</option>
				<option value="applying">Applying</option>
				<option value="planning">Planning</option>
			</select>
		</div>

		<!-- Tag filter dropdown -->
		{#if allTags.length > 0}
			<div class="relative">
				<button
					onclick={() => (tagDropdownOpen = !tagDropdownOpen)}
					class="field-input py-1.5 flex items-center gap-1.5 w-auto px-3 text-sm"
					style={filterTags.length ? 'border-color: var(--accent); color: var(--accent);' : ''}>
					<svg class="h-3.5 w-3.5 flex-shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">
						<path d="M9.568 3H5.25A2.25 2.25 0 0 0 3 5.25v4.318c0 .597.237 1.17.659 1.591l9.581 9.581c.699.699 1.78.872 2.607.33a18.095 18.095 0 0 0 5.223-5.223c.542-.827.369-1.908-.33-2.607L9.568 3Z"/>
						<path d="M6 6h.008v.008H6V6Z"/>
					</svg>
					Tags{filterTags.length ? ` (${filterTags.length})` : ''}
					<svg class="h-3 w-3 flex-shrink-0" viewBox="0 0 12 12" fill="none">
						<path d="M3 4.5L6 7.5L9 4.5" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"/>
					</svg>
				</button>
				{#if tagDropdownOpen}
					<!-- svelte-ignore a11y_click_events_have_key_events a11y_no_static_element_interactions -->
					<div class="fixed inset-0 z-10" onclick={() => (tagDropdownOpen = false)}></div>
					<div class="absolute top-full mt-1 left-0 z-20 min-w-48 rounded-xl border border-zinc-700 shadow-xl py-1"
						style="background: var(--color-zinc-900);">
						{#each allTags as tag (tag.id)}
							<button
								onclick={() => toggleTagFilter(tag.name)}
								class="w-full flex items-center gap-2.5 px-3 py-2 text-sm hover:bg-zinc-800 transition-colors text-left">
								<span class="w-3 h-3 rounded-full flex-shrink-0 {filterTags.includes(tag.name) ? 'ring-2 ring-offset-1' : ''}"
									style="background: {tag.color}; {filterTags.includes(tag.name) ? `ring-color: ${tag.color}; ring-offset-color: var(--color-zinc-900);` : ''}">
								</span>
								<span class="flex-1 text-zinc-200">{tag.name}</span>
								{#if filterTags.includes(tag.name)}
									<svg class="h-3.5 w-3.5 flex-shrink-0" style="color: var(--accent);" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5">
										<path d="m4.5 12.75 6 6 9-13.5"/>
									</svg>
								{/if}
							</button>
						{/each}
					</div>
				{/if}
			</div>
		{/if}

		{#if hasFilters}
			<button onclick={clearFilters} class="text-sm text-zinc-500 hover:text-zinc-300 transition-colors">
				Clear filters
			</button>
		{/if}
	</div>

	<!-- Active tag filter pills -->
	{#if filterTags.length > 0}
		<div class="flex items-center gap-2 flex-wrap">
			{#each filterTags as tagName}
				{@const tag = allTags.find((t) => t.name === tagName)}
				<span class="inline-flex items-center gap-1.5 text-xs px-2.5 py-1 rounded-full border"
					style="border-color: {tag?.color ?? '#6B7280'}33; background: {tag?.color ?? '#6B7280'}15; color: var(--color-zinc-200);">
					<span class="w-2 h-2 rounded-full flex-shrink-0" style="background: {tag?.color ?? '#6B7280'};"></span>
					{tagName}
					<button onclick={() => toggleTagFilter(tagName)} class="ml-0.5 hover:text-white">×</button>
				</span>
			{/each}
		</div>
	{/if}

	{#if loading}
		<p class="text-zinc-500 text-sm">Loading…</p>
	{:else if error}
		<div class="border border-zinc-800 rounded-xl p-12 text-center">
			<p class="text-zinc-400 text-sm">Could not load stacks.</p>
			<p class="text-zinc-600 text-xs mt-1">{error}</p>
			<button onclick={load} class="mt-3 inline-block text-sm hover:underline" style="color: var(--accent);">
				Try again →
			</button>
		</div>
	{:else if items.length === 0}
		<EmptyState
			icon="M6.429 9.75 2.25 12l4.179 2.25m0-4.5 5.571 3 5.571-3m-11.142 0L2.25 7.5 12 2.25l9.75 5.25-4.179 2.25m0 0L21.75 12l-4.179 2.25m0 0 4.179 2.25L12 21.75 2.25 16.5l4.179-2.25m11.142 0-5.571 3-5.571-3"
			heading={hasFilters ? 'No stacks match these filters' : 'No stacks yet'}
			sub={hasFilters ? 'Try adjusting your search or tag filters.' : 'Create your first stack to start managing infrastructure with OpenTofu, Terraform, Ansible, or Pulumi.'}
		>
			{#if hasFilters}
				<button onclick={clearFilters} class="text-xs font-medium px-3 py-1.5 rounded-lg transition-colors" style="background: var(--accent-muted); color: var(--accent); border: 1px solid var(--accent-border);">
					Clear filters
				</button>
			{:else}
				<a href="/stacks/new" class="text-xs font-medium px-3 py-1.5 rounded-lg transition-colors" style="background: var(--accent-muted); color: var(--accent); border: 1px solid var(--accent-border);">
					New stack →
				</a>
			{/if}
		</EmptyState>
	{:else}
		<div class="border border-zinc-800 rounded-xl overflow-hidden">
			<table class="w-full text-sm">
				<thead class="text-zinc-400 text-xs uppercase tracking-wide" style="background: var(--color-zinc-900); border-bottom: 1px solid var(--color-zinc-800);">
					<tr>
						<th class="text-left px-4 py-3">Name</th>
						<th class="text-left px-4 py-3">Tool</th>
						<th class="text-left px-4 py-3">Branch</th>
						<th class="text-left px-4 py-3">Last run</th>
						<th class="text-left px-4 py-3">Auto-apply</th>
						<th class="px-4 py-3 w-8"></th>
					</tr>
				</thead>
				<tbody class="divide-y divide-zinc-800">
					{#each items as stack (stack.id)}
						<tr class="hover:bg-zinc-900/50 transition-colors">
							<td class="px-4 py-3">
								<div class="flex items-center gap-2 flex-wrap">
									{#if stack.is_pinned}
										<svg class="h-3 w-3 flex-shrink-0" style="color: var(--accent);" viewBox="0 0 24 24" fill="currentColor">
											<path d="M15.75 9V5.25A2.25 2.25 0 0 0 13.5 3h-3a2.25 2.25 0 0 0-2.25 2.25V9l-1.5 1.5v.75h10.5v-.75L15.75 9ZM9 15v3.75a.75.75 0 0 0 .75.75h4.5a.75.75 0 0 0 .75-.75V15H9Z"/>
										</svg>
									{/if}
									<a href="/stacks/{stack.id}" class="font-medium {stack.is_disabled ? 'text-zinc-500 hover:text-zinc-300' : 'text-white hover:text-opacity-80'}">
										{stack.name}
									</a>
									{#if stack.is_preview}
										<span class="text-xs px-1.5 py-0.5 rounded bg-violet-950 text-violet-400" title="PR #{stack.preview_pr_number} preview">PR preview</span>
									{/if}
									{#if stack.is_disabled}
										<span class="text-xs px-1.5 py-0.5 rounded bg-zinc-800 text-zinc-500">disabled</span>
									{/if}
									{#if stack.is_locked}
										<span class="text-xs px-1.5 py-0.5 rounded bg-amber-950 text-amber-400" title={stack.lock_reason || 'Locked'}>locked</span>
									{/if}
									{#if stack.upstream_count > 0 || stack.downstream_count > 0}
										{@const upNames = stack.upstream_stacks?.map((s) => s.name).join(', ')}
										{@const downNames = stack.downstream_stacks?.map((s) => s.name).join(', ')}
										{@const tip = [upNames ? `needs: ${upNames}` : '', downNames ? `needed by: ${downNames}` : ''].filter(Boolean).join(' · ')}
										<span class="text-xs text-zinc-600 font-mono" title={tip}>
											{[stack.upstream_count > 0 ? `↑${stack.upstream_count}` : '', stack.downstream_count > 0 ? `↓${stack.downstream_count}` : ''].filter(Boolean).join(' ')}
										</span>
									{/if}
								</div>
								<!-- Tag pills -->
								{#if stack.tags?.length > 0}
									<div class="flex items-center gap-1.5 mt-1 flex-wrap">
										{#each stack.tags as tag (tag.id)}
											<button
												onclick={() => { if (!filterTags.includes(tag.name)) toggleTagFilter(tag.name); }}
												class="inline-flex items-center gap-1 text-xs px-2 py-0.5 rounded-full border transition-colors hover:opacity-80"
												style="border-color: {tag.color}33; background: {tag.color}18; color: var(--color-zinc-300);"
												title="Filter by {tag.name}">
												<span class="w-1.5 h-1.5 rounded-full flex-shrink-0" style="background: {tag.color};"></span>
												{tag.name}
											</button>
										{/each}
									</div>
								{/if}
								{#if stack.description}
									<p class="text-zinc-500 text-xs mt-0.5 {stack.tags?.length > 0 ? '' : ''}">
										{stack.description}
									</p>
								{/if}
							</td>
							<td class="px-4 py-3">
								<span class="px-1.5 py-0.5 rounded text-xs font-medium {toolBadge[stack.tool] ?? 'bg-zinc-800 text-zinc-400'}">
									{stack.tool}
								</span>
							</td>
							<td class="px-4 py-3 text-zinc-400 font-mono text-xs">{stack.repo_branch}</td>
							<td class="px-4 py-3 text-xs">
								{#if stack.last_run_status}
									<span class="font-medium {runStatusColour[stack.last_run_status] ?? 'text-zinc-400'}">
										{stack.last_run_status}
									</span>
								{:else}
									<span class="text-zinc-600">—</span>
								{/if}
							</td>
							<td class="px-4 py-3 text-zinc-400">
								{stack.auto_apply ? '✓' : '—'}
							</td>
							<td class="px-4 py-3">
								<button
									onclick={(e) => togglePin(stack, e)}
									title={stack.is_pinned ? 'Unpin stack' : 'Pin stack'}
									class="p-1 rounded transition-colors hover:bg-zinc-800 {stack.is_pinned ? '' : 'opacity-20 hover:opacity-60'}"
									style={stack.is_pinned ? 'color: var(--accent);' : 'color: var(--color-zinc-400);'}>
									<svg class="h-3.5 w-3.5" viewBox="0 0 24 24" fill={stack.is_pinned ? 'currentColor' : 'none'} stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">
										<path d="M15.75 9V5.25A2.25 2.25 0 0 0 13.5 3h-3a2.25 2.25 0 0 0-2.25 2.25V9l-1.5 1.5v.75h10.5v-.75L15.75 9ZM9 15v3.75a.75.75 0 0 0 .75.75h4.5a.75.75 0 0 0 .75-.75V15H9Z"/>
									</svg>
								</button>
							</td>
						</tr>
					{/each}
				</tbody>
			</table>
		</div>

		{#if pagination && pagination.total > pagination.limit}
			<div class="flex items-center justify-between text-xs text-zinc-500">
				<span>{offset + 1}–{Math.min(offset + items.length, pagination.total)} of {pagination.total}</span>
				<div class="flex gap-2">
					<button onclick={prev} disabled={offset === 0} class="px-3 py-1.5 rounded-lg bg-zinc-800 hover:bg-zinc-700 disabled:opacity-40 transition-colors">
						Previous
					</button>
					<button onclick={next} disabled={!pagination.has_more} class="px-3 py-1.5 rounded-lg bg-zinc-800 hover:bg-zinc-700 disabled:opacity-40 transition-colors">
						Next
					</button>
				</div>
			</div>
		{/if}
	{/if}
</div>
