<script lang="ts">
	import { orgQuotas, type OrgQuota, type OrgQuotaStatus } from '$lib/api/client';
	import { onMount } from 'svelte';
	import { toast } from '$lib/stores/toasts.svelte';
	import { auth } from '$lib/stores/auth.svelte';

	let quota = $state<OrgQuota | null>(null);
	let status = $state<OrgQuotaStatus | null>(null);
	let loading = $state(true);
	let saving = $state(false);

	// Form state — drives the input. `null` / empty string both mean unlimited.
	let capInput = $state<string>('');
	let capEnabled = $state(false);

	const isAdmin = $derived(auth.isAdmin);

	async function load() {
		loading = true;
		try {
			const [q, s] = await Promise.all([orgQuotas.get(), orgQuotas.status()]);
			quota = q;
			status = s;
			capEnabled = q.max_concurrent_runs !== null;
			capInput = q.max_concurrent_runs?.toString() ?? '';
		} catch (e) {
			toast.error((e as Error).message);
		} finally {
			loading = false;
		}
	}

	onMount(load);

	async function save() {
		saving = true;
		try {
			const cap = capEnabled && capInput.trim() !== '' ? parseInt(capInput, 10) : null;
			if (cap !== null && (Number.isNaN(cap) || cap < 0)) {
				toast.error('Cap must be a non-negative integer');
				saving = false;
				return;
			}
			await orgQuotas.update(cap);
			toast.success('Quota saved');
			await load();
		} catch (e) {
			toast.error((e as Error).message);
		} finally {
			saving = false;
		}
	}
</script>

<div class="max-w-2xl space-y-6">
	<header>
		<h1 class="text-xl font-semibold text-zinc-100">Resource quotas</h1>
		<p class="text-sm text-zinc-500 mt-1">
			Limit how many runs can be active at once for this organisation. Useful for shared deployments where you want to prevent any single org from monopolising the worker.
		</p>
	</header>

	{#if loading}
		<p class="text-sm text-zinc-500">Loading…</p>
	{:else}
		<!-- Current usage card -->
		<section class="border border-zinc-800 rounded-lg p-4 bg-zinc-950/50">
			<p class="text-xs text-zinc-500 uppercase tracking-wide mb-2">Current usage</p>
			<div class="flex items-baseline gap-2">
				<span class="text-2xl font-mono text-zinc-100">{status?.active_concurrent_runs ?? 0}</span>
				<span class="text-zinc-500">of</span>
				<span class="text-2xl font-mono text-zinc-300">
					{status?.max_concurrent_runs ?? '∞'}
				</span>
				<span class="text-zinc-500">concurrent runs</span>
			</div>
			{#if status?.max_concurrent_runs !== null && status?.max_concurrent_runs !== undefined}
				{@const pct = Math.min(
					100,
					(100 * status.active_concurrent_runs) / Math.max(1, status.max_concurrent_runs)
				)}
				<div class="mt-3 h-1.5 bg-zinc-800 rounded-full overflow-hidden">
					<div
						class="h-full rounded-full transition-all
							{pct >= 100 ? 'bg-red-500' : pct >= 80 ? 'bg-amber-500' : 'bg-teal-500'}"
						style="width: {pct}%"
					></div>
				</div>
			{/if}
		</section>

		<!-- Cap editor -->
		<section class="space-y-4">
			<header>
				<h2 class="text-base font-medium text-zinc-100">Concurrent run cap</h2>
				<p class="text-xs text-zinc-500 mt-1">
					Counts runs in queued, preparing, planning, applying, unconfirmed, or pending_approval state. New runs that would exceed the cap are rejected with HTTP 429.
				</p>
			</header>

			<label class="flex items-center gap-2 text-sm text-zinc-300">
				<input
					type="checkbox"
					bind:checked={capEnabled}
					disabled={!isAdmin}
					class="rounded"
				/>
				Enforce a concurrent run cap
			</label>

			{#if capEnabled}
				<div class="flex items-center gap-3">
					<input
						type="number"
						min="0"
						bind:value={capInput}
						placeholder="e.g. 10"
						disabled={!isAdmin}
						class="bg-zinc-900 border border-zinc-700 rounded px-3 py-1.5 text-zinc-200 w-32 focus:outline-none focus:border-zinc-500 disabled:opacity-50"
					/>
					<span class="text-sm text-zinc-500">maximum concurrent runs</span>
				</div>
				<p class="text-xs text-zinc-500">
					Set to <code class="text-zinc-300">0</code> to block all new runs (useful for maintenance windows).
				</p>
			{/if}

			{#if isAdmin}
				<div class="flex items-center gap-3 pt-2">
					<button
						onclick={save}
						disabled={saving}
						class="bg-teal-700 hover:bg-teal-600 disabled:opacity-50 text-white text-sm px-4 py-1.5 rounded-lg transition-colors"
					>
						{saving ? 'Saving…' : 'Save'}
					</button>
					{#if quota?.updated_at}
						<span class="text-xs text-zinc-500">
							Last updated {new Date(quota.updated_at).toLocaleString()}
						</span>
					{/if}
				</div>
			{:else}
				<p class="text-xs text-zinc-500">Read-only — admin role required to change quotas.</p>
			{/if}
		</section>
	{/if}
</div>
