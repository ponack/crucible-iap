<script lang="ts">
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { runs, orgTags, type Run, type Tag, type PageMeta } from '$lib/api/client';
	import { triggerBadge } from '$lib/trigger';
	import EmptyState from '$lib/components/EmptyState.svelte';

	let loading = $state(true);
	let error = $state<string | null>(null);
	let allRuns = $state<Run[]>([]);
	let pagination = $state<PageMeta | null>(null);
	let offset = $state(0);
	let allTags = $state<Tag[]>([]);
	let tagDropdownOpen = $state(false);

	let filterStatus = $state('');
	let filterType = $state('');
	let filterStack = $state(page.url.searchParams.get('stack') ?? '');
	let filterTags = $state<string[]>([]);

	async function load() {
		loading = true;
		error = null;
		try {
			const filters = {
				status: filterStatus || undefined,
				type: filterType || undefined,
				tags: filterTags.length ? filterTags : undefined
			};
			const res = filterStack
				? await runs.list(filterStack, offset, 50, filters)
				: await runs.listAll(offset, 50, filters);
			allRuns = res.data;
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
	function clearFilters() { filterStatus = ''; filterType = ''; filterStack = ''; filterTags = []; offset = 0; load(); }

	function toggleTagFilter(name: string) {
		filterTags = filterTags.includes(name)
			? filterTags.filter((t) => t !== name)
			: [...filterTags, name];
		offset = 0;
		load();
	}

	const hasFilters = $derived(filterStatus !== '' || filterType !== '' || filterStack !== '' || filterTags.length > 0);

	function fmtDate(iso: string) {
		return new Date(iso).toLocaleString();
	}

	const statusColour: Record<string, string> = {
		queued: 'text-zinc-400',
		preparing: 'text-teal-400',
		planning: 'text-teal-400',
		unconfirmed: 'text-yellow-400',
		pending_approval: 'text-purple-400',
		confirmed: 'text-teal-400',
		applying: 'text-teal-400',
		finished: 'text-green-400',
		failed: 'text-red-400',
		canceled: 'text-zinc-500',
		discarded: 'text-zinc-500'
	};
</script>

<div class="p-6 space-y-4">
	<div class="flex items-center justify-between">
		<h1 class="text-lg font-semibold text-white">Runs</h1>
		<div class="flex items-center gap-2">
			<select bind:value={filterStatus} onchange={applyFilters} class="field-input w-44 py-1.5">
				<option value="">Any status</option>
				<option value="queued">Queued</option>
				<option value="planning">Planning</option>
				<option value="pending_approval">Pending approval</option>
			<option value="unconfirmed">Needs confirmation</option>
				<option value="applying">Applying</option>
				<option value="finished">Finished</option>
				<option value="failed">Failed</option>
				<option value="canceled">Canceled</option>
				<option value="discarded">Discarded</option>
			</select>
			<select bind:value={filterType} onchange={applyFilters} class="field-input w-36 py-1.5">
				<option value="">Any type</option>
				<option value="tracked">Tracked</option>
				<option value="proposed">Proposed</option>
				<option value="destroy">Destroy</option>
			</select>
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
					</button>
					{#if tagDropdownOpen}
						<!-- svelte-ignore a11y_click_events_have_key_events a11y_no_static_element_interactions -->
						<div class="fixed inset-0 z-10" onclick={() => (tagDropdownOpen = false)}></div>
						<div class="absolute top-full mt-1 right-0 z-20 min-w-44 rounded-xl border border-zinc-700 shadow-xl py-1"
							style="background: var(--color-zinc-900);">
							{#each allTags as tag (tag.id)}
								<button onclick={() => { toggleTagFilter(tag.name); tagDropdownOpen = false; }}
									class="w-full flex items-center gap-2.5 px-3 py-2 text-sm hover:bg-zinc-800 transition-colors text-left">
									<span class="w-3 h-3 rounded-full flex-shrink-0" style="background: {tag.color};"></span>
									<span class="flex-1 text-zinc-200">{tag.name}</span>
									{#if filterTags.includes(tag.name)}
										<svg class="h-3.5 w-3.5" style="color: var(--accent);" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><path d="m4.5 12.75 6 6 9-13.5"/></svg>
									{/if}
								</button>
							{/each}
						</div>
					{/if}
				</div>
			{/if}
			{#if hasFilters}
				<button onclick={clearFilters} class="text-sm text-zinc-500 hover:text-zinc-300 transition-colors">
					Clear
				</button>
			{/if}
		</div>
	</div>

	{#if loading}
		<p class="text-zinc-500 text-sm">Loading…</p>
	{:else if error}
		<p class="text-red-400 text-sm">{error}</p>
	{:else if allRuns.length === 0}
		<EmptyState
			icon="M5.25 5.653c0-.856.917-1.398 1.667-.986l11.54 6.347a1.125 1.125 0 0 1 0 1.972l-11.54 6.347a1.125 1.125 0 0 1-1.667-.986V5.653Z"
			heading="No runs yet"
			sub="Runs appear here when a stack is triggered manually, by a webhook push, or on a schedule."
		/>
	{:else}
		<div class="border border-zinc-800 rounded-xl overflow-hidden">
			<table class="w-full text-sm">
				<thead class="bg-zinc-900 text-zinc-400 text-xs uppercase tracking-wide">
					<tr>
						<th class="text-left px-4 py-3">Status</th>
						<th class="text-left px-4 py-3">Stack</th>
						<th class="text-left px-4 py-3">Type</th>
						<th class="text-left px-4 py-3">Trigger</th>
						<th class="text-left px-4 py-3">Queued</th>
					</tr>
				</thead>
				<tbody class="divide-y divide-zinc-700">
					{#each allRuns as run (run.id)}
						{@const tb = triggerBadge(run.trigger)}
						<tr class="hover:bg-zinc-900/50 transition-colors">
							<td class="px-4 py-3">
								<a href="/runs/{run.id}" class="font-medium {statusColour[run.status] ?? 'text-zinc-400'}">
									{run.status}
								</a>
							</td>
							<td class="px-4 py-3">
								<a href="/stacks/{run.stack_id}" class="text-zinc-300 hover:text-white">
									{run.stack_name ?? run.stack_id.slice(0, 8)}
								</a>
								{#if run.commit_message}
									<div class="text-xs text-zinc-600 font-mono truncate max-w-xs mt-0.5" title={run.commit_message}>
										{run.commit_sha ? run.commit_sha.slice(0, 7) + ' ' : ''}{run.commit_message}
									</div>
								{/if}
							</td>
							<td class="px-4 py-3 {run.type === 'destroy' ? 'text-orange-400 font-medium' : 'text-zinc-400'}">
								{run.type}{#if run.is_drift} <span class="text-xs text-amber-500">drift</span>{/if}
								{#if run.pr_number}
									<a href={run.pr_url} target="_blank" rel="noopener"
										class="ml-1 text-xs text-teal-400 hover:text-teal-300">#{run.pr_number}</a>
								{/if}
							</td>
							<td class="px-4 py-3">
								<span class="text-xs px-1.5 py-0.5 rounded font-medium {tb.classes}">{tb.label}</span>
							</td>
							<td class="px-4 py-3 text-zinc-500 text-xs">{fmtDate(run.queued_at)}</td>
						</tr>
					{/each}
				</tbody>
			</table>
		</div>

		{#if pagination && pagination.total > pagination.limit}
			<div class="flex items-center justify-between text-xs text-zinc-500">
				<span>{offset + 1}–{Math.min(offset + allRuns.length, pagination.total)} of {pagination.total}</span>
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
