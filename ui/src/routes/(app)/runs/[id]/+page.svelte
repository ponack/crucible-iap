<script lang="ts">
	import { page } from '$app/state';
	import { onMount, onDestroy } from 'svelte';
	import { runs, type Run } from '$lib/api/client';
	import { auth } from '$lib/stores/auth.svelte';

	const runID = $derived(page.params.id as string);

	let run = $state<Run | null>(null);
	let logLines = $state<string[]>([]);
	let loading = $state(true);
	let error = $state<string | null>(null);
	let acting = $state<string | null>(null); // 'confirm' | 'discard' | 'cancel'

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
				// Refresh run to get final status
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
		};
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
				<span>Trigger: <span class="text-zinc-300">{run.trigger}</span></span>
				{#if run.branch}
					<span>Branch: <span class="text-zinc-300 font-mono">{run.branch}</span></span>
				{/if}
				<span>Queued: <span class="text-zinc-300">{fmtDate(run.queued_at)}</span></span>
				{#if run.started_at}
					<span>Duration: <span class="text-zinc-300">{duration(run.started_at, run.finished_at)}</span></span>
				{/if}
			</div>
		</div>

		<!-- Action buttons -->
		<div class="flex items-center gap-2 flex-shrink-0">
			{#if run.status === 'unconfirmed'}
				<button onclick={confirm} disabled={acting !== null}
					class="bg-green-700 hover:bg-green-600 disabled:opacity-50 text-white text-sm px-3 py-1.5 rounded-lg transition-colors">
					{acting === 'confirm' ? 'Confirming…' : 'Confirm & apply'}
				</button>
				<button onclick={discard} disabled={acting !== null}
					class="border border-zinc-700 hover:border-zinc-500 text-zinc-300 text-sm px-3 py-1.5 rounded-lg transition-colors disabled:opacity-50">
					{acting === 'discard' ? 'Discarding…' : 'Discard'}
				</button>
			{/if}
			{#if ['queued','preparing','planning','applying'].includes(run.status)}
				<button onclick={cancel} disabled={acting !== null}
					class="border border-red-900 hover:border-red-700 text-red-400 text-sm px-3 py-1.5 rounded-lg transition-colors disabled:opacity-50">
					{acting === 'cancel' ? 'Canceling…' : 'Cancel'}
				</button>
			{/if}
		</div>
	</div>

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
