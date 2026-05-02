<script lang="ts">
	import { system, type HealthStatus } from '$lib/api/client';
	import { onMount } from 'svelte';
	import { page } from '$app/state';

	// Derive the Grafana base URL from the current origin so it works across
	// any deployment (localhost dev, custom domain, etc.).
	const grafanaBase = $derived(page.url.origin + '/grafana');

	const dashUID = 'crucible-main';

	// Common iframe query params: no controls, no time picker, transparent bg,
	// light-on-dark theme to match the Crucible UI, 1-hour window.
	const params = 'orgId=1&refresh=30s&theme=dark&from=now-1h&to=now&kiosk';

	const panels = [
		{ id: 1, title: 'HTTP Request Rate',          cols: 'col-span-2' },
		{ id: 2, title: 'HTTP Error Rate',             cols: 'col-span-1' },
		{ id: 3, title: 'HTTP Latency (p50/p95/p99)', cols: 'col-span-3' },
		{ id: 4, title: 'Run Completions by Status',  cols: 'col-span-2' },
		{ id: 5, title: 'Queue Depth',                cols: 'col-span-1' },
		{ id: 6, title: 'Active Runs',                cols: 'col-span-1' },
		{ id: 7, title: 'Stack Count',                cols: 'col-span-1' },
		{ id: 8, title: 'Run Success Rate (1 h)',     cols: 'col-span-1' },
	];

	let health = $state<HealthStatus | null>(null);

	onMount(async () => {
		try { health = await system.health(); } catch {}
	});

	function panelURL(panelID: number) {
		return `${grafanaBase}/d-solo/${dashUID}?panelId=${panelID}&${params}`;
	}
</script>

<div class="p-6 space-y-6 max-w-7xl mx-auto">
	<div class="flex items-center justify-between">
		<div>
			<h1 class="text-lg font-semibold text-white">Monitoring</h1>
			<p class="text-xs text-zinc-500 mt-0.5">Live metrics from Prometheus · refreshes every 30 s</p>
		</div>
		{#if health}
			<a href="{grafanaBase}/d/{dashUID}" target="_blank" rel="noopener noreferrer"
				class="text-xs text-teal-400 hover:text-teal-300 transition-colors">
				Open in Grafana ↗
			</a>
		{/if}
	</div>

	<div class="grid grid-cols-3 gap-4">
		{#each panels as panel}
			<div class="bg-zinc-900 border border-zinc-800 rounded-xl overflow-hidden {panel.cols}">
				<div class="px-4 py-2 border-b border-zinc-800">
					<span class="text-xs font-medium text-zinc-400">{panel.title}</span>
				</div>
				<iframe
					src={panelURL(panel.id)}
					title={panel.title}
					class="w-full h-52 border-0"
					loading="lazy"
				></iframe>
			</div>
		{/each}
	</div>

	<p class="text-xs text-zinc-600">
		Full dashboard and alerting available in
		<a href="{grafanaBase}" target="_blank" rel="noopener noreferrer"
			class="text-teal-400 hover:text-teal-300">Grafana</a>.
		Admin credentials are set via <code class="text-zinc-400">GRAFANA_ADMIN_USER</code> /
		<code class="text-zinc-400">GRAFANA_ADMIN_PASSWORD</code> in your <code class="text-zinc-400">.env</code>.
	</p>
</div>
