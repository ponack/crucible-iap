<script lang="ts">
	import { onMount } from 'svelte';
	import { downloadExport } from '$lib/api/compliance-export';
	import { projects as projectsApi, orgTags, type Project, type Tag } from '$lib/api/client';
	import { auth } from '$lib/stores/auth.svelte';
	import { toast } from '$lib/stores/toasts.svelte';

	// Default window: the previous calendar quarter, since that's the most
	// common audit interval. Operators override via the date inputs.
	function defaultRange(): { start: string; end: string } {
		const now = new Date();
		const start = new Date(Date.UTC(now.getUTCFullYear(), now.getUTCMonth() - 3, 1));
		const end = new Date(Date.UTC(now.getUTCFullYear(), now.getUTCMonth(), 1));
		return { start: start.toISOString().slice(0, 10), end: end.toISOString().slice(0, 10) };
	}

	const initial = defaultRange();
	let start = $state(initial.start);
	let end = $state(initial.end);
	let projectID = $state('');
	let selectedTags = $state<string[]>([]);

	let allProjects = $state<Project[]>([]);
	let allTags = $state<Tag[]>([]);
	let busy = $state(false);
	let lastError = $state<string | null>(null);

	onMount(async () => {
		const [pRes, tRes] = await Promise.allSettled([projectsApi.list(), orgTags.list()]);
		if (pRes.status === 'fulfilled') allProjects = pRes.value;
		if (tRes.status === 'fulfilled') allTags = tRes.value;
	});

	function toggleTag(name: string) {
		selectedTags = selectedTags.includes(name)
			? selectedTags.filter((t) => t !== name)
			: [...selectedTags, name];
	}

	async function run(e: SubmitEvent) {
		e.preventDefault();
		busy = true;
		lastError = null;
		try {
			await downloadExport({
				start: new Date(start + 'T00:00:00Z').toISOString(),
				end: new Date(end + 'T00:00:00Z').toISOString(),
				project_id: projectID || undefined,
				tags: selectedTags.length ? selectedTags : undefined
			});
			toast.success('Export downloaded.');
		} catch (err) {
			lastError = (err as Error).message;
		} finally {
			busy = false;
		}
	}
</script>

<div class="space-y-6 max-w-2xl">
	<div>
		<h1 class="text-lg font-semibold text-white">Compliance export</h1>
		<p class="mt-1 text-sm text-zinc-500">
			Generate a downloadable bundle for SOC 2 / HIPAA / PCI evidence. The ZIP contains runs, audit events, policy results, and approval records for the window — in both CSV and JSON — plus an HMAC-signed manifest so the recipient can verify the bundle wasn't tampered with.
		</p>
	</div>

	{#if !auth.isAdmin}
		<div class="rounded-lg border border-amber-800 bg-amber-950/30 px-4 py-3 text-sm text-amber-300">
			Only org admins can generate compliance exports.
		</div>
	{:else}
		<form onsubmit={run} class="space-y-5 rounded-xl border border-zinc-800 p-5">
			{#if lastError}
				<div class="rounded-lg border border-red-800 bg-red-950 px-4 py-3 text-sm text-red-300">{lastError}</div>
			{/if}

			<div class="grid grid-cols-2 gap-4">
				<div class="space-y-1.5">
					<label class="field-label" for="exp-start">Start (UTC)</label>
					<input id="exp-start" type="date" class="field-input" bind:value={start} required />
				</div>
				<div class="space-y-1.5">
					<label class="field-label" for="exp-end">End (UTC, exclusive)</label>
					<input id="exp-end" type="date" class="field-input" bind:value={end} required />
				</div>
			</div>

			<div class="space-y-1.5">
				<label class="field-label" for="exp-project">Project (optional)</label>
				<select id="exp-project" class="field-input" bind:value={projectID}>
					<option value="">All projects</option>
					{#each allProjects as p (p.id)}
						<option value={p.id}>{p.name}</option>
					{/each}
				</select>
			</div>

			{#if allTags.length > 0}
				<div class="space-y-1.5">
					<span class="field-label">Tags (optional — any-of match)</span>
					<div class="flex flex-wrap gap-2">
						{#each allTags as tag (tag.id)}
							{@const active = selectedTags.includes(tag.name)}
							<button
								type="button"
								onclick={() => toggleTag(tag.name)}
								class="inline-flex items-center gap-1.5 rounded-lg px-2.5 py-1 text-xs transition-colors"
								style="border: 1px solid {active ? 'var(--accent)' : 'var(--color-zinc-700)'}; color: {active ? 'var(--accent)' : 'var(--color-zinc-300)'}; background: {active ? 'var(--accent-muted)' : 'transparent'};">
								<span class="w-2 h-2 rounded-full" style="background: {tag.color};"></span>
								{tag.name}
							</button>
						{/each}
					</div>
				</div>
			{/if}

			<div class="flex items-center justify-between pt-2">
				<p class="text-xs text-zinc-500">
					Exports run synchronously. Large windows over many stacks may take 10–30s.
				</p>
				<button
					type="submit"
					disabled={busy}
					class="rounded-lg bg-teal-600 px-4 py-1.5 text-sm font-medium text-white transition-colors hover:bg-teal-500 disabled:opacity-50">
					{busy ? 'Building bundle…' : 'Generate export'}
				</button>
			</div>
		</form>

		<div class="rounded-xl border border-zinc-800 p-5 space-y-2">
			<h2 class="text-sm font-medium text-zinc-300">What's in the bundle</h2>
			<ul class="text-xs text-zinc-500 space-y-1 list-disc list-inside">
				<li><code class="text-zinc-300">runs.csv</code> / <code class="text-zinc-300">runs.json</code> — one row per run with status, type, trigger, commit, who triggered/approved, plan counts, cost change.</li>
				<li><code class="text-zinc-300">audit.csv</code> / <code class="text-zinc-300">audit.json</code> — every audit event in the window.</li>
				<li><code class="text-zinc-300">policy-results.json</code> — pre/post/apply policy evaluations for runs in the window.</li>
				<li><code class="text-zinc-300">approvals.json</code> — per-step chain approvals.</li>
				<li><code class="text-zinc-300">manifest.json</code> — summary + counts + schema version.</li>
				<li><code class="text-zinc-300">manifest.json.sig</code> — HMAC-SHA256 of <code class="text-zinc-300">manifest.json</code> with your instance secret. Verify with <code class="text-zinc-300">openssl dgst -sha256 -hmac "$SECRET" manifest.json</code>.</li>
			</ul>
		</div>
	{/if}
</div>
