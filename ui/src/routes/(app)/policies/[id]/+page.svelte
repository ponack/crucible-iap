<script lang="ts">
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { onMount } from 'svelte';
	import { policies, type Policy } from '$lib/api/client';
	import { auth } from '$lib/stores/auth.svelte';
	import RegoEditor from '$lib/components/RegoEditor.svelte';
	import PolicyInputSchema from '$lib/components/PolicyInputSchema.svelte';
	import { policyTemplates, sampleInputs, type PolicyType } from '$lib/policy-data';
	import { type PolicyResult, policies as policiesApi } from '$lib/api/client';

	const id = $derived(page.params.id!);

	let policy = $state<Policy | null>(null);
	let loading = $state(true);
	let error = $state<string | null>(null);
	let saving = $state(false);
	let saveError = $state<string | null>(null);
	let saved = $state(false);
	let validating = $state(false);
	let validateResult = $state<{ ok: boolean; error?: string } | null>(null);
	let showTemplates = $state(false);
	let testInput = $state('');
	let testing = $state(false);
	let testResult = $state<{ ok: boolean; error?: string; result?: PolicyResult; trace?: string } | null>(null);
	let showTest = $state(false);
	let traceEnabled = $state(false);
	let isOrgDefault = $state(false);
	let togglingOrgDefault = $state(false);

	let form = $state({ name: '', description: '', body: '', is_active: true });

	onMount(async () => {
		try {
			policy = await policies.get(id);
			resetForm();
			policiesApi.isOrgDefault(id).then((r) => (isOrgDefault = r.is_org_default)).catch(() => {});
		} catch (e) {
			error = (e as Error).message;
		} finally {
			loading = false;
		}
	});

	function resetForm() {
		if (!policy) return;
		form = {
			name: policy.name,
			description: policy.description ?? '',
			body: policy.body,
			is_active: policy.is_active
		};
		validateResult = null;
	}

	async function runTest() {
		if (!policy) return;
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
			testResult = await policies.test(policy.type, form.body, parsed, traceEnabled);
		} catch (e) {
			testResult = { ok: false, error: (e as Error).message };
		} finally {
			testing = false;
		}
	}

	function openTest() {
		if (!testInput && policy) {
			testInput = sampleInputs[policy.type as PolicyType] ?? '{}';
		}
		showTest = !showTest;
	}

	async function save(e: SubmitEvent) {
		e.preventDefault();
		saving = true;
		saveError = null;
		saved = false;
		try {
			policy = await policies.update(id, form);
			saved = true;
			setTimeout(() => (saved = false), 3000);
		} catch (e) {
			saveError = (e as Error).message;
		} finally {
			saving = false;
		}
	}

	async function validateRego() {
		if (!policy) return;
		validating = true;
		validateResult = null;
		try {
			validateResult = await policies.validate(policy.type, form.body);
		} catch (e) {
			validateResult = { ok: false, error: (e as Error).message };
		} finally {
			validating = false;
		}
	}

	async function deletePolicy() {
		if (!confirm(`Delete policy "${policy?.name}"? Stacks using it will no longer have it evaluated.`))
			return;
		await policies.delete(id);
		goto('/policies');
	}

	async function toggleOrgDefault() {
		if (!policy) return;
		togglingOrgDefault = true;
		try {
			if (isOrgDefault) {
				await policiesApi.unsetOrgDefault(policy.id);
				isOrgDefault = false;
			} else {
				await policiesApi.setOrgDefault(policy.id);
				isOrgDefault = true;
			}
		} catch (e) {
			alert((e as Error).message);
		} finally {
			togglingOrgDefault = false;
		}
	}

	function applyTemplate(body: string) {
		form.body = body;
		showTemplates = false;
		validateResult = null;
	}

	const typeLabels: Record<string, string> = {
		post_plan: 'Post-plan',
		pre_plan: 'Pre-plan',
		pre_apply: 'Pre-apply',
		trigger: 'Trigger',
		login: 'Login'
	};
</script>

{#if loading}
	<div class="p-6 text-sm text-zinc-500">Loading…</div>
{:else if error || !policy}
	<div class="p-6 text-sm text-red-400">{error ?? 'Policy not found'}</div>
{:else}
	<div class="max-w-3xl space-y-6 p-6">
		<!-- Header -->
		<div class="flex items-start justify-between">
			<div>
				<div class="mb-1 flex items-center gap-2 text-sm text-zinc-500">
					<a href="/policies" class="hover:text-zinc-300">Policies</a>
					<span>/</span>
					<span class="font-medium text-white">{policy.name}</span>
				</div>
				<div class="flex items-center gap-2 text-xs text-zinc-500">
					<span class="rounded bg-zinc-800 px-1.5 py-0.5 text-zinc-400">
						{typeLabels[policy.type] ?? policy.type}
					</span>
					<span class={policy.is_active ? 'text-green-400' : 'text-zinc-500'}>
						{policy.is_active ? 'Active' : 'Inactive'}
					</span>
					{#if isOrgDefault}
						<span class="rounded bg-teal-900 px-1.5 py-0.5 text-teal-300">Org default</span>
					{/if}
				</div>
			</div>
			{#if auth.isAdmin}
				<button
					onclick={deletePolicy}
					class="rounded-lg border border-red-900 px-3 py-1.5 text-sm text-red-400 transition-colors hover:border-red-700"
				>
					Delete
				</button>
			{/if}
		</div>

		<!-- Editor form -->
		<form onsubmit={save} class="space-y-4">
			{#if saveError}
				<div class="rounded-lg border border-red-800 bg-red-950 px-4 py-3 text-sm text-red-300">
					{saveError}
				</div>
			{/if}
			{#if saved}
				<div
					class="rounded-lg border border-green-800 bg-green-950 px-4 py-3 text-sm text-green-300"
				>
					Policy saved and reloaded into engine.
				</div>
			{/if}

			<div class="grid grid-cols-2 gap-4">
				<div class="space-y-1.5">
					<label class="field-label" for="p-name">Name</label>
					<input id="p-name" class="field-input" bind:value={form.name} required />
				</div>
				<div class="space-y-1.5">
					<label class="field-label" for="p-type-display">Type</label>
					<input
						id="p-type-display"
						class="field-input cursor-not-allowed opacity-60"
						value={typeLabels[policy.type] ?? policy.type}
						disabled
					/>
				</div>
			</div>

			<div class="space-y-1.5">
				<label class="field-label" for="p-desc">Description</label>
				<input
					id="p-desc"
					class="field-input"
					bind:value={form.description}
					placeholder="Optional"
				/>
			</div>

			<!-- Rego editor -->
			<div class="space-y-1.5">
				<div class="flex items-center justify-between">
					<span class="field-label">Rego source</span>
					<div class="flex items-center gap-3">
						<button
							type="button"
							onclick={() => (showTemplates = !showTemplates)}
							class="text-xs text-zinc-400 transition-colors hover:text-zinc-200"
						>
							{showTemplates ? 'Hide templates' : 'Load template'}
						</button>
						<button
							type="button"
							onclick={validateRego}
							disabled={validating || !form.body}
							class="text-xs text-zinc-400 transition-colors hover:text-zinc-200 disabled:opacity-40"
						>
							{validating ? 'Validating…' : 'Validate syntax'}
						</button>
					</div>
				</div>

				{#if showTemplates}
					<div class="rounded-lg border border-zinc-800 bg-zinc-950 p-3 space-y-1">
						<p class="mb-2 text-xs text-zinc-500">
							Select a template to replace the current source:
						</p>
						{#each policyTemplates[policy.type as PolicyType] as t}
							<button
								type="button"
								onclick={() => applyTemplate(t.body)}
								class="w-full rounded-md px-3 py-2 text-left text-xs transition-colors hover:bg-zinc-800"
							>
								<span class="font-medium text-zinc-200">{t.name}</span>
								<span class="ml-2 text-zinc-500">{t.description}</span>
							</button>
						{/each}
					</div>
				{/if}

				<RegoEditor bind:value={form.body} minLines={20} />

				{#if validateResult}
					{#if validateResult.ok}
						<p class="text-xs text-green-400">Syntax valid — no compile errors.</p>
					{:else}
						<p class="whitespace-pre-wrap font-mono text-xs text-red-400">
							{validateResult.error}
						</p>
					{/if}
				{/if}
			</div>

			<!-- Input reference -->
			<PolicyInputSchema type={policy.type as PolicyType} />

			<!-- Dry-run sandbox -->
			<div class="rounded-lg border border-zinc-800">
				<button
					type="button"
					onclick={openTest}
					class="flex w-full items-center justify-between px-3 py-2 text-xs text-zinc-400 transition-colors hover:text-zinc-200"
				>
					<span class="font-medium">Test policy</span>
					<span class="text-zinc-600">{showTest ? 'hide' : 'run against sample input'}</span>
				</button>

				{#if showTest}
					<div class="space-y-3 border-t border-zinc-800 px-3 py-3">
						<p class="text-xs text-zinc-500">
							Paste or edit a JSON input below and run the policy against it without saving.
						</p>
						<div class="space-y-1.5">
							<label class="field-label" for="test-input">Test input (JSON)</label>
							<textarea
								id="test-input"
								class="field-input font-mono text-xs"
								rows="12"
								bind:value={testInput}
								spellcheck="false"
							></textarea>
						</div>
						<div class="flex items-center justify-between">
							<label class="flex cursor-pointer items-center gap-1.5 text-xs text-zinc-500 hover:text-zinc-300">
								<input type="checkbox" bind:checked={traceEnabled} class="accent-teal-500" />
								Include trace
							</label>
							<button
								type="button"
								onclick={runTest}
								disabled={testing || !testInput}
								class="rounded-lg bg-zinc-700 px-3 py-1.5 text-xs text-zinc-200 transition-colors hover:bg-zinc-600 disabled:opacity-40"
							>
								{testing ? 'Running…' : 'Run test'}
							</button>
						</div>

						{#if testResult}
							{#if !testResult.ok}
								<p class="font-mono text-xs text-red-400">{testResult.error}</p>
							{:else if testResult.result}
								{@const r = testResult.result}
								<div class="space-y-1 rounded-lg bg-zinc-950 px-3 py-2">
									<p class="text-xs font-medium {r.allow ? 'text-green-400' : 'text-red-400'}">
										{r.allow ? 'PASS — no denials' : 'BLOCKED'}
									</p>
									{#if r.deny && r.deny.length > 0}
										{#each r.deny as msg}
											<p class="font-mono text-xs text-red-300">deny: {msg}</p>
										{/each}
									{/if}
									{#if r.warn && r.warn.length > 0}
										{#each r.warn as msg}
											<p class="font-mono text-xs text-amber-300">warn: {msg}</p>
										{/each}
									{/if}
									{#if r.trigger && r.trigger.length > 0}
										{#each r.trigger as id}
											<p class="font-mono text-xs text-teal-300">trigger: {id}</p>
										{/each}
									{/if}
									{#if r.require_approval}
										<p class="font-mono text-xs text-yellow-300">require_approval: true</p>
									{/if}
								</div>
								{#if testResult.trace}
									<details class="rounded-lg border border-zinc-800">
										<summary class="cursor-pointer px-3 py-2 text-xs text-zinc-500 hover:text-zinc-300">
											Evaluation trace
										</summary>
										<pre class="max-h-64 overflow-auto px-3 pb-3 font-mono text-[11px] leading-relaxed text-zinc-400 whitespace-pre">{testResult.trace}</pre>
									</details>
								{/if}
							{/if}
						{/if}
					</div>
				{/if}
			</div>

			<div class="flex items-center justify-between pt-1">
				<div class="flex items-center gap-4">
					<label class="flex cursor-pointer items-center gap-2 text-sm text-zinc-300">
						<input type="checkbox" bind:checked={form.is_active} />
						Active
					</label>
					{#if auth.isAdmin}
						<button
							type="button"
							onclick={toggleOrgDefault}
							disabled={togglingOrgDefault}
							class="text-xs transition-colors disabled:opacity-40
								{isOrgDefault
								? 'text-teal-400 hover:text-teal-200'
								: 'text-zinc-500 hover:text-zinc-300'}"
						>
							{togglingOrgDefault
								? '…'
								: isOrgDefault
									? 'Org default (click to remove)'
									: 'Set as org default'}
						</button>
					{/if}
				</div>
				<div class="flex gap-3">
					<button
						type="button"
						onclick={resetForm}
						class="rounded-lg border border-zinc-700 px-3 py-1.5 text-sm text-zinc-400 transition-colors hover:border-zinc-500"
					>
						Reset
					</button>
					<button
						type="submit"
						disabled={saving}
						class="rounded-lg bg-teal-600 px-4 py-1.5 text-sm text-white transition-colors hover:bg-teal-500 disabled:opacity-50"
					>
						{saving ? 'Saving…' : 'Save policy'}
					</button>
				</div>
			</div>
		</form>
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
