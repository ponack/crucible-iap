<script lang="ts">
	import { onMount, onDestroy } from 'svelte';
	import { goto } from '$app/navigation';
	import { stacks, runs, audit, system, type Stack, type Run, type AuditEvent, type HealthStatus } from '$lib/api/client';

	let loading = $state(true);

	// Stats
	let totalStacks = $state(0);
	let disabledStacks = $state(0);
	let activeRuns = $state<Run[]>([]);
	let awaitingApproval = $state<Run[]>([]);
	let recentRuns = $state<Run[]>([]);
	let recentAudit = $state<AuditEvent[]>([]);
	let health = $state<HealthStatus | null>(null);

	// Inline action state
	let actioning = $state<Record<string, string>>({}); // runID → 'confirming'|'discarding'|'canceling'

	const failedRecent = $derived(recentRuns.filter((r) => r.status === 'failed'));
	const driftStacks = $derived(
		recentRuns
			.filter((r) => r.is_drift && r.status === 'finished' && ((r.plan_add ?? 0) + (r.plan_change ?? 0) + (r.plan_destroy ?? 0)) > 0)
			.reduce<string[]>((acc, r) => acc.includes(r.stack_name ?? '') ? acc : [...acc, r.stack_name ?? ''], [])
	);

	async function load() {
		try {
			const [stacksRes, activeRes, approvalRes, recentRes, auditRes, healthRes] = await Promise.all([
				stacks.list(0, 200),
				runs.listAll(0, 50), // all statuses; we filter to active client-side
				runs.listAll(0, 20, { status: 'unconfirmed' }),
				runs.listAll(0, 15),
				audit.list(0, 10),
				system.health()
			]);
			totalStacks = stacksRes.pagination.total;
			disabledStacks = stacksRes.data.filter((s) => s.is_disabled).length;
			// active = anything not terminal and not unconfirmed
			activeRuns = activeRes.data.filter((r) =>
				['queued','preparing','planning','confirmed','applying'].includes(r.status)
			);
			awaitingApproval = approvalRes.data;
			recentRuns = recentRes.data;
			recentAudit = auditRes.data;
			health = healthRes;
		} catch {
			// best-effort; partial data is fine
		} finally {
			loading = false;
		}
	}

	// Poll active runs every 10s so the dashboard stays live
	let pollTimer: ReturnType<typeof setInterval>;
	onMount(() => {
		load();
		pollTimer = setInterval(async () => {
			try {
				const [activeRes, approvalRes, recentRes] = await Promise.all([
					runs.listAll(0, 50),
					runs.listAll(0, 20, { status: 'unconfirmed' }),
					runs.listAll(0, 15)
				]);
				activeRuns = activeRes.data.filter((r) =>
					['queued','preparing','planning','confirmed','applying'].includes(r.status)
				);
				awaitingApproval = approvalRes.data;
				recentRuns = recentRes.data;
			} catch { /* ignore */ }
		}, 10_000);
	});
	onDestroy(() => clearInterval(pollTimer));

	async function confirm(run: Run) {
		actioning = { ...actioning, [run.id]: 'confirming' };
		try {
			await runs.confirm(run.id);
			awaitingApproval = awaitingApproval.filter((r) => r.id !== run.id);
		} catch (e) { alert((e as Error).message); }
		const { [run.id]: _, ...rest } = actioning; actioning = rest;
	}

	async function discard(run: Run) {
		actioning = { ...actioning, [run.id]: 'discarding' };
		try {
			await runs.discard(run.id);
			awaitingApproval = awaitingApproval.filter((r) => r.id !== run.id);
		} catch (e) { alert((e as Error).message); }
		const { [run.id]: _, ...rest } = actioning; actioning = rest;
	}

	async function cancel(run: Run) {
		actioning = { ...actioning, [run.id]: 'canceling' };
		try {
			await runs.cancel(run.id);
			activeRuns = activeRuns.filter((r) => r.id !== run.id);
		} catch (e) { alert((e as Error).message); }
		const { [run.id]: _, ...rest } = actioning; actioning = rest;
	}

	function fmtAgo(iso: string) {
		const s = Math.floor((Date.now() - new Date(iso).getTime()) / 1000);
		if (s < 60) return `${s}s ago`;
		if (s < 3600) return `${Math.floor(s / 60)}m ago`;
		if (s < 86400) return `${Math.floor(s / 3600)}h ago`;
		return `${Math.floor(s / 86400)}d ago`;
	}

	function fmtDuration(start: string | undefined, end: string | undefined) {
		if (!start) return '';
		const s = Math.floor((new Date(end ?? Date.now()).getTime() - new Date(start).getTime()) / 1000);
		if (s < 60) return `${s}s`;
		return `${Math.floor(s / 60)}m ${s % 60}s`;
	}

	function planDelta(r: Run) {
		const a = r.plan_add ?? 0, c = r.plan_change ?? 0, d = r.plan_destroy ?? 0;
		if (a + c + d === 0) return '';
		const parts = [];
		if (a) parts.push(`+${a}`);
		if (c) parts.push(`~${c}`);
		if (d) parts.push(`-${d}`);
		return parts.join(' ');
	}

	const statusColour: Record<string, string> = {
		queued:      'text-zinc-400',
		preparing:   'text-blue-400',
		planning:    'text-blue-400',
		unconfirmed: 'text-yellow-400',
		confirmed:   'text-blue-400',
		applying:    'text-blue-400',
		finished:    'text-green-400',
		failed:      'text-red-400',
		canceled:    'text-zinc-500',
		discarded:   'text-zinc-500'
	};

	const statusDot: Record<string, string> = {
		queued:      'bg-zinc-500',
		preparing:   'bg-blue-500 animate-pulse',
		planning:    'bg-blue-500 animate-pulse',
		unconfirmed: 'bg-yellow-500',
		confirmed:   'bg-blue-500',
		applying:    'bg-blue-500 animate-pulse',
		finished:    'bg-green-500',
		failed:      'bg-red-500',
		canceled:    'bg-zinc-600',
		discarded:   'bg-zinc-600'
	};

	function auditVerb(action: string) {
		const map: Record<string, string> = {
			'stack.created': 'created stack',
			'stack.updated': 'updated stack',
			'stack.deleted': 'deleted stack',
			'run.created': 'triggered run',
			'run.confirmed': 'approved run',
			'run.discarded': 'discarded run',
			'run.canceled': 'canceled run',
			'policy.created': 'created policy',
			'policy.updated': 'updated policy',
			'policy.deleted': 'deleted policy',
			'stack.remote_state.added': 'added remote state source',
			'stack.remote_state.removed': 'removed remote state source',
			'stack.token.created': 'created stack token',
			'stack.token.revoked': 'revoked stack token',
			'org.member.removed': 'removed member',
			'org.invite.created': 'sent invite',
			'org.invite.revoked': 'revoked invite'
		};
		return map[action] ?? action;
	}
</script>

<div class="p-6 space-y-6 max-w-5xl">

	<div class="flex items-center justify-between">
		<h1 class="text-xl font-semibold text-white">Dashboard</h1>
		{#if health}
			<span class="text-xs text-zinc-600 font-mono">
				{health.version === 'dev' ? 'dev build' : health.version}
				· up {health.uptime}
				{#if health.update_available}
					· <a href="https://github.com/ponack/crucible-iap/releases/latest" target="_blank" rel="noopener" class="text-yellow-400 hover:underline">{health.latest_version} available</a>
				{/if}
			</span>
		{/if}
	</div>

	{#if loading}
		<div class="text-zinc-500 text-sm py-12 text-center">Loading…</div>
	{:else}

	<!-- ── Stat cards ───────────────────────────────────────────────────────── -->
	<div class="grid grid-cols-2 sm:grid-cols-4 gap-3">
		<a href="/stacks" class="group border border-zinc-800 hover:border-zinc-700 rounded-xl p-4 space-y-1 transition-colors">
			<p class="text-2xl font-semibold text-white">{totalStacks}</p>
			<p class="text-xs text-zinc-500 group-hover:text-zinc-400 transition-colors">
				Stacks
				{#if disabledStacks > 0}
					<span class="text-zinc-600">· {disabledStacks} disabled</span>
				{/if}
			</p>
		</a>

		<a href="/runs" class="group border border-zinc-800 hover:border-zinc-700 rounded-xl p-4 space-y-1 transition-colors">
			<p class="text-2xl font-semibold {activeRuns.length > 0 ? 'text-blue-400' : 'text-white'}">{activeRuns.length}</p>
			<p class="text-xs text-zinc-500 group-hover:text-zinc-400 transition-colors">Active runs</p>
		</a>

		<button onclick={() => { if (awaitingApproval.length) document.getElementById('awaiting')?.scrollIntoView({ behavior: 'smooth' }); }}
			class="group text-left border {awaitingApproval.length > 0 ? 'border-yellow-800 hover:border-yellow-700' : 'border-zinc-800 hover:border-zinc-700'} rounded-xl p-4 space-y-1 transition-colors">
			<p class="text-2xl font-semibold {awaitingApproval.length > 0 ? 'text-yellow-400' : 'text-white'}">{awaitingApproval.length}</p>
			<p class="text-xs text-zinc-500 group-hover:text-zinc-400 transition-colors">Awaiting approval</p>
		</button>

		<a href="/runs" class="group border {failedRecent.length > 0 ? 'border-red-900 hover:border-red-800' : 'border-zinc-800 hover:border-zinc-700'} rounded-xl p-4 space-y-1 transition-colors">
			<p class="text-2xl font-semibold {failedRecent.length > 0 ? 'text-red-400' : 'text-white'}">{failedRecent.length}</p>
			<p class="text-xs text-zinc-500 group-hover:text-zinc-400 transition-colors">Failed (recent)</p>
		</a>
	</div>

	<!-- ── Drift alert banner ───────────────────────────────────────────────── -->
	{#if driftStacks.length > 0}
		<div class="border border-orange-800 bg-orange-950/40 rounded-xl px-5 py-3 flex items-center gap-3">
			<span class="text-orange-400 text-sm font-medium">Drift detected</span>
			<span class="text-orange-300 text-sm">
				{driftStacks.slice(0, 3).join(', ')}{driftStacks.length > 3 ? ` and ${driftStacks.length - 3} more` : ''} reported infrastructure changes on the last drift check.
			</span>
		</div>
	{/if}

	<!-- ── Awaiting approval ────────────────────────────────────────────────── -->
	{#if awaitingApproval.length > 0}
		<section id="awaiting" class="space-y-3">
			<h2 class="text-sm font-medium text-zinc-400 uppercase tracking-wide flex items-center gap-2">
				<span class="inline-block w-2 h-2 rounded-full bg-yellow-500"></span>
				Awaiting approval
			</h2>
			<div class="border border-yellow-900/60 rounded-xl overflow-hidden divide-y divide-zinc-800">
				{#each awaitingApproval as run (run.id)}
					{@const delta = planDelta(run)}
					<div class="flex items-center gap-3 px-4 py-3 hover:bg-zinc-900/50 transition-colors">
						<div class="flex-1 min-w-0">
							<div class="flex items-center gap-2 flex-wrap">
								<a href="/stacks/{run.stack_id}" class="text-sm text-zinc-200 hover:text-white font-medium truncate">{run.stack_name}</a>
								<span class="text-zinc-600 text-xs">·</span>
								<a href="/runs/{run.id}" class="text-xs text-zinc-500 hover:text-zinc-300 font-mono">{run.id.slice(0, 8)}</a>
								{#if delta}
									<span class="text-xs font-mono text-yellow-400">{delta}</span>
								{/if}
								{#if run.commit_sha}
									<span class="text-xs text-zinc-600 font-mono">{run.commit_sha.slice(0, 7)}</span>
								{/if}
							</div>
							{#if run.commit_message}
								<p class="text-xs text-zinc-500 truncate mt-0.5">{run.commit_message}</p>
							{/if}
						</div>
						<span class="text-xs text-zinc-600 shrink-0">{fmtAgo(run.queued_at)}</span>
						<div class="flex items-center gap-2 shrink-0">
							<button onclick={() => confirm(run)} disabled={!!actioning[run.id]}
								class="text-xs bg-green-900/60 hover:bg-green-800/60 text-green-300 px-3 py-1.5 rounded-lg transition-colors disabled:opacity-50">
								{actioning[run.id] === 'confirming' ? 'Approving…' : 'Approve'}
							</button>
							<button onclick={() => discard(run)} disabled={!!actioning[run.id]}
								class="text-xs border border-zinc-700 hover:border-zinc-500 text-zinc-400 hover:text-zinc-200 px-3 py-1.5 rounded-lg transition-colors disabled:opacity-50">
								{actioning[run.id] === 'discarding' ? 'Discarding…' : 'Discard'}
							</button>
						</div>
					</div>
				{/each}
			</div>
		</section>
	{/if}

	<!-- ── Active runs ──────────────────────────────────────────────────────── -->
	{#if activeRuns.length > 0}
		<section class="space-y-3">
			<h2 class="text-sm font-medium text-zinc-400 uppercase tracking-wide flex items-center gap-2">
				<span class="inline-block w-2 h-2 rounded-full bg-blue-500 animate-pulse"></span>
				Active runs
			</h2>
			<div class="border border-zinc-800 rounded-xl overflow-hidden divide-y divide-zinc-800">
				{#each activeRuns as run (run.id)}
					<div class="flex items-center gap-3 px-4 py-3 hover:bg-zinc-900/50 transition-colors">
						<span class="w-2 h-2 rounded-full flex-shrink-0 {statusDot[run.status] ?? 'bg-zinc-500'}"></span>
						<div class="flex-1 min-w-0">
							<div class="flex items-center gap-2 flex-wrap">
								<a href="/stacks/{run.stack_id}" class="text-sm text-zinc-200 hover:text-white font-medium">{run.stack_name}</a>
								<span class="text-zinc-600 text-xs">·</span>
								<a href="/runs/{run.id}" class="text-xs {statusColour[run.status]} capitalize">{run.status}</a>
								<span class="text-xs text-zinc-600 capitalize">{run.type}</span>
								{#if run.started_at}
									<span class="text-xs text-zinc-600">{fmtDuration(run.started_at, undefined)}</span>
								{/if}
							</div>
							{#if run.commit_message}
								<p class="text-xs text-zinc-500 truncate mt-0.5">{run.commit_message}</p>
							{/if}
						</div>
						<button onclick={() => cancel(run)} disabled={!!actioning[run.id]}
							class="text-xs text-zinc-600 hover:text-red-400 transition-colors disabled:opacity-50 shrink-0">
							{actioning[run.id] === 'canceling' ? 'Canceling…' : 'Cancel'}
						</button>
					</div>
				{/each}
			</div>
		</section>
	{/if}

	<!-- ── Recent runs + Audit feed ────────────────────────────────────────── -->
	<div class="grid grid-cols-1 lg:grid-cols-2 gap-6">

		<!-- Recent runs -->
		<section class="space-y-3">
			<div class="flex items-center justify-between">
				<h2 class="text-sm font-medium text-zinc-400 uppercase tracking-wide">Recent runs</h2>
				<a href="/runs" class="text-xs text-zinc-600 hover:text-zinc-400 transition-colors">View all →</a>
			</div>
			{#if recentRuns.length === 0}
				<p class="text-zinc-600 text-sm">No runs yet.</p>
			{:else}
				<div class="border border-zinc-800 rounded-xl overflow-hidden divide-y divide-zinc-800">
					{#each recentRuns as run (run.id)}
						{@const delta = planDelta(run)}
						<a href="/runs/{run.id}" class="flex items-center gap-3 px-4 py-2.5 hover:bg-zinc-900/50 transition-colors group">
							<span class="w-1.5 h-1.5 rounded-full flex-shrink-0 mt-0.5 {statusDot[run.status] ?? 'bg-zinc-500'}"></span>
							<div class="flex-1 min-w-0">
								<div class="flex items-center gap-1.5 flex-wrap">
									<span class="text-sm text-zinc-200 group-hover:text-white truncate">{run.stack_name}</span>
									<span class="text-xs {statusColour[run.status]} capitalize">{run.status}</span>
									{#if delta}
										<span class="text-xs font-mono text-zinc-400">{delta}</span>
									{/if}
								</div>
								{#if run.commit_message}
									<p class="text-xs text-zinc-600 truncate">{run.commit_message}</p>
								{/if}
							</div>
							<span class="text-xs text-zinc-600 shrink-0">{fmtAgo(run.queued_at)}</span>
						</a>
					{/each}
				</div>
			{/if}
		</section>

		<!-- Audit feed -->
		<section class="space-y-3">
			<div class="flex items-center justify-between">
				<h2 class="text-sm font-medium text-zinc-400 uppercase tracking-wide">Recent activity</h2>
				<a href="/audit" class="text-xs text-zinc-600 hover:text-zinc-400 transition-colors">View all →</a>
			</div>
			{#if recentAudit.length === 0}
				<p class="text-zinc-600 text-sm">No activity yet.</p>
			{:else}
				<div class="border border-zinc-800 rounded-xl overflow-hidden divide-y divide-zinc-800">
					{#each recentAudit as ev (ev.id)}
						<div class="flex items-start gap-3 px-4 py-2.5">
							<div class="w-6 h-6 rounded-full bg-zinc-800 flex items-center justify-center flex-shrink-0 mt-0.5">
								<span class="text-zinc-400 text-xs">{(ev.actor_id ?? '?')[0]?.toUpperCase()}</span>
							</div>
							<div class="flex-1 min-w-0">
								<p class="text-sm text-zinc-300 truncate">{auditVerb(ev.action)}</p>
								{#if ev.resource_id}
									<p class="text-xs text-zinc-600 font-mono truncate">{ev.resource_id.slice(0, 8)}</p>
								{/if}
							</div>
							<span class="text-xs text-zinc-600 shrink-0">{fmtAgo(ev.occurred_at)}</span>
						</div>
					{/each}
				</div>
			{/if}
		</section>
	</div>

	<!-- ── Empty state (no stacks yet) ─────────────────────────────────────── -->
	{#if totalStacks === 0}
		<div class="border border-zinc-800 rounded-xl p-12 text-center space-y-4">
			<p class="text-zinc-300 text-base font-medium">Welcome to Crucible IAP</p>
			<p class="text-zinc-500 text-sm max-w-sm mx-auto">Get started by creating your first stack. A stack connects a repository to a Terraform/OpenTofu workspace and manages its full run lifecycle.</p>
			<a href="/stacks/new"
				class="inline-block bg-indigo-600 hover:bg-indigo-500 text-white text-sm px-5 py-2 rounded-lg transition-colors">
				Create your first stack →
			</a>
		</div>
	{/if}

	{/if}
</div>
