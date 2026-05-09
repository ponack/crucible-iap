<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { stacks, stackTemplates, projects, type StackTemplate, type Project } from '$lib/api/client';

	let submitting = $state(false);
	let error = $state<string | null>(null);

	let templates = $state<StackTemplate[]>([]);
	let selectedTemplateID = $state('');
	let projectList = $state<Project[]>([]);

	onMount(async () => {
		await Promise.all([
			stackTemplates.list().then((t) => { templates = t; }).catch(() => {}),
			projects.list().then((p) => { projectList = p; }).catch(() => {})
		]);
	});

	function applyTemplate(id: string) {
		const t = templates.find((t) => t.id === id);
		if (!t) return;
		form.tool = t.tool as typeof form.tool;
		form.tool_version = t.tool_version;
		form.repo_url = t.repo_url;
		form.repo_branch = t.repo_branch;
		form.project_root = t.project_root;
		form.runner_image = t.runner_image;
		form.auto_apply = t.auto_apply;
		form.drift_detection = t.drift_detection;
		form.drift_schedule = t.drift_schedule || '0 */6 * * *';
		form.auto_remediate_drift = t.auto_remediate_drift;
	}

	let form = $state({
		name: '',
		description: '',
		tool: 'opentofu' as 'opentofu' | 'terraform' | 'ansible' | 'pulumi',
		tool_version: '',
		repo_url: '',
		repo_branch: 'main',
		project_root: '.',
		runner_image: '',
		auto_apply: false,
		drift_detection: false,
		drift_schedule: '0 */6 * * *',
		auto_remediate_drift: false,
		project_id: ''
	});

	async function submit(e: SubmitEvent) {
		e.preventDefault();
		submitting = true;
		error = null;
		try {
			const payload: Record<string, unknown> = { ...form };
			// Strip optional string fields that are empty so the backend uses its defaults
			if (!payload.tool_version) delete payload.tool_version;
			if (!payload.runner_image) delete payload.runner_image;
			if (!payload.drift_detection) { delete payload.drift_schedule; delete payload.auto_remediate_drift; }
			if (!payload.project_id) delete payload.project_id;
			const stack = await stacks.create(payload);
			goto(`/stacks/${stack.id}`);
		} catch (e) {
			error = (e as Error).message;
			submitting = false;
		}
	}
</script>

<div class="p-6 max-w-2xl space-y-6">
	<div class="flex items-center gap-3">
		<a href="/stacks" class="text-zinc-500 hover:text-zinc-300 text-sm">← Stacks</a>
		<span class="text-zinc-700">/</span>
		<h1 class="text-lg font-semibold text-white">New stack</h1>
	</div>

	{#if templates.length > 0}
		<div class="flex items-center gap-3">
			<label class="text-xs text-zinc-500 shrink-0" for="template-picker">Start from template</label>
			<select id="template-picker" class="field-input max-w-xs"
				bind:value={selectedTemplateID}
				onchange={() => applyTemplate(selectedTemplateID)}>
				<option value="">— none —</option>
				{#each templates as t (t.id)}
					<option value={t.id}>{t.name}</option>
				{/each}
			</select>
		</div>
	{/if}

	{#if error}
		<div class="bg-red-950 border border-red-800 rounded-lg px-4 py-3 text-red-300 text-sm">
			{error}
		</div>
	{/if}

	<form onsubmit={submit} class="space-y-5">
		<fieldset class="border border-zinc-800 rounded-xl p-5 space-y-4">
			<legend class="text-xs text-zinc-500 uppercase tracking-widest px-1">General</legend>

			<div class="grid grid-cols-2 gap-4">
				<div class="space-y-1.5">
					<label class="field-label" for="name">Name</label>
					<input id="name" class="field-input" bind:value={form.name} required placeholder="my-stack" />
				</div>
				<div class="space-y-1.5">
					<label class="field-label" for="tool">Tool</label>
					<select id="tool" class="field-input" bind:value={form.tool}>
						<option value="opentofu">OpenTofu</option>
						<option value="terraform">Terraform</option>
						<option value="ansible">Ansible</option>
						<option value="pulumi">Pulumi</option>
					</select>
				</div>
			</div>

			<div class="space-y-1.5">
				<label class="field-label" for="tool_version">Tool version <span class="text-zinc-600">(optional)</span></label>
				<input id="tool_version" class="field-input font-mono text-sm" bind:value={form.tool_version}
					placeholder="e.g. 1.7.0 — leave blank to use the runner default" />
			</div>

			<div class="space-y-1.5">
				<label class="field-label" for="description">Description <span class="text-zinc-600">(optional)</span></label>
				<input id="description" class="field-input" bind:value={form.description} placeholder="What does this stack manage?" />
			</div>

			{#if projectList.length > 0}
				<div class="space-y-1.5">
					<label class="field-label" for="project_id">Project <span class="text-zinc-600">(optional)</span></label>
					<select id="project_id" class="field-input" bind:value={form.project_id}>
						<option value="">— unassigned —</option>
						{#each projectList as p (p.id)}
							<option value={p.id}>{p.name}</option>
						{/each}
					</select>
				</div>
			{/if}
		</fieldset>

		<fieldset class="border border-zinc-800 rounded-xl p-5 space-y-4">
			<legend class="text-xs text-zinc-500 uppercase tracking-widest px-1">Repository</legend>

			<div class="space-y-1.5">
				<label class="field-label" for="repo_url">Repo URL</label>
				<input id="repo_url" class="field-input font-mono text-sm" bind:value={form.repo_url} required
					placeholder="https://github.com/org/infra" />
			</div>

			<div class="grid grid-cols-2 gap-4">
				<div class="space-y-1.5">
					<label class="field-label" for="repo_branch">Branch</label>
					<input id="repo_branch" class="field-input font-mono text-sm" bind:value={form.repo_branch} placeholder="main" />
				</div>
				<div class="space-y-1.5">
					<label class="field-label" for="project_root">Project root</label>
					<input id="project_root" class="field-input font-mono text-sm" bind:value={form.project_root} placeholder="." />
				</div>
			</div>
		</fieldset>

		<fieldset class="border border-zinc-800 rounded-xl p-5 space-y-4">
			<legend class="text-xs text-zinc-500 uppercase tracking-widest px-1">Runner</legend>

			<div class="space-y-1.5">
				<label class="field-label" for="runner_image">Runner image <span class="text-zinc-600">(optional)</span></label>
				<input id="runner_image" class="field-input font-mono text-sm" bind:value={form.runner_image}
					placeholder="e.g. ghcr.io/ponack/crucible-iap-runner:latest" />
				<p class="text-xs text-zinc-600">Leave blank to use the server default runner image.</p>
			</div>
		</fieldset>

		<fieldset class="border border-zinc-800 rounded-xl p-5 space-y-3">
			<legend class="text-xs text-zinc-500 uppercase tracking-widest px-1">Behaviour</legend>

			<label class="flex items-center gap-3 cursor-pointer">
				<input type="checkbox" class="rounded border-zinc-700 bg-zinc-900 text-teal-500"
					bind:checked={form.auto_apply} />
				<span class="text-sm text-zinc-300">
					Auto-apply — apply immediately after a clean plan (no confirmation required)
				</span>
			</label>

			<label class="flex items-center gap-3 cursor-pointer">
				<input type="checkbox" class="rounded border-zinc-700 bg-zinc-900 text-teal-500"
					bind:checked={form.drift_detection} />
				<span class="text-sm text-zinc-300">
					Drift detection — schedule periodic plan runs to detect configuration drift
				</span>
			</label>

			{#if form.drift_detection}
				<div class="space-y-1.5 pl-7">
					<label class="field-label" for="drift_schedule">Drift schedule (cron)</label>
					<select id="drift_schedule" class="field-input" bind:value={form.drift_schedule}>
						<option value="0 */1 * * *">Every hour</option>
						<option value="0 */6 * * *">Every 6 hours</option>
						<option value="0 */12 * * *">Every 12 hours</option>
						<option value="0 0 * * *">Daily (midnight UTC)</option>
						<option value="0 0 * * 1">Weekly (Monday midnight UTC)</option>
					</select>
				</div>
				<label class="flex items-center gap-3 cursor-pointer pl-7">
					<input type="checkbox" class="rounded border-zinc-700 bg-zinc-900 text-teal-500"
						bind:checked={form.auto_remediate_drift} />
					<span class="text-sm text-zinc-300">
						Auto-remediate drift — automatically apply when drift is detected
					</span>
				</label>
			{/if}
		</fieldset>

		<div class="flex items-center gap-3">
			<button type="submit" disabled={submitting}
				class="bg-teal-600 hover:bg-teal-500 disabled:opacity-50 text-white text-sm px-4 py-2 rounded-lg transition-colors">
				{submitting ? 'Creating…' : 'Create stack'}
			</button>
			<a href="/stacks" class="text-sm text-zinc-500 hover:text-zinc-300">Cancel</a>
		</div>
	</form>
</div>

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
		border-color: var(--color-teal-500, #6366f1);
	}
</style>
