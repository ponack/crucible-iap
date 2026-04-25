<script lang="ts">
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { onMount, onDestroy } from 'svelte';
	import { runs, type Run, type RunPolicyResult } from '$lib/api/client';
	import { auth } from '$lib/stores/auth.svelte';

	const runID = $derived(page.params.id as string);

	let run = $state<Run | null>(null);
	let logLines = $state<string[]>([]);
	let loading = $state(true);
	let error = $state<string | null>(null);
	let acting = $state<string | null>(null); // 'approve' | 'confirm' | 'discard' | 'cancel'
	let editingAnnotation = $state(false);
	let annotationDraft = $state('');
	let policyResults = $state<RunPolicyResult[]>([]);

	let logEl = $state<HTMLElement | undefined>(undefined);
	let sse: EventSource | null = null;
	let autoScroll = $state(true);

	const terminalStatuses = new Set(['finished', 'failed', 'canceled', 'discarded']);

	onMount(async () => {
		try {
			run = await runs.get(runID);
		} catch (e) {
			error = (e as Error).message;
			loading = false;
			return;
		}
		loading = false;
		startSSE();
		runs.policyResults(runID).then((r) => (policyResults = r)).catch(() => {});
	});

	onDestroy(() => sse?.close());

	function startSSE() {
		if (!run) return;

		const token = auth.accessToken;
		// EventSource doesn't support headers — use query param for token
		sse = new EventSource(`/api/v1/runs/${runID}/logs?token=${token}`);

		sse.onmessage = (e) => {
			if (e.data === '[DONE]') {
				sse?.close();
				sse = null;
				runs.get(runID).then((r) => (run = r)).catch(() => {});
				return;
			}
			logLines = [...logLines, e.data];
			const el = logEl;
			if (autoScroll && el) {
				requestAnimationFrame(() => el.scrollTo({ top: el.scrollHeight }));
			}
		};

		sse.onerror = () => {
			sse?.close();
			sse = null;
			// Refresh run status — the connection may have dropped at end of run
			// before [DONE] was delivered (e.g. network blip or proxy timeout).
			runs.get(runID).then((r) => (run = r)).catch(() => {});
		};
	}

	async function approve() {
		acting = 'approve';
		try {
			await runs.approve(runID);
			run = await runs.get(runID);
		} catch (e) {
			alert((e as Error).message);
		} finally {
			acting = null;
		}
	}

	async function confirm() {
		acting = 'confirm';
		try {
			await runs.confirm(runID);
			run = await runs.get(runID);
			startSSE();
		} catch (e) {
			alert((e as Error).message);
		} finally {
			acting = null;
		}
	}

	async function discard() {
		acting = 'discard';
		try {
			await runs.discard(runID);
			run = await runs.get(runID);
		} catch (e) {
			alert((e as Error).message);
		} finally {
			acting = null;
		}
	}

	async function cancel() {
		acting = 'cancel';
		try {
			await runs.cancel(runID);
			run = await runs.get(runID);
		} catch (e) {
			alert((e as Error).message);
		} finally {
			acting = null;
		}
	}

	function fmtDate(iso?: string) {
		return iso ? new Date(iso).toLocaleString() : '—';
	}

	function downloadLog() {
		if (logLines.length === 0) return;
		const blob = new Blob([logLines.join('\n')], { type: 'text/plain' });
		const url = URL.createObjectURL(blob);
		const a = document.createElement('a');
		a.href = url;
		a.download = `run-${runID.slice(0, 8)}.log`;
		a.click();
		URL.revokeObjectURL(url);
	}

	async function deleteRun() {
		if (!window.confirm('Delete this run and its artifacts? This cannot be undone.')) return;
		try {
			await runs.remove(runID);
			goto('/runs');
		} catch (e) {
			alert((e as Error).message);
		}
	}

	async function downloadPlan() {
		try {
			const blob = await runs.downloadPlan(runID);
			const url = URL.createObjectURL(blob);
			const a = document.createElement('a');
			a.href = url;
			a.download = `run-${runID.slice(0, 8)}.tfplan`;
			a.click();
			URL.revokeObjectURL(url);
		} catch (e) {
			alert((e as Error).message);
		}
	}

	async function saveAnnotation() {
		editingAnnotation = false;
		if (!run || annotationDraft === (run.annotation ?? '')) return;
		try {
			await runs.annotate(runID, annotationDraft);
			run = { ...run, annotation: annotationDraft || undefined };
		} catch (e) {
			alert((e as Error).message);
		}
	}

	function duration(start?: string, end?: string) {
		if (!start) return null;
		const ms = new Date(end ?? new Date()).getTime() - new Date(start).getTime();
		const s = Math.round(ms / 1000);
		return s < 60 ? `${s}s` : `${Math.floor(s / 60)}m ${s % 60}s`;
	}

	const statusColour: Record<string, string> = {
		queued: 'bg-zinc-800 text-zinc-300',
		preparing: 'bg-blue-900 text-blue-300',
		planning: 'bg-blue-900 text-blue-300',
		unconfirmed: 'bg-yellow-900 text-yellow-300',
		pending_approval: 'bg-purple-900 text-purple-300',
		confirmed: 'bg-blue-900 text-blue-300',
		applying: 'bg-blue-900 text-blue-300',
		finished: 'bg-green-900 text-green-300',
		failed: 'bg-red-900 text-red-300',
		canceled: 'bg-zinc-800 text-zinc-400',
		discarded: 'bg-zinc-800 text-zinc-400'
	};
</script>

{#if loading}
	<div class="p-6 text-zinc-500 text-sm">Loading…</div>
{:else if error || !run}
	<div class="p-6 text-red-400 text-sm">{error ?? 'Run not found'}</div>
{:else}
<div class="flex flex-col h-full">

	<!-- Pending approval banner -->
	{#if run.status === 'pending_approval'}
		<div class="flex-shrink-0 bg-purple-950 border-b border-purple-900 px-6 py-3 flex items-center gap-3">
			<span class="text-purple-300 text-sm font-medium">Approval required.</span>
			<span class="text-purple-400 text-xs">A policy requires explicit sign-off before this run can proceed. Review the plan output and policy results below.</span>
		</div>
	{/if}

	<!-- Destroy warning banner -->
	{#if run.type === 'destroy' && run.status === 'unconfirmed'}
		<div class="flex-shrink-0 bg-orange-950 border-b border-orange-900 px-6 py-3 flex items-center gap-3">
			<span class="text-orange-300 text-sm font-medium">Destroy plan ready.</span>
			<span class="text-orange-400 text-xs">Review the plan output below, then confirm to permanently destroy all managed infrastructure.</span>
		</div>
	{/if}

	<!-- Header bar -->
	<div class="flex-shrink-0 border-b border-zinc-800 px-6 py-4 flex items-start justify-between gap-4">
		<div class="space-y-1">
			<div class="flex items-center gap-2 text-sm">
				<a href="/stacks/{run.stack_id}" class="text-zinc-500 hover:text-zinc-300">Stack</a>
				<span class="text-zinc-700">/</span>
				<span class="text-zinc-400">Run</span>
				<span class="text-zinc-700">/</span>
				<span class="text-zinc-400 font-mono text-xs">{run.id.slice(0, 8)}</span>
				<span class="px-1.5 py-0.5 rounded text-xs font-medium {statusColour[run.status] ?? 'bg-zinc-800 text-zinc-400'}">
					{run.status}
				</span>
			</div>
			<div class="flex items-center gap-4 text-xs text-zinc-500">
				<span>Type: <span class="text-zinc-300">{run.type}</span></span>
				{#if run.plan_add !== undefined || run.plan_change !== undefined || run.plan_destroy !== undefined}
					<span class="flex items-center gap-1.5 font-mono">
						<span class="text-green-400">+{run.plan_add ?? 0}</span>
						<span class="text-yellow-400">~{run.plan_change ?? 0}</span>
						<span class="text-red-400">-{run.plan_destroy ?? 0}</span>
					</span>
				{/if}
				{#if run.cost_add !== undefined || run.cost_change !== undefined || run.cost_remove !== undefined}
					{@const cur = run.cost_currency ?? 'USD'}
					{@const fmt = (n: number) => n.toLocaleString('en-US', { style: 'currency', currency: cur, maximumFractionDigits: 2 })}
					<span class="flex items-center gap-1 text-zinc-400 font-mono" title="Infracost monthly estimate">
						<span class="text-xs text-zinc-600">$/mo</span>
						{#if (run.cost_add ?? 0) !== 0}<span class="text-green-400">+{fmt(run.cost_add ?? 0)}</span>{/if}
						{#if (run.cost_change ?? 0) !== 0}<span class="text-yellow-400">~{fmt(run.cost_change ?? 0)}</span>{/if}
						{#if (run.cost_remove ?? 0) !== 0}<span class="text-red-400">-{fmt(run.cost_remove ?? 0)}</span>{/if}
					</span>
				{/if}
				<span>Trigger: <span class="text-zinc-300">{run.trigger}</span></span>
				{#if run.triggered_by_name}
					<span>By: <span class="text-zinc-300">{run.triggered_by_name}</span></span>
				{/if}
				{#if run.branch}
					<span>Branch: <span class="text-zinc-300 font-mono">{run.branch}</span></span>
				{/if}
				<span>Queued: <span class="text-zinc-300">{fmtDate(run.queued_at)}</span></span>
				{#if run.started_at}
					<span>Duration: <span class="text-zinc-300">{duration(run.started_at, run.finished_at)}</span></span>
				{/if}
			</div>
			{#if run.commit_message}
				<div class="text-xs text-zinc-500 font-mono truncate max-w-xl" title={run.commit_message}>
					{run.commit_sha ? run.commit_sha.slice(0, 7) + ' ' : ''}{run.commit_message}
				</div>
			{/if}
			{#if run.approved_by_name}
				<div class="text-xs text-zinc-500">
					Approved by <span class="text-zinc-300">{run.approved_by_name}</span>
					{#if run.approved_at}<span> · {fmtDate(run.approved_at)}</span>{/if}
				</div>
			{/if}
			{#if run.annotation || run.my_stack_role !== 'viewer'}
				<div class="text-xs text-zinc-500 flex items-center gap-1.5">
					<span>Note:</span>
					{#if editingAnnotation}
						<input
							type="text"
							class="bg-zinc-900 border border-zinc-700 rounded px-2 py-0.5 text-zinc-200 text-xs focus:outline-none focus:border-zinc-500 w-64"
							bind:value={annotationDraft}
							onkeydown={(e) => { if (e.key === 'Enter') saveAnnotation(); if (e.key === 'Escape') { editingAnnotation = false; } }}
							onblur={saveAnnotation}
						/>
					{:else}
						<span
							class="text-zinc-300 cursor-pointer hover:text-white"
							onclick={() => { annotationDraft = run?.annotation ?? ''; editingAnnotation = true; }}
							role="button"
							tabindex="0"
							onkeydown={(e) => { if (e.key === 'Enter') { annotationDraft = run?.annotation ?? ''; editingAnnotation = true; } }}
						>
							{run.annotation ?? 'Add note…'}
						</span>
					{/if}
				</div>
			{/if}
		</div>

		<!-- Action buttons -->
		<div class="flex items-center gap-2 flex-shrink-0">
			{#if run.has_plan}
				<button onclick={downloadPlan}
					class="border border-zinc-700 hover:border-zinc-500 text-zinc-400 hover:text-zinc-200 text-sm px-3 py-1.5 rounded-lg transition-colors">
					Plan file
				</button>
			{/if}
			{#if auth.isAdmin && terminalStatuses.has(run.status)}
				<button onclick={deleteRun}
					class="border border-red-900 hover:border-red-700 text-red-400 text-sm px-3 py-1.5 rounded-lg transition-colors">
					Delete
				</button>
			{/if}
			{#if run.status === 'pending_approval' && run.my_stack_role !== 'viewer'}
				<button onclick={approve} disabled={acting !== null}
					class="bg-purple-700 hover:bg-purple-600 disabled:opacity-50 text-white text-sm px-3 py-1.5 rounded-lg transition-colors font-medium">
					{acting === 'approve' ? 'Approving…' : 'Approve'}
				</button>
				<button onclick={cancel} disabled={acting !== null}
					class="border border-zinc-700 hover:border-zinc-500 text-zinc-300 text-sm px-3 py-1.5 rounded-lg transition-colors disabled:opacity-50">
					{acting === 'cancel' ? 'Canceling…' : 'Discard'}
				</button>
			{/if}
			{#if run.status === 'unconfirmed' && run.my_stack_role !== 'viewer'}
				{#if run.type === 'destroy'}
					<button onclick={confirm} disabled={acting !== null}
						class="bg-orange-700 hover:bg-orange-600 disabled:opacity-50 text-white text-sm px-3 py-1.5 rounded-lg transition-colors font-medium">
						{acting === 'confirm' ? 'Confirming…' : 'Confirm destroy'}
					</button>
				{:else}
					<button onclick={confirm} disabled={acting !== null}
						class="bg-green-700 hover:bg-green-600 disabled:opacity-50 text-white text-sm px-3 py-1.5 rounded-lg transition-colors">
						{acting === 'confirm' ? 'Confirming…' : 'Confirm & apply'}
					</button>
				{/if}
				<button onclick={discard} disabled={acting !== null}
					class="border border-zinc-700 hover:border-zinc-500 text-zinc-300 text-sm px-3 py-1.5 rounded-lg transition-colors disabled:opacity-50">
					{acting === 'discard' ? 'Discarding…' : 'Discard'}
				</button>
			{/if}
			{#if ['queued','preparing','planning','applying'].includes(run.status) && run.my_stack_role !== 'viewer'}
				<button onclick={cancel} disabled={acting !== null}
					class="border border-red-900 hover:border-red-700 text-red-400 text-sm px-3 py-1.5 rounded-lg transition-colors disabled:opacity-50">
					{acting === 'cancel' ? 'Canceling…' : 'Cancel'}
				</button>
			{/if}
		</div>
	</div>

	<!-- Policy results (shown only when at least one policy was evaluated) -->
	{#if policyResults.length > 0}
		<div class="flex-shrink-0 border-b border-zinc-800 px-6 py-3 space-y-2">
			<p class="text-xs font-medium text-zinc-500 uppercase tracking-wide">Policy evaluation</p>
			<div class="flex flex-wrap gap-2">
				{#each policyResults as r}
					<div class="flex items-start gap-2 rounded-lg border px-3 py-2 text-xs
						{r.allow
							? 'border-green-900 bg-green-950/50'
							: 'border-red-900 bg-red-950/50'}">
						<div class="space-y-0.5">
							<div class="flex items-center gap-1.5">
								<span class="font-medium {r.allow ? 'text-green-300' : 'text-red-300'}">
									{r.allow ? '✓' : '✗'} {r.policy_name}
								</span>
								<span class="text-zinc-600">{r.hook}</span>
							</div>
							{#each r.deny_msgs as msg}
								<p class="text-red-400 font-mono">{msg}</p>
							{/each}
							{#each r.warn_msgs as msg}
								<p class="text-amber-400 font-mono">⚠ {msg}</p>
							{/each}
						</div>
					</div>
				{/each}
			</div>
		</div>
	{/if}

	<!-- Log viewer -->
	<div class="flex-1 flex flex-col min-h-0">
		<div class="flex items-center justify-between px-4 py-2 bg-zinc-950 border-b border-zinc-800">
			<span class="text-xs text-zinc-500 font-mono">Run output</span>
			<div class="flex items-center gap-3">
				{#if logLines.length > 0}
					<button onclick={downloadLog}
						class="text-xs text-zinc-500 hover:text-zinc-300 transition-colors">
						Download
					</button>
				{/if}
				<label class="flex items-center gap-1.5 text-xs text-zinc-500 cursor-pointer">
					<input type="checkbox" bind:checked={autoScroll} class="rounded" />
					Auto-scroll
				</label>
			</div>
		</div>
		<div bind:this={logEl}
			class="flex-1 overflow-y-auto bg-zinc-950 px-4 py-3 font-mono text-xs text-zinc-300 leading-relaxed">
			{#if logLines.length === 0}
				{#if run.status === 'queued'}
					<span class="text-zinc-600 animate-pulse">Waiting for output…</span>
				{:else if terminalStatuses.has(run.status)}
					<span class="text-zinc-600">No log output recorded.</span>
				{:else}
					<span class="text-zinc-600 animate-pulse">Waiting for output…</span>
				{/if}
			{:else}
				{#each logLines as line, i (i)}
					<div class="whitespace-pre-wrap break-all">{line}</div>
				{/each}
			{/if}
		</div>
	</div>

</div>
{/if}
