<script lang="ts">
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { onMount } from 'svelte';
	import { stacks, runs, type Stack, type Run, type StackToken } from '$lib/api/client';

	const stackID = $derived(page.params.id);

	let stack = $state<Stack | null>(null);
	let recentRuns = $state<Run[]>([]);
	let tokens = $state<StackToken[]>([]);
	let loading = $state(true);
	let error = $state<string | null>(null);

	// Edit form state
	let editing = $state(false);
	let saving = $state(false);
	let editError = $state<string | null>(null);
	let form = $state({ name: '', description: '', repo_branch: '', project_root: '', auto_apply: false, drift_detection: false });

	// Token creation
	let newTokenName = $state('');
	let creatingToken = $state(false);
	let newTokenSecret = $state<string | null>(null);

	// Run creation
	let triggeringRun = $state(false);

	onMount(async () => {
		try {
			[stack, recentRuns, tokens] = await Promise.all([
				stacks.get(stackID),
				runs.list(stackID),
				stacks.tokens.list(stackID)
			]);
			resetForm();
		} catch (e) {
			error = (e as Error).message;
		} finally {
			loading = false;
		}
	});

	function resetForm() {
		if (!stack) return;
		form = {
			name: stack.name,
			description: stack.description ?? '',
			repo_branch: stack.repo_branch,
			project_root: stack.project_root,
			auto_apply: stack.auto_apply,
			drift_detection: stack.drift_detection
		};
	}

	async function saveEdit(e: SubmitEvent) {
		e.preventDefault();
		saving = true;
		editError = null;
		try {
			stack = await stacks.update(stackID, form);
			editing = false;
		} catch (e) {
			editError = (e as Error).message;
		} finally {
			saving = false;
		}
	}

	async function deleteStack() {
		if (!confirm(`Delete stack "${stack?.name}"? This cannot be undone.`)) return;
		await stacks.delete(stackID);
		goto('/stacks');
	}

	async function triggerRun() {
		triggeringRun = true;
		try {
			const run = await runs.create(stackID);
			goto(`/runs/${run.id}`);
		} catch (e) {
			alert((e as Error).message);
			triggeringRun = false;
		}
	}

	async function createToken(e: SubmitEvent) {
		e.preventDefault();
		creatingToken = true;
		newTokenSecret = null;
		try {
			const t = await stacks.tokens.create(stackID, newTokenName || 'default');
			newTokenSecret = t.secret ?? null;
			newTokenName = '';
			tokens = await stacks.tokens.list(stackID);
		} catch (e) {
			alert((e as Error).message);
		} finally {
			creatingToken = false;
		}
	}

	async function revokeToken(tokenID: string) {
		if (!confirm('Revoke this token? Terraform will stop being able to access state.')) return;
		await stacks.tokens.revoke(stackID, tokenID);
		tokens = tokens.filter((t) => t.id !== tokenID);
	}

	const statusColour: Record<string, string> = {
		queued: 'text-zinc-400',
		preparing: 'text-blue-400',
		planning: 'text-blue-400',
		unconfirmed: 'text-yellow-400',
		confirmed: 'text-blue-400',
		applying: 'text-blue-400',
		finished: 'text-green-400',
		failed: 'text-red-400',
		canceled: 'text-zinc-500',
		discarded: 'text-zinc-500'
	};

	function fmtDate(iso: string) {
		return new Date(iso).toLocaleString();
	}
</script>

{#if loading}
	<div class="p-6 text-zinc-500 text-sm">Loading…</div>
{:else if error || !stack}
	<div class="p-6 text-red-400 text-sm">{error ?? 'Stack not found'}</div>
{:else}
<div class="p-6 space-y-6 max-w-3xl">

	<!-- Header -->
	<div class="flex items-start justify-between">
		<div>
			<div class="flex items-center gap-2 text-sm text-zinc-500 mb-1">
				<a href="/stacks" class="hover:text-zinc-300">Stacks</a>
				<span>/</span>
				<span class="text-white font-medium">{stack.name}</span>
			</div>
			<div class="flex items-center gap-2">
				<span class="text-xs px-1.5 py-0.5 rounded font-medium
					{stack.tool === 'opentofu' ? 'bg-violet-900 text-violet-300' :
					 stack.tool === 'terraform' ? 'bg-purple-900 text-purple-300' :
					 stack.tool === 'ansible' ? 'bg-red-900 text-red-300' :
					 'bg-sky-900 text-sky-300'}">
					{stack.tool}
				</span>
				{#if stack.description}
					<span class="text-zinc-400 text-sm">{stack.description}</span>
				{/if}
			</div>
		</div>
		<div class="flex items-center gap-2">
			<button onclick={triggerRun} disabled={triggeringRun}
				class="bg-indigo-600 hover:bg-indigo-500 disabled:opacity-50 text-white text-sm px-3 py-1.5 rounded-lg transition-colors">
				{triggeringRun ? 'Queuing…' : 'Trigger run'}
			</button>
			<button onclick={() => { editing = !editing; resetForm(); }}
				class="border border-zinc-700 hover:border-zinc-500 text-zinc-300 text-sm px-3 py-1.5 rounded-lg transition-colors">
				{editing ? 'Cancel' : 'Edit'}
			</button>
			<button onclick={deleteStack}
				class="border border-red-900 hover:border-red-700 text-red-400 text-sm px-3 py-1.5 rounded-lg transition-colors">
				Delete
			</button>
		</div>
	</div>

	<!-- Edit form -->
	{#if editing}
	<div class="border border-zinc-800 rounded-xl p-5">
		{#if editError}
			<div class="mb-4 bg-red-950 border border-red-800 rounded-lg px-4 py-3 text-red-300 text-sm">{editError}</div>
		{/if}
		<form onsubmit={saveEdit} class="space-y-4">
			<div class="grid grid-cols-2 gap-4">
				<div class="space-y-1.5">
					<label class="field-label" for="edit-name">Name</label>
					<input id="edit-name" class="field-input" bind:value={form.name} required />
				</div>
				<div class="space-y-1.5">
					<label class="field-label" for="edit-branch">Branch</label>
					<input id="edit-branch" class="field-input font-mono text-sm" bind:value={form.repo_branch} />
				</div>
			</div>
			<div class="space-y-1.5">
				<label class="field-label" for="edit-desc">Description</label>
				<input id="edit-desc" class="field-input" bind:value={form.description} />
			</div>
			<div class="space-y-1.5">
				<label class="field-label" for="edit-root">Project root</label>
				<input id="edit-root" class="field-input font-mono text-sm" bind:value={form.project_root} />
			</div>
			<div class="flex gap-6">
				<label class="flex items-center gap-2 cursor-pointer text-sm text-zinc-300">
					<input type="checkbox" bind:checked={form.auto_apply} /> Auto-apply
				</label>
				<label class="flex items-center gap-2 cursor-pointer text-sm text-zinc-300">
					<input type="checkbox" bind:checked={form.drift_detection} /> Drift detection
				</label>
			</div>
			<div class="flex gap-3 pt-1">
				<button type="submit" disabled={saving}
					class="bg-indigo-600 hover:bg-indigo-500 disabled:opacity-50 text-white text-sm px-4 py-1.5 rounded-lg transition-colors">
					{saving ? 'Saving…' : 'Save changes'}
				</button>
			</div>
		</form>
	</div>
	{/if}

	<!-- Stack details -->
	<div class="border border-zinc-800 rounded-xl divide-y divide-zinc-800 text-sm">
		{#each [
			['Repository', stack.repo_url],
			['Branch', stack.repo_branch],
			['Project root', stack.project_root],
			['Auto-apply', stack.auto_apply ? 'Yes' : 'No'],
			['Drift detection', stack.drift_detection ? 'Yes' : 'No'],
			['Created', fmtDate(stack.created_at)]
		] as [label, value]}
			<div class="flex px-4 py-3">
				<span class="w-36 flex-shrink-0 text-zinc-500">{label}</span>
				<span class="text-zinc-200 font-mono text-xs break-all">{value}</span>
			</div>
		{/each}
	</div>

	<!-- Recent runs -->
	<section class="space-y-3">
		<h2 class="text-sm font-medium text-zinc-400 uppercase tracking-wide">Recent runs</h2>
		{#if recentRuns.length === 0}
			<p class="text-zinc-600 text-sm">No runs yet.</p>
		{:else}
			<div class="border border-zinc-800 rounded-xl overflow-hidden">
				<table class="w-full text-sm">
					<thead class="bg-zinc-900 text-zinc-500 text-xs uppercase tracking-wide">
						<tr>
							<th class="text-left px-4 py-2">Status</th>
							<th class="text-left px-4 py-2">Type</th>
							<th class="text-left px-4 py-2">Trigger</th>
							<th class="text-left px-4 py-2">Queued</th>
						</tr>
					</thead>
					<tbody class="divide-y divide-zinc-800">
						{#each recentRuns as run (run.id)}
							<tr class="hover:bg-zinc-900/50 transition-colors">
								<td class="px-4 py-2.5">
									<a href="/runs/{run.id}" class="font-medium {statusColour[run.status] ?? 'text-zinc-400'}">
										{run.status}
									</a>
								</td>
								<td class="px-4 py-2.5 text-zinc-400">{run.type}</td>
								<td class="px-4 py-2.5 text-zinc-500">{run.trigger}</td>
								<td class="px-4 py-2.5 text-zinc-500 text-xs">{fmtDate(run.queued_at)}</td>
							</tr>
						{/each}
					</tbody>
				</table>
			</div>
		{/if}
	</section>

	<!-- State backend / tokens -->
	<section class="space-y-3">
		<h2 class="text-sm font-medium text-zinc-400 uppercase tracking-wide">State backend</h2>
		<div class="bg-zinc-900 border border-zinc-800 rounded-xl p-4 font-mono text-xs text-zinc-300 space-y-0.5">
			<div><span class="text-zinc-500">terraform &#123;</span></div>
			<div class="pl-4"><span class="text-zinc-500">backend "http" &#123;</span></div>
			<div class="pl-8">address  = <span class="text-green-400">"{window?.location?.origin ?? 'https://your-domain'}/api/v1/state/{stackID}"</span></div>
			<div class="pl-8">username = <span class="text-yellow-400">"TOKEN_ID"</span></div>
			<div class="pl-8">password = <span class="text-yellow-400">"TOKEN_SECRET"</span></div>
			<div class="pl-4"><span class="text-zinc-500">&#125;</span></div>
			<div><span class="text-zinc-500">&#125;</span></div>
		</div>

		{#if newTokenSecret}
			<div class="bg-green-950 border border-green-800 rounded-xl p-4 space-y-2">
				<p class="text-green-300 text-sm font-medium">Token created — copy the secret now, it won't be shown again.</p>
				<code class="block text-xs text-green-200 break-all">{newTokenSecret}</code>
				<button onclick={() => (newTokenSecret = null)} class="text-xs text-green-500 hover:text-green-300">Dismiss</button>
			</div>
		{/if}

		{#if tokens.length > 0}
			<div class="border border-zinc-800 rounded-xl overflow-hidden">
				<table class="w-full text-sm">
					<thead class="bg-zinc-900 text-zinc-500 text-xs uppercase tracking-wide">
						<tr>
							<th class="text-left px-4 py-2">Name</th>
							<th class="text-left px-4 py-2">Token ID</th>
							<th class="text-left px-4 py-2">Last used</th>
							<th class="px-4 py-2"></th>
						</tr>
					</thead>
					<tbody class="divide-y divide-zinc-800">
						{#each tokens as token (token.id)}
							<tr>
								<td class="px-4 py-2.5 text-zinc-300">{token.name}</td>
								<td class="px-4 py-2.5 font-mono text-xs text-zinc-500">{token.id}</td>
								<td class="px-4 py-2.5 text-zinc-500 text-xs">{token.last_used ? fmtDate(token.last_used) : '—'}</td>
								<td class="px-4 py-2.5 text-right">
									<button onclick={() => revokeToken(token.id)}
										class="text-xs text-red-500 hover:text-red-300">Revoke</button>
								</td>
							</tr>
						{/each}
					</tbody>
				</table>
			</div>
		{/if}

		<form onsubmit={createToken} class="flex items-center gap-2">
			<input class="field-input w-48" bind:value={newTokenName} placeholder="Token name" />
			<button type="submit" disabled={creatingToken}
				class="border border-zinc-700 hover:border-zinc-500 text-zinc-300 text-sm px-3 py-1.5 rounded-lg transition-colors disabled:opacity-50">
				{creatingToken ? 'Creating…' : 'New token'}
			</button>
		</form>
	</section>

</div>
{/if}

<style>
	:global(.field-label) {
		display: block;
		font-size: 0.75rem;
		color: var(--color-zinc-400);
	}
	:global(.field-input) {
		display: block;
		width: 100%;
		padding: 0.375rem 0.625rem;
		background: var(--color-zinc-900);
		border: 1px solid var(--color-zinc-700);
		border-radius: 0.5rem;
		color: #fff;
		font-size: 0.875rem;
		outline: none;
		transition: border-color 0.1s;
	}
	:global(.field-input:focus) {
		border-color: #6366f1;
	}
</style>
