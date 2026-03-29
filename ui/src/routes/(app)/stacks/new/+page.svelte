<script lang="ts">
	import { goto } from '$app/navigation';
	import { stacks } from '$lib/api/client';

	let submitting = $state(false);
	let error = $state<string | null>(null);

	let form = $state({
		name: '',
		description: '',
		tool: 'opentofu' as 'opentofu' | 'terraform' | 'ansible' | 'pulumi',
		repo_url: '',
		repo_branch: 'main',
		project_root: '.',
		auto_apply: false,
		drift_detection: false
	});

	async function submit(e: SubmitEvent) {
		e.preventDefault();
		submitting = true;
		error = null;
		try {
			const stack = await stacks.create(form);
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
				<label class="field-label" for="description">Description <span class="text-zinc-600">(optional)</span></label>
				<input id="description" class="field-input" bind:value={form.description} placeholder="What does this stack manage?" />
			</div>
		</fieldset>

		<fieldset class="border border-zinc-800 rounded-xl p-5 space-y-4">
			<legend class="text-xs text-zinc-500 uppercase tracking-widest px-1">Repository</legend>

			<div class="space-y-1.5">
				<label class="field-label" for="repo_url">Repo URL</label>
				<input id="repo_url" class="field-input font-mono text-sm" bind:value={form.repo_url} required
					placeholder="https://github.com/org/infra.git" />
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

		<fieldset class="border border-zinc-800 rounded-xl p-5 space-y-3">
			<legend class="text-xs text-zinc-500 uppercase tracking-widest px-1">Behaviour</legend>

			<label class="flex items-center gap-3 cursor-pointer">
				<input type="checkbox" class="rounded border-zinc-700 bg-zinc-900 text-indigo-500"
					bind:checked={form.auto_apply} />
				<span class="text-sm text-zinc-300">
					Auto-apply — apply immediately after a clean plan (no confirmation required)
				</span>
			</label>

			<label class="flex items-center gap-3 cursor-pointer">
				<input type="checkbox" class="rounded border-zinc-700 bg-zinc-900 text-indigo-500"
					bind:checked={form.drift_detection} />
				<span class="text-sm text-zinc-300">
					Drift detection — schedule periodic plan runs to detect configuration drift
				</span>
			</label>
		</fieldset>

		<div class="flex items-center gap-3">
			<button type="submit" disabled={submitting}
				class="bg-indigo-600 hover:bg-indigo-500 disabled:opacity-50 text-white text-sm px-4 py-2 rounded-lg transition-colors">
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
		border-color: var(--color-indigo-500, #6366f1);
	}
</style>
