<script lang="ts">
	import { onMount } from 'svelte';
	import { policies, type Policy, type PolicyResult } from '$lib/api/client';
	import { sampleInputs, type PolicyType } from '$lib/policy-data';

	let allPolicies = $state<Policy[]>([]);
	let selectedID = $state('');
	let testInput = $state('{}');
	let traceEnabled = $state(false);
	let testing = $state(false);
	let testResult = $state<{ ok: boolean; error?: string; result?: PolicyResult; trace?: string } | null>(null);
	let loadError = $state<string | null>(null);

	const selected = $derived(allPolicies.find((p) => p.id === selectedID) ?? null);

	onMount(async () => {
		try {
			allPolicies = await policies.list();
			if (allPolicies.length > 0) {
				selectedID = allPolicies[0].id;
			}
		} catch (e) {
			loadError = (e as Error).message;
		}
	});

	$effect(() => {
		if (selected) {
			testInput = sampleInputs[selected.type as PolicyType] ?? '{}';
			testResult = null;
		}
	});

	async function runTest() {
		if (!selected) return;
		let parsed: unknown;
		try {
			parsed = JSON.parse(testInput);
		} catch {
			testResult = { ok: false, error: 'Invalid JSON in test input.' };
			return;
		}
		testing = true;
		testResult = null;
		try {
			testResult = await policies.test(selected.type, selected.body, parsed, traceEnabled);
		} catch (e) {
			testResult = { ok: false, error: (e as Error).message };
		} finally {
			testing = false;
		}
	}

	const typeLabels: Record<string, string> = {
		post_plan: 'Post-plan',
		pre_plan: 'Pre-plan',
		pre_apply: 'Pre-apply',
		trigger: 'Trigger',
		login: 'Login'
	};

	const typeBadge: Record<string, string> = {
		post_plan: 'bg-teal-900 text-teal-300',
		pre_plan: 'bg-sky-900 text-sky-300',
		pre_apply: 'bg-violet-900 text-violet-300',
		trigger: 'bg-amber-900 text-amber-300',
		login: 'bg-rose-900 text-rose-300'
	};
</script>

<div class="max-w-3xl space-y-6 p-6">
	<div class="flex items-center gap-2 text-sm text-zinc-500">
		<a href="/policies" class="hover:text-zinc-300">Policies</a>
		<span>/</span>
		<span class="text-white">Test playground</span>
	</div>

	{#if loadError}
		<p class="text-sm text-red-400">{loadError}</p>
	{:else if allPolicies.length === 0}
		<p class="text-sm text-zinc-500">No policies found. <a href="/policies" class="text-teal-400 hover:text-teal-300">Create one first.</a></p>
	{:else}
		<!-- Policy selector -->
		<div class="rounded-xl border border-zinc-800 bg-zinc-900 overflow-hidden">
			<div class="px-4 py-3 border-b border-zinc-800">
				<p class="text-xs text-zinc-500 uppercase tracking-widest">Policy</p>
			</div>
			<div class="px-4 py-3 flex items-center gap-3">
				<select
					bind:value={selectedID}
					class="flex-1 bg-zinc-800 border border-zinc-700 text-zinc-200 text-sm rounded-lg px-3 py-2 focus:outline-none focus:ring-2 focus:ring-teal-500"
				>
					{#each allPolicies as p (p.id)}
						<option value={p.id}>{p.name}</option>
					{/each}
				</select>
				{#if selected}
					<span class="text-xs rounded px-2 py-0.5 {typeBadge[selected.type] ?? 'bg-zinc-800 text-zinc-400'}">
						{typeLabels[selected.type] ?? selected.type}
					</span>
					<span class="text-xs {selected.is_active ? 'text-green-400' : 'text-zinc-500'}">
						{selected.is_active ? 'Active' : 'Inactive'}
					</span>
				{/if}
			</div>
		</div>

		<!-- Input editor -->
		<div class="rounded-xl border border-zinc-800 bg-zinc-900 overflow-hidden">
			<div class="px-4 py-3 border-b border-zinc-800 flex items-center justify-between">
				<p class="text-xs text-zinc-500 uppercase tracking-widest">Test input (JSON)</p>
				{#if selected}
					<button
						onclick={() => { testInput = sampleInputs[selected!.type as PolicyType] ?? '{}'; testResult = null; }}
						class="text-xs text-zinc-500 hover:text-zinc-300 transition-colors"
					>
						Reset to sample
					</button>
				{/if}
			</div>
			<div class="p-4">
				<textarea
					class="w-full bg-zinc-800 border border-zinc-700 text-zinc-100 font-mono text-xs rounded-lg px-3 py-2 focus:outline-none focus:ring-2 focus:ring-teal-500 resize-y"
					rows="18"
					bind:value={testInput}
					spellcheck="false"
				></textarea>
			</div>
		</div>

		<!-- Run controls -->
		<div class="flex items-center justify-between">
			<label class="flex cursor-pointer items-center gap-2 text-sm text-zinc-400 hover:text-zinc-200">
				<input type="checkbox" bind:checked={traceEnabled} class="accent-teal-500" />
				Include evaluation trace
			</label>
			<button
				onclick={runTest}
				disabled={testing || !selected}
				class="rounded-lg bg-teal-600 hover:bg-teal-500 disabled:opacity-50 text-white text-sm px-5 py-2 transition-colors"
			>
				{testing ? 'Running…' : 'Run test'}
			</button>
		</div>

		<!-- Results -->
		{#if testResult}
			<div class="rounded-xl border border-zinc-800 bg-zinc-900 overflow-hidden">
				<div class="px-4 py-3 border-b border-zinc-800">
					<p class="text-xs text-zinc-500 uppercase tracking-widest">Result</p>
				</div>

				{#if !testResult.ok}
					<div class="px-4 py-3">
						<p class="font-mono text-xs text-red-400">{testResult.error}</p>
					</div>
				{:else if testResult.result}
					{@const r = testResult.result}
					<div class="px-4 py-3 space-y-2">
						<p class="text-sm font-semibold {r.allow ? 'text-green-400' : 'text-red-400'}">
							{r.allow ? 'PASS — policy allows this operation' : 'BLOCKED — policy denied this operation'}
						</p>

						{#if r.deny && r.deny.length > 0}
							<div class="space-y-1">
								{#each r.deny as msg}
									<div class="flex items-start gap-2 rounded bg-red-950/50 border border-red-900/40 px-3 py-1.5">
										<span class="text-xs font-mono text-red-400 shrink-0">deny</span>
										<span class="text-xs text-red-300">{msg}</span>
									</div>
								{/each}
							</div>
						{/if}

						{#if r.warn && r.warn.length > 0}
							<div class="space-y-1">
								{#each r.warn as msg}
									<div class="flex items-start gap-2 rounded bg-amber-950/50 border border-amber-900/40 px-3 py-1.5">
										<span class="text-xs font-mono text-amber-400 shrink-0">warn</span>
										<span class="text-xs text-amber-300">{msg}</span>
									</div>
								{/each}
							</div>
						{/if}

						{#if r.trigger && r.trigger.length > 0}
							<div class="space-y-1">
								{#each r.trigger as id}
									<div class="flex items-start gap-2 rounded bg-teal-950/50 border border-teal-900/40 px-3 py-1.5">
										<span class="text-xs font-mono text-teal-400 shrink-0">trigger</span>
										<span class="text-xs font-mono text-teal-300">{id}</span>
									</div>
								{/each}
							</div>
						{/if}

						{#if r.require_approval}
							<div class="flex items-start gap-2 rounded bg-yellow-950/50 border border-yellow-900/40 px-3 py-1.5">
								<span class="text-xs font-mono text-yellow-400 shrink-0">require_approval</span>
								<span class="text-xs text-yellow-300">true</span>
							</div>
						{/if}

						{#if !r.deny?.length && !r.warn?.length && !r.trigger?.length && !r.require_approval}
							<p class="text-xs text-zinc-500">No messages — policy evaluated cleanly.</p>
						{/if}
					</div>

					{#if testResult.trace}
						<div class="border-t border-zinc-800">
							<details>
								<summary class="cursor-pointer px-4 py-3 text-xs text-zinc-500 hover:text-zinc-300 select-none">
									Evaluation trace ({testResult.trace.split('\n').length} lines)
								</summary>
								<pre class="max-h-96 overflow-auto border-t border-zinc-800 px-4 py-3 font-mono text-[11px] leading-relaxed text-zinc-400 whitespace-pre bg-zinc-950">{testResult.trace}</pre>
							</details>
						</div>
					{/if}
				{/if}
			</div>
		{/if}
	{/if}
</div>
