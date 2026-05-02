<script lang="ts">
	import { configExport, type ImportResult } from '$lib/api/client';
	import { auth } from '$lib/stores/auth.svelte';
	import { toast } from '$lib/stores/toasts.svelte';

	let importing = $state(false);
	let importError = $state<string | null>(null);
	let importResult = $state<ImportResult | null>(null);
	let fileInput = $state<HTMLInputElement | null>(null);

	async function triggerExport() {
		const url = configExport.exportURL();
		const res = await fetch(url, {
			headers: auth.accessToken ? { Authorization: `Bearer ${auth.accessToken}` } : {}
		});
		if (!res.ok) {
			toast.error('Export failed: ' + (await res.text()));
			return;
		}
		const blob = await res.blob();
		const a = document.createElement('a');
		a.href = URL.createObjectURL(blob);
		a.download = `crucible-export-${new Date().toISOString().slice(0, 10)}.json`;
		a.click();
		URL.revokeObjectURL(a.href);
	}

	async function handleFileChange(e: Event) {
		const file = (e.target as HTMLInputElement).files?.[0];
		if (!file) return;

		importing = true;
		importError = null;
		importResult = null;

		try {
			const text = await file.text();
			const manifest = JSON.parse(text);
			importResult = await configExport.import(manifest);
		} catch (e) {
			importError = (e as Error).message;
		} finally {
			importing = false;
			if (fileInput) fileInput.value = '';
		}
	}

	const resultRows: Array<{ label: string; key: keyof ImportResult }> = [
		{ label: 'Stacks', key: 'stacks' },
		{ label: 'Policies', key: 'policies' },
		{ label: 'Variable sets', key: 'variable_sets' },
		{ label: 'Stack templates', key: 'stack_templates' },
		{ label: 'Blueprints', key: 'blueprints' },
		{ label: 'Worker pools', key: 'worker_pools' }
	];
</script>

<div class="space-y-8 max-w-2xl">
	<div>
		<h1 class="text-base font-semibold text-white">Export / Import</h1>
		<p class="text-sm text-zinc-500 mt-1">
			Back up your configuration or migrate it to another Crucible instance.
			Secret values are never exported — only non-secret env vars and variable set values are included.
		</p>
	</div>

	<!-- Export -->
	<section class="space-y-3">
		<h2 class="text-sm font-medium text-zinc-300">Export configuration</h2>
		<p class="text-xs text-zinc-500">
			Downloads a JSON file containing stacks, policies, variable sets, templates, blueprints,
			and worker pool definitions. Secret env vars are omitted; non-secret values are included in plaintext.
		</p>
		<button
			onclick={triggerExport}
			class="rounded-lg border border-zinc-700 hover:border-zinc-500 text-zinc-300 text-sm px-4 py-2 transition-colors">
			Download export
		</button>
	</section>

	<div class="border-t border-zinc-800"></div>

	<!-- Import -->
	<section class="space-y-3">
		<h2 class="text-sm font-medium text-zinc-300">Import configuration</h2>
		<p class="text-xs text-zinc-500">
			Upload a previously exported JSON file. Existing resources (matched by name) are skipped — nothing is overwritten.
			Worker pools imported this way have a placeholder token; rotate it in Worker Pools settings before use.
		</p>

		{#if importError}
			<div class="rounded-lg border border-red-800 bg-red-950 px-4 py-3 text-sm text-red-300">{importError}</div>
		{/if}

		{#if importResult}
			<div class="rounded-xl border border-zinc-800 overflow-hidden">
				<table class="w-full text-sm">
					<thead class="bg-zinc-900 text-zinc-500 text-xs uppercase tracking-wide">
						<tr>
							<th class="text-left px-4 py-2">Resource</th>
							<th class="text-right px-4 py-2">Created</th>
							<th class="text-right px-4 py-2">Skipped</th>
						</tr>
					</thead>
					<tbody class="divide-y divide-zinc-800">
						{#each resultRows as row}
							<tr>
								<td class="px-4 py-2.5 text-zinc-300">{row.label}</td>
								<td class="px-4 py-2.5 text-right">
									{#if importResult[row.key].created > 0}
										<span class="text-emerald-400 font-mono">{importResult[row.key].created}</span>
									{:else}
										<span class="text-zinc-600 font-mono">0</span>
									{/if}
								</td>
								<td class="px-4 py-2.5 text-right text-zinc-500 font-mono">{importResult[row.key].skipped}</td>
							</tr>
						{/each}
					</tbody>
				</table>
			</div>
			<p class="text-xs text-emerald-500">Import complete.</p>
		{/if}

		<div class="flex items-center gap-3">
			<label
				class="cursor-pointer rounded-lg bg-teal-600 px-4 py-2 text-sm text-white transition-colors hover:bg-teal-500 {importing ? 'opacity-50 pointer-events-none' : ''}">
				{importing ? 'Importing…' : 'Choose file'}
				<input
					bind:this={fileInput}
					type="file"
					accept=".json,application/json"
					onchange={handleFileChange}
					class="sr-only"
					disabled={importing} />
			</label>
			{#if importing}
				<span class="text-xs text-zinc-500">Processing…</span>
			{/if}
		</div>
	</section>
</div>
