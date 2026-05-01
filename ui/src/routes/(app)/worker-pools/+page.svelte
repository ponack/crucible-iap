<script lang="ts">
	import { workerPools, type WorkerPool } from '$lib/api/client';
	import { onMount } from 'svelte';

	let items = $state<WorkerPool[]>([]);
	let loading = $state(true);
	let error = $state<string | null>(null);

	let creating = $state(false);
	let newName = $state('');
	let newDesc = $state('');
	let newCapacity = $state(3);
	let createError = $state<string | null>(null);
	let newToken = $state<string | null>(null);
	let newPoolName = $state<string | null>(null);

	let rotatingID = $state<string | null>(null);
	let rotatedToken = $state<string | null>(null);
	let rotatedPoolName = $state<string | null>(null);

	async function load() {
		loading = true;
		error = null;
		try {
			const res = await workerPools.list();
			items = res.data;
		} catch (e) {
			error = (e as Error).message;
		} finally {
			loading = false;
		}
	}

	onMount(load);

	async function handleCreate() {
		createError = null;
		newToken = null;
		try {
			const res = await workerPools.create({ name: newName, description: newDesc, capacity: newCapacity });
			newToken = res.token;
			newPoolName = res.pool.name;
			items = [res.pool, ...items];
			newName = '';
			newDesc = '';
			newCapacity = 3;
			creating = false;
		} catch (e) {
			createError = (e as Error).message;
		}
	}

	async function handleDelete(id: string, name: string) {
		if (!confirm(`Delete worker pool "${name}"? Any stacks using it will need to be reassigned.`)) return;
		try {
			await workerPools.delete(id);
			items = items.filter(p => p.id !== id);
		} catch (e) {
			alert((e as Error).message);
		}
	}

	async function handleRotate(id: string, name: string) {
		rotatingID = id;
		rotatedToken = null;
		try {
			const res = await workerPools.rotateToken(id);
			rotatedToken = res.token;
			rotatedPoolName = name;
		} catch (e) {
			alert((e as Error).message);
		} finally {
			rotatingID = null;
		}
	}

	function relativeTime(ts: string | undefined): string {
		if (!ts) return 'never';
		const diff = Date.now() - new Date(ts).getTime();
		if (diff < 60000) return 'just now';
		if (diff < 3600000) return `${Math.floor(diff / 60000)}m ago`;
		if (diff < 86400000) return `${Math.floor(diff / 3600000)}h ago`;
		return `${Math.floor(diff / 86400000)}d ago`;
	}
</script>

<div class="p-6 space-y-6">
	<div class="flex items-center justify-between">
		<div>
			<h1 class="text-lg font-semibold text-white">Worker Pools</h1>
			<p class="text-zinc-500 text-sm mt-0.5">External agent processes that run jobs on your own infrastructure instead of the built-in runner.</p>
		</div>
		<button onclick={() => creating = !creating}
			class="bg-indigo-600 hover:bg-indigo-500 text-white text-sm px-3 py-1.5 rounded-lg transition-colors">
			New pool
		</button>
	</div>

	{#if newToken}
		<div class="border border-yellow-800 bg-yellow-950/30 rounded-xl p-4 space-y-2">
			<p class="text-yellow-300 text-sm font-medium">Pool "{newPoolName}" created — save this token now</p>
			<p class="text-yellow-500 text-xs">It will not be shown again. Use it as <code class="bg-yellow-950 px-1 rounded">CRUCIBLE_POOL_TOKEN</code> for your agent.</p>
			<code class="block bg-zinc-900 border border-zinc-700 rounded p-3 text-xs text-green-400 break-all select-all">{newToken}</code>
			<button onclick={() => newToken = null} class="text-xs text-zinc-500 hover:text-zinc-300">Dismiss</button>
		</div>
	{/if}

	{#if rotatedToken}
		<div class="border border-yellow-800 bg-yellow-950/30 rounded-xl p-4 space-y-2">
			<p class="text-yellow-300 text-sm font-medium">New token for "{rotatedPoolName}" — save it now</p>
			<p class="text-yellow-500 text-xs">The old token is immediately invalidated. Update all agents before dismissing.</p>
			<code class="block bg-zinc-900 border border-zinc-700 rounded p-3 text-xs text-green-400 break-all select-all">{rotatedToken}</code>
			<button onclick={() => rotatedToken = null} class="text-xs text-zinc-500 hover:text-zinc-300">Dismiss</button>
		</div>
	{/if}

	{#if creating}
		<div class="border border-zinc-800 rounded-xl p-4 space-y-3">
			<h2 class="text-sm font-medium text-white">New worker pool</h2>
			{#if createError}
				<p class="text-red-400 text-xs">{createError}</p>
			{/if}
			<div class="grid grid-cols-2 gap-3">
				<div class="col-span-2 flex flex-col gap-1">
					<label for="pool-name" class="text-xs text-zinc-400">Name</label>
					<input id="pool-name" bind:value={newName} placeholder="my-pool"
						class="bg-zinc-900 border border-zinc-700 rounded-lg px-3 py-1.5 text-sm text-white focus:outline-none focus:border-indigo-500" />
				</div>
				<div class="col-span-2 flex flex-col gap-1">
					<label for="pool-desc" class="text-xs text-zinc-400">Description (optional)</label>
					<input id="pool-desc" bind:value={newDesc} placeholder="On-prem GPU cluster"
						class="bg-zinc-900 border border-zinc-700 rounded-lg px-3 py-1.5 text-sm text-white focus:outline-none focus:border-indigo-500" />
				</div>
				<div class="flex flex-col gap-1">
					<label for="pool-cap" class="text-xs text-zinc-400">Max concurrent runs</label>
					<input id="pool-cap" type="number" min="1" max="50" bind:value={newCapacity}
						class="bg-zinc-900 border border-zinc-700 rounded-lg px-3 py-1.5 text-sm text-white focus:outline-none focus:border-indigo-500" />
				</div>
			</div>
			<div class="flex gap-2">
				<button onclick={handleCreate} disabled={!newName}
					class="bg-indigo-600 hover:bg-indigo-500 disabled:opacity-40 text-white text-sm px-4 py-1.5 rounded-lg transition-colors">
					Create
				</button>
				<button onclick={() => { creating = false; createError = null; }} class="text-sm text-zinc-500 hover:text-zinc-300">
					Cancel
				</button>
			</div>
		</div>
	{/if}

	{#if loading}
		<p class="text-zinc-500 text-sm">Loading…</p>
	{:else if error}
		<div class="border border-zinc-800 rounded-xl p-12 text-center">
			<p class="text-zinc-400 text-sm">Could not load worker pools.</p>
			<p class="text-zinc-600 text-xs mt-1">{error}</p>
			<button onclick={load} class="mt-3 text-indigo-400 text-sm hover:underline">Try again →</button>
		</div>
	{:else if items.length === 0 && !creating}
		<div class="border border-zinc-800 rounded-xl p-12 text-center space-y-2">
			<p class="text-zinc-400 text-sm">No worker pools yet.</p>
			<p class="text-zinc-600 text-xs max-w-sm mx-auto">Worker pools let you run infrastructure jobs on your own servers. Deploy <code class="text-zinc-400">crucible-agent</code> with a pool token and point stacks at the pool.</p>
			<button onclick={() => creating = true} class="mt-3 text-indigo-400 text-sm hover:underline">Create your first pool →</button>
		</div>
	{:else if items.length > 0}
		<div class="border border-zinc-800 rounded-xl overflow-hidden">
			<table class="w-full text-sm">
				<thead class="bg-zinc-900 text-zinc-400 text-xs uppercase tracking-wide">
					<tr>
						<th class="text-left px-4 py-3">Name</th>
						<th class="text-left px-4 py-3">Capacity</th>
						<th class="text-left px-4 py-3">Last seen</th>
						<th class="text-left px-4 py-3">Status</th>
						<th class="px-4 py-3"></th>
					</tr>
				</thead>
				<tbody class="divide-y divide-zinc-700">
					{#each items as pool (pool.id)}
						<tr class="hover:bg-zinc-900/50 transition-colors">
							<td class="px-4 py-3">
								<div class="font-medium text-white">{pool.name}</div>
								{#if pool.description}
									<p class="text-zinc-500 text-xs mt-0.5">{pool.description}</p>
								{/if}
							</td>
							<td class="px-4 py-3 text-zinc-400">{pool.capacity} concurrent</td>
							<td class="px-4 py-3 text-zinc-400 text-xs">{relativeTime(pool.last_seen_at)}</td>
							<td class="px-4 py-3">
								{#if pool.is_disabled}
									<span class="text-xs px-1.5 py-0.5 rounded bg-zinc-800 text-zinc-500">disabled</span>
								{:else}
									<span class="text-xs px-1.5 py-0.5 rounded bg-green-950 text-green-400">active</span>
								{/if}
							</td>
							<td class="px-4 py-3 text-right">
								<div class="flex items-center justify-end gap-3">
									<button onclick={() => handleRotate(pool.id, pool.name)}
										disabled={rotatingID === pool.id}
										class="text-xs text-zinc-500 hover:text-zinc-300 disabled:opacity-40">
										Rotate token
									</button>
									<button onclick={() => handleDelete(pool.id, pool.name)}
										class="text-xs text-red-500 hover:text-red-300">
										Delete
									</button>
								</div>
							</td>
						</tr>
					{/each}
				</tbody>
			</table>
		</div>
	{/if}

	<div class="border border-zinc-800 rounded-xl p-4 space-y-4">
		<div>
			<h2 class="text-sm font-medium text-white">Deploying an agent</h2>
			<p class="text-zinc-500 text-xs mt-1">The agent runs on any host with Docker access — your own server, a VM, or bare metal. It is fully independent of the Crucible stack and uses its own config file.</p>
		</div>

		<div class="space-y-1.5">
			<p class="text-zinc-300 text-xs font-medium">Option A — Separate host (recommended)</p>
			<p class="text-zinc-500 text-xs">Copy <code class="bg-zinc-800 px-1 rounded">docker-compose.agent.yml</code> and <code class="bg-zinc-800 px-1 rounded">.env.agent.example</code> from the Crucible repo to the target host, then:</p>
			<pre class="bg-zinc-900 border border-zinc-700 rounded p-3 text-xs text-zinc-300 overflow-x-auto">{`cp .env.agent.example .env.agent
# Edit .env.agent — set CRUCIBLE_API_URL, CRUCIBLE_ORG_ID, CRUCIBLE_POOL_TOKEN

docker compose -f docker-compose.agent.yml up -d`}</pre>
		</div>

		<div class="space-y-1.5">
			<p class="text-zinc-300 text-xs font-medium">Option B — Same host as Crucible</p>
			<p class="text-zinc-500 text-xs">Run the agent alongside the main stack using the <code class="bg-zinc-800 px-1 rounded">worker-agent</code> compose profile. The API URL is wired up automatically.</p>
			<pre class="bg-zinc-900 border border-zinc-700 rounded p-3 text-xs text-zinc-300 overflow-x-auto">{`cp .env.agent.example .env.agent
# Edit .env.agent — set CRUCIBLE_ORG_ID and CRUCIBLE_POOL_TOKEN

docker compose --profile worker-agent up -d crucible-agent`}</pre>
		</div>

		<p class="text-zinc-600 text-xs">Full env var reference and setup guide: <span class="text-zinc-400">docs/operator-guide.md → External worker agents</span>. Assign stacks to this pool via <span class="text-zinc-400">Settings → Worker pool</span> in the stack detail page.</p>
	</div>
</div>
