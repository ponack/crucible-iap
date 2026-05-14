<script lang="ts">
	import { onMount } from 'svelte';
	import {
		siemApi,
		type SIEMDestination,
		type SIEMDestinationType,
		type SIEMDelivery
	} from '$lib/api/siem';

	// ── State ─────────────────────────────────────────────────────────────────

	let destinations = $state<SIEMDestination[]>([]);
	let deliveries = $state<SIEMDelivery[]>([]);
	let loading = $state(true);
	let showModal = $state(false);
	let editingDest = $state<SIEMDestination | null>(null);
	let testingID = $state<string | null>(null);
	let testResult = $state<{ ok: boolean; error?: string } | null>(null);
	let showDeliveries = $state(false);
	let deliveryFilter = $state('');

	// ── Form state ────────────────────────────────────────────────────────────

	let formName = $state('');
	let formType = $state<SIEMDestinationType>('splunk');
	let formEnabled = $state(true);
	let formConfig = $state<Record<string, string>>({});

	const DEST_TYPES: { value: SIEMDestinationType; label: string }[] = [
		{ value: 'splunk', label: 'Splunk (HEC)' },
		{ value: 'datadog', label: 'Datadog Logs' },
		{ value: 'elasticsearch', label: 'Elasticsearch' },
		{ value: 'webhook', label: 'Generic Webhook' },
		{ value: 'chronicle', label: 'GCP SecOps / Chronicle' },
		{ value: 'wazuh', label: 'Wazuh' },
		{ value: 'graylog', label: 'Graylog (GELF)' }
	];

	const CONFIG_FIELDS: Record<SIEMDestinationType, { key: string; label: string; placeholder?: string; type?: string; required?: boolean }[]> = {
		splunk: [
			{ key: 'url', label: 'HEC URL', placeholder: 'https://splunk:8088', required: true },
			{ key: 'token', label: 'HEC Token', type: 'password', required: true },
			{ key: 'index', label: 'Index', placeholder: 'main' },
			{ key: 'source', label: 'Source', placeholder: 'crucible-iap' },
			{ key: 'sourcetype', label: 'Sourcetype', placeholder: '_json' },
			{ key: 'tls_insecure', label: 'Skip TLS verification', type: 'checkbox' }
		],
		datadog: [
			{ key: 'api_key', label: 'API Key', type: 'password', required: true },
			{ key: 'site', label: 'Site', placeholder: 'datadoghq.com' },
			{ key: 'service', label: 'Service', placeholder: 'crucible-iap' },
			{ key: 'tags', label: 'Tags', placeholder: 'env:prod,team:infra' }
		],
		elasticsearch: [
			{ key: 'url', label: 'Elasticsearch URL', placeholder: 'https://es:9200', required: true },
			{ key: 'index', label: 'Index', placeholder: 'crucible-audit', required: true },
			{ key: 'username', label: 'Username' },
			{ key: 'password', label: 'Password', type: 'password' },
			{ key: 'api_key', label: 'API Key (base64 id:key)', type: 'password' },
			{ key: 'pipeline_id', label: 'Ingest Pipeline ID' },
			{ key: 'tls_insecure', label: 'Skip TLS verification', type: 'checkbox' }
		],
		webhook: [
			{ key: 'url', label: 'Webhook URL', placeholder: 'https://example.com/hook', required: true },
			{ key: 'secret', label: 'HMAC Secret', type: 'password' },
			{ key: 'tls_insecure', label: 'Skip TLS verification', type: 'checkbox' }
		],
		chronicle: [
			{ key: 'customer_id', label: 'Customer ID', required: true },
			{ key: 'region', label: 'Region', placeholder: 'us' },
			{ key: 'log_type', label: 'Log Type', placeholder: 'THIRD_PARTY_APP' },
			{ key: 'service_account_json', label: 'Service Account JSON', type: 'textarea', required: true }
		],
		wazuh: [
			{ key: 'url', label: 'Manager URL', placeholder: 'https://wazuh-manager:55000' },
			{ key: 'username', label: 'Username' },
			{ key: 'password', label: 'Password', type: 'password' },
			{ key: 'agent_id', label: 'Agent ID', placeholder: '000' },
			{ key: 'syslog_address', label: 'Syslog Address (host:port — overrides REST)', placeholder: 'wazuh:514' },
			{ key: 'tls_insecure', label: 'Skip TLS verification', type: 'checkbox' }
		],
		graylog: [
			{ key: 'url', label: 'GELF HTTP URL', placeholder: 'http://graylog:12201/gelf', required: true },
			{ key: 'tls_insecure', label: 'Skip TLS verification', type: 'checkbox' }
		]
	};

	const STATUS_COLORS: Record<string, string> = {
		delivered: 'text-green-400',
		failed: 'text-red-400',
		pending: 'text-yellow-400'
	};

	// ── Lifecycle ─────────────────────────────────────────────────────────────

	onMount(async () => {
		destinations = await siemApi.list();
		loading = false;
	});

	async function loadDeliveries() {
		const res = await siemApi.listDeliveries(deliveryFilter ? { destination_id: deliveryFilter } : undefined);
		deliveries = res.items ?? [];
	}

	// ── Actions ───────────────────────────────────────────────────────────────

	function openCreate() {
		editingDest = null;
		formName = '';
		formType = 'splunk';
		formEnabled = true;
		formConfig = {};
		showModal = true;
	}

	function openEdit(d: SIEMDestination) {
		editingDest = d;
		formName = d.name;
		formType = d.type;
		formEnabled = d.enabled;
		formConfig = {};
		showModal = true;
	}

	async function saveDestination() {
		const config: Record<string, unknown> = {};
		for (const field of CONFIG_FIELDS[formType]) {
			if (field.type === 'checkbox') {
				config[field.key] = formConfig[field.key] === 'true';
			} else if (formConfig[field.key] !== undefined && formConfig[field.key] !== '') {
				config[field.key] = formConfig[field.key];
			}
		}

		if (editingDest) {
			const updated = await siemApi.update(editingDest.id, {
				name: formName,
				enabled: formEnabled,
				...(Object.keys(config).length > 0 ? { config } : {})
			});
			destinations = destinations.map((d) => (d.id === updated.id ? updated : d));
		} else {
			const created = await siemApi.create({ name: formName, type: formType, config, enabled: formEnabled });
			destinations = [...destinations, created];
		}
		showModal = false;
	}

	async function toggleEnabled(dest: SIEMDestination) {
		const updated = await siemApi.update(dest.id, { enabled: !dest.enabled });
		destinations = destinations.map((d) => (d.id === updated.id ? updated : d));
	}

	async function deleteDest(id: string) {
		if (!confirm('Delete this SIEM destination? Delivery history will also be removed.')) return;
		await siemApi.delete(id);
		destinations = destinations.filter((d) => d.id !== id);
	}

	async function testConn(id: string) {
		testingID = id;
		testResult = null;
		const res = await siemApi.test(id);
		testResult = { ok: res.ok === 'true', error: res.error };
		testingID = null;
	}
</script>

<div class="max-w-4xl">
	<div class="flex items-center justify-between mb-6">
		<div>
			<h1 class="text-xl font-semibold text-white">SIEM Streaming</h1>
			<p class="mt-1 text-sm text-zinc-400">
				Stream audit events to external SIEM and log aggregation systems in near real-time.
			</p>
		</div>
		<button
			onclick={openCreate}
			class="rounded-lg bg-amber px-4 py-2 text-sm font-semibold text-navy transition-colors hover:bg-amber-light"
		>
			Add destination
		</button>
	</div>

	{#if loading}
		<p class="text-sm text-zinc-500">Loading…</p>
	{:else if destinations.length === 0}
		<div class="rounded-xl border border-zinc-800 bg-zinc-900 p-10 text-center">
			<p class="text-sm text-zinc-400">No SIEM destinations configured.</p>
			<p class="mt-1 text-xs text-zinc-600">
				Add a destination to stream audit events to Splunk, Datadog, Elasticsearch, GCP SecOps,
				Wazuh, Graylog, or a generic webhook.
			</p>
		</div>
	{:else}
		<div class="space-y-3">
			{#each destinations as dest (dest.id)}
				<div class="rounded-xl border border-zinc-800 bg-zinc-900 px-5 py-4">
					<div class="flex items-start justify-between gap-4">
						<div class="flex items-center gap-3 min-w-0">
							<span
								class="h-2 w-2 flex-shrink-0 rounded-full {dest.enabled
									? 'bg-green-500'
									: 'bg-zinc-600'}"
							></span>
							<div class="min-w-0">
								<p class="font-medium text-white truncate">{dest.name}</p>
								<p class="text-xs text-zinc-500 mt-0.5">
									{DEST_TYPES.find((t) => t.value === dest.type)?.label ?? dest.type}
								</p>
							</div>
						</div>
						<div class="flex items-center gap-2 flex-shrink-0">
							{#if testResult && testingID === null}
								<!-- show last result briefly -->
							{/if}
							<button
								onclick={() => testConn(dest.id)}
								disabled={testingID === dest.id}
								class="rounded px-2.5 py-1 text-xs font-medium text-zinc-300 border border-zinc-700 hover:bg-zinc-800 disabled:opacity-50"
							>
								{testingID === dest.id ? 'Testing…' : 'Test'}
							</button>
							<button
								onclick={() => toggleEnabled(dest)}
								class="rounded px-2.5 py-1 text-xs font-medium border
									{dest.enabled
										? 'text-zinc-300 border-zinc-700 hover:bg-zinc-800'
										: 'text-amber border-amber/40 hover:bg-amber/10'}"
							>
								{dest.enabled ? 'Disable' : 'Enable'}
							</button>
							<button
								onclick={() => openEdit(dest)}
								class="rounded px-2.5 py-1 text-xs font-medium text-zinc-300 border border-zinc-700 hover:bg-zinc-800"
							>
								Edit
							</button>
							<button
								onclick={() => deleteDest(dest.id)}
								class="rounded px-2.5 py-1 text-xs font-medium text-red-400 border border-red-900/50 hover:bg-red-900/20"
							>
								Delete
							</button>
						</div>
					</div>
				</div>
			{/each}
		</div>
	{/if}

	<!-- Delivery log toggle -->
	<div class="mt-8">
		<button
			onclick={async () => {
				showDeliveries = !showDeliveries;
				if (showDeliveries && deliveries.length === 0) await loadDeliveries();
			}}
			class="text-sm font-medium text-zinc-400 hover:text-white transition-colors"
		>
			{showDeliveries ? '▾' : '▸'} Delivery log
		</button>

		{#if showDeliveries}
			<div class="mt-4">
				<div class="mb-3 flex items-center gap-3">
					<select
						bind:value={deliveryFilter}
						onchange={loadDeliveries}
						class="rounded-lg border border-zinc-700 bg-zinc-800 px-3 py-1.5 text-sm text-white"
					>
						<option value="">All destinations</option>
						{#each destinations as d}
							<option value={d.id}>{d.name}</option>
						{/each}
					</select>
					<button
						onclick={loadDeliveries}
						class="rounded-lg border border-zinc-700 px-3 py-1.5 text-sm text-zinc-300 hover:bg-zinc-800"
					>
						Refresh
					</button>
				</div>

				{#if deliveries.length === 0}
					<p class="text-sm text-zinc-500">No deliveries recorded yet.</p>
				{:else}
					<div class="overflow-x-auto rounded-xl border border-zinc-800">
						<table class="w-full text-sm">
							<thead>
								<tr class="border-b border-zinc-800 text-left text-xs text-zinc-500">
									<th class="px-4 py-2">Destination</th>
									<th class="px-4 py-2">Event ID</th>
									<th class="px-4 py-2">Status</th>
									<th class="px-4 py-2">Attempts</th>
									<th class="px-4 py-2">Time</th>
									<th class="px-4 py-2">Error</th>
								</tr>
							</thead>
							<tbody>
								{#each deliveries as dv (dv.id)}
									<tr class="border-b border-zinc-800/50 hover:bg-zinc-800/30">
										<td class="px-4 py-2 text-zinc-300">{dv.destination_name}</td>
										<td class="px-4 py-2 text-zinc-500 font-mono text-xs">{dv.event_id}</td>
										<td class="px-4 py-2 font-medium {STATUS_COLORS[dv.status] ?? 'text-zinc-400'}"
											>{dv.status}</td
										>
										<td class="px-4 py-2 text-zinc-400">{dv.attempts}</td>
										<td class="px-4 py-2 text-zinc-500 text-xs">
											{new Date(dv.delivered_at ?? dv.created_at).toLocaleString()}
										</td>
										<td class="px-4 py-2 text-red-400 text-xs max-w-xs truncate"
											>{dv.last_error ?? ''}</td
										>
									</tr>
								{/each}
							</tbody>
						</table>
					</div>
				{/if}
			</div>
		{/if}
	</div>
</div>

<!-- Add / Edit modal -->
{#if showModal}
	<div class="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4">
		<div class="w-full max-w-lg rounded-2xl border border-zinc-700 bg-zinc-900 shadow-2xl overflow-y-auto max-h-[90vh]">
			<div class="border-b border-zinc-800 px-6 py-4">
				<h2 class="text-base font-semibold text-white">
					{editingDest ? 'Edit destination' : 'Add SIEM destination'}
				</h2>
			</div>
			<div class="px-6 py-5 space-y-4">
				<!-- Name -->
				<div>
					<label for="siem-name" class="block text-xs font-medium text-zinc-400 mb-1">Name</label>
					<input
						id="siem-name"
						bind:value={formName}
						class="w-full rounded-lg border border-zinc-700 bg-zinc-800 px-3 py-2 text-sm text-white placeholder-zinc-500 focus:border-amber/50 focus:outline-none"
						placeholder="Production Splunk"
					/>
				</div>

				<!-- Type (create only) -->
				{#if !editingDest}
					<div>
						<label for="siem-type" class="block text-xs font-medium text-zinc-400 mb-1"
							>Destination type</label
						>
						<select
							id="siem-type"
							bind:value={formType}
							class="w-full rounded-lg border border-zinc-700 bg-zinc-800 px-3 py-2 text-sm text-white focus:border-amber/50 focus:outline-none"
						>
							{#each DEST_TYPES as t}
								<option value={t.value}>{t.label}</option>
							{/each}
						</select>
					</div>
				{/if}

				<!-- Type-specific config fields -->
				{#if !editingDest || true}
					{@const fields = CONFIG_FIELDS[formType]}
					<div class="space-y-3">
						<p class="text-xs font-medium text-zinc-500 uppercase tracking-widest">
							{editingDest ? 'Update config (leave blank to keep existing)' : 'Configuration'}
						</p>
						{#each fields as field}
							<div>
								<label for="siem-{field.key}" class="block text-xs font-medium text-zinc-400 mb-1">
									{field.label}{#if field.required}<span class="text-amber ml-0.5">*</span>{/if}
								</label>
								{#if field.type === 'checkbox'}
									<label class="flex items-center gap-2 cursor-pointer">
										<input
											id="siem-{field.key}"
											type="checkbox"
											checked={formConfig[field.key] === 'true'}
											onchange={(e) => {
												formConfig[field.key] = (e.target as HTMLInputElement).checked
													? 'true'
													: 'false';
											}}
											class="rounded border-zinc-600"
										/>
										<span class="text-sm text-zinc-300">Enable</span>
									</label>
								{:else if field.type === 'textarea'}
									<textarea
										id="siem-{field.key}"
										bind:value={formConfig[field.key]}
										rows="5"
										class="w-full rounded-lg border border-zinc-700 bg-zinc-800 px-3 py-2 text-xs font-mono text-white placeholder-zinc-500 focus:border-amber/50 focus:outline-none"
										placeholder={field.placeholder ?? ''}
									></textarea>
								{:else}
									<input
										id="siem-{field.key}"
										type={field.type ?? 'text'}
										bind:value={formConfig[field.key]}
										class="w-full rounded-lg border border-zinc-700 bg-zinc-800 px-3 py-2 text-sm text-white placeholder-zinc-500 focus:border-amber/50 focus:outline-none"
										placeholder={field.placeholder ?? ''}
									/>
								{/if}
							</div>
						{/each}
					</div>
				{/if}

				<!-- Enabled toggle -->
				<div class="flex items-center gap-3">
					<label for="siem-enabled" class="text-sm text-zinc-300">Enabled</label>
					<input id="siem-enabled" type="checkbox" bind:checked={formEnabled} class="rounded border-zinc-600" />
				</div>
			</div>

			<div class="flex justify-end gap-3 border-t border-zinc-800 px-6 py-4">
				<button
					onclick={() => (showModal = false)}
					class="rounded-lg border border-zinc-700 px-4 py-2 text-sm font-medium text-zinc-300 hover:bg-zinc-800"
				>
					Cancel
				</button>
				<button
					onclick={saveDestination}
					disabled={!formName}
					class="rounded-lg bg-amber px-4 py-2 text-sm font-semibold text-navy transition-colors hover:bg-amber-light disabled:opacity-50"
				>
					{editingDest ? 'Save changes' : 'Add destination'}
				</button>
			</div>
		</div>
	</div>
{/if}
