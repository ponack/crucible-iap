<script lang="ts">
	import { stacks, type Stack, type PageMeta } from '$lib/api/client';
	import { onMount } from 'svelte';

	let items = $state<Stack[]>([]);
	let pagination = $state<PageMeta | null>(null);
	let offset = $state(0);
	let loading = $state(true);
	let error = $state<string | null>(null);

	let filterQ = $state('');
	let filterTool = $state('');
	let filterStatus = $state('');

	async function load() {
		loading = true;
		error = null;
		try {
			const res = await stacks.list(offset, 50, {
				q: filterQ || undefined,
				tool: filterTool || undefined,
				status: filterStatus || undefined
			});
			items = res.data;
			pagination = res.pagination;
		} catch (e) {
			error = (e as Error).message;
		} finally {
			loading = false;
		}
	}

	onMount(load);

	function prev() { offset = Math.max(0, offset - (pagination?.limit ?? 50)); load(); }
	function next() { offset += pagination?.limit ?? 50; load(); }

	function applyFilters() { offset = 0; load(); }
	function clearFilters() { filterQ = ''; filterTool = ''; filterStatus = ''; offset = 0; load(); }

	const hasFilters = $derived(filterQ !== '' || filterTool !== '' || filterStatus !== '');

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
		applying: 'text-blue-400',
		planning: 'text-blue-400',
		preparing: 'text-blue-400',
		queued: 'text-zinc-400',
		canceled: 'text-zinc-500',
		discarded: 'text-zinc-500'
	};
</script>

<div class="p-6 space-y-4">
	<div class="flex items-center justify-between">
		<h1 class="text-lg font-semibold text-white">Stacks</h1>
		<a href="/stacks/new" class="bg-indigo-600 hover:bg-indigo-500 text-white text-sm px-3 py-1.5 rounded-lg transition-colors">
			New stack
		</a>
	</div>

	<!-- Filter bar -->
	<div class="flex items-center gap-2 flex-wrap">
		<input
			type="search" placeholder="Search stacks…"
			bind:value={filterQ}
			onkeydown={(e) => e.key === 'Enter' && applyFilters()}
			class="bg-zinc-900 border border-zinc-700 rounded-lg px-3 py-1.5 text-sm text-white placeholder-zinc-600 focus:outline-none focus:border-indigo-500 w-56"
		/>
		<select bind:value={filterTool} onchange={applyFilters}
			class="bg-zinc-900 border border-zinc-700 rounded-lg px-3 py-1.5 text-sm text-zinc-300 focus:outline-none focus:border-indigo-500">
			<option value="">All tools</option>
			<option value="opentofu">OpenTofu</option>
			<option value="terraform">Terraform</option>
			<option value="ansible">Ansible</option>
			<option value="pulumi">Pulumi</option>
		</select>
		<select bind:value={filterStatus} onchange={applyFilters}
			class="bg-zinc-900 border border-zinc-700 rounded-lg px-3 py-1.5 text-sm text-zinc-300 focus:outline-none focus:border-indigo-500">
			<option value="">Any status</option>
			<option value="finished">Finished</option>
			<option value="failed">Failed</option>
			<option value="unconfirmed">Needs approval</option>
			<option value="applying">Applying</option>
			<option value="planning">Planning</option>
		</select>
		<button onclick={applyFilters}
			class="bg-indigo-600 hover:bg-indigo-500 text-white text-sm px-3 py-1.5 rounded-lg transition-colors">
			Search
		</button>
		{#if hasFilters}
			<button onclick={clearFilters} class="text-sm text-zinc-500 hover:text-zinc-300 transition-colors">
				Clear
			</button>
		{/if}
	</div>

	{#if loading}
		<p class="text-zinc-500 text-sm">Loading…</p>
	{:else if error}
		<div class="border border-zinc-800 rounded-xl p-12 text-center">
			<p class="text-zinc-400 text-sm">Could not load stacks.</p>
			<p class="text-zinc-600 text-xs mt-1">{error}</p>
			<button onclick={load} class="mt-3 inline-block text-indigo-400 text-sm hover:underline">
				Try again →
			</button>
		</div>
	{:else if items.length === 0}
		<div class="border border-zinc-800 rounded-xl p-12 text-center">
			<p class="text-zinc-400 text-sm">No stacks yet.</p>
			<a href="/stacks/new" class="mt-3 inline-block text-indigo-400 text-sm hover:underline">
				Create your first stack →
			</a>
		</div>
	{:else}
		<div class="border border-zinc-800 rounded-xl overflow-hidden">
			<table class="w-full text-sm">
				<thead class="bg-zinc-900 text-zinc-400 text-xs uppercase tracking-wide">
					<tr>
						<th class="text-left px-4 py-3">Name</th>
						<th class="text-left px-4 py-3">Tool</th>
						<th class="text-left px-4 py-3">Branch</th>
						<th class="text-left px-4 py-3">Last run</th>
						<th class="text-left px-4 py-3">Auto-apply</th>
					</tr>
				</thead>
				<tbody class="divide-y divide-zinc-700">
					{#each items as stack (stack.id)}
						<tr class="hover:bg-zinc-900/50 transition-colors">
							<td class="px-4 py-3">
								<div class="flex items-center gap-2">
									<a href="/stacks/{stack.id}" class="font-medium {stack.is_disabled ? 'text-zinc-500 hover:text-zinc-300' : 'text-white hover:text-indigo-400'}">
										{stack.name}
									</a>
									{#if stack.is_preview}
										<span class="text-xs px-1.5 py-0.5 rounded bg-violet-950 text-violet-400" title="PR #{stack.preview_pr_number} preview">PR preview</span>
									{/if}
									{#if stack.is_disabled}
										<span class="text-xs px-1.5 py-0.5 rounded bg-zinc-800 text-zinc-500">disabled</span>
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
								{#if stack.description}
									<p class="text-zinc-500 text-xs mt-0.5">{stack.description}</p>
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
