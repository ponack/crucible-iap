<script lang="ts">
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { onMount } from 'svelte';
	import { projects, org, type ProjectDetail, type ProjectMember, type OrgMember } from '$lib/api/client';
	import { auth } from '$lib/stores/auth.svelte';
	import { toast } from '$lib/stores/toasts.svelte';

	const id = $derived(page.params.id!);

	let detail = $state<ProjectDetail | null>(null);
	let loading = $state(true);
	let error = $state<string | null>(null);

	let activeTab = $state<'stacks' | 'members'>('stacks');

	// Edit
	let editing = $state(false);
	let saving = $state(false);
	let editError = $state<string | null>(null);
	let form = $state({ name: '', description: '' });

	// Add member
	let orgMembers = $state<OrgMember[]>([]);
	let addingMember = $state(false);
	let addMemberUserID = $state('');
	let addMemberRole = $state<'admin' | 'member' | 'viewer'>('member');
	let savingMember = $state(false);
	let memberError = $state<string | null>(null);

	const isAdmin = $derived(auth.orgRole === 'admin');

	onMount(async () => {
		try {
			detail = await projects.get(id);
			form = { name: detail.name, description: detail.description };
		} catch (e) {
			error = (e as Error).message;
		} finally {
			loading = false;
		}
	});

	async function saveEdit(e: SubmitEvent) {
		e.preventDefault();
		saving = true;
		editError = null;
		try {
			const updated = await projects.update(id, form);
			detail = { ...detail!, name: updated.name, description: updated.description };
			editing = false;
		} catch (e) {
			editError = (e as Error).message;
		} finally {
			saving = false;
		}
	}

	async function deleteProject() {
		if (!detail || !confirm(`Delete project "${detail.name}"? This cannot be undone.`)) return;
		try {
			await projects.delete(id);
			goto('/projects');
		} catch (e) {
			toast.error((e as Error).message);
		}
	}

	async function showAddMember() {
		addingMember = true;
		if (orgMembers.length === 0) {
			try {
				orgMembers = await org.members.list();
			} catch {}
		}
	}

	const availableOrgMembers = $derived(
		orgMembers.filter((m) => !detail?.members.some((pm) => pm.user_id === m.user_id))
	);

	async function addMember(e: SubmitEvent) {
		e.preventDefault();
		if (!addMemberUserID) return;
		savingMember = true;
		memberError = null;
		try {
			await projects.upsertMember(id, addMemberUserID, addMemberRole);
			const om = orgMembers.find((m) => m.user_id === addMemberUserID);
			if (om && detail) {
				const newMember: ProjectMember = {
					user_id: om.user_id,
					email: om.email,
					name: om.name,
					role: addMemberRole,
					added_at: new Date().toISOString()
				};
				detail.members = [...detail.members, newMember];
				detail.member_count = detail.members.length;
			}
			addingMember = false;
			addMemberUserID = '';
			addMemberRole = 'member';
		} catch (e) {
			memberError = (e as Error).message;
		} finally {
			savingMember = false;
		}
	}

	async function changeRole(userID: string, role: string) {
		try {
			await projects.upsertMember(id, userID, role as 'admin' | 'member' | 'viewer');
			if (detail) {
				detail.members = detail.members.map((m) =>
					m.user_id === userID ? { ...m, role: role as ProjectMember['role'] } : m
				);
			}
		} catch (e) {
			toast.error((e as Error).message);
		}
	}

	async function removeMember(userID: string) {
		if (!confirm('Remove this member from the project?')) return;
		try {
			await projects.removeMember(id, userID);
			if (detail) {
				detail.members = detail.members.filter((m) => m.user_id !== userID);
				detail.member_count = detail.members.length;
			}
		} catch (e) {
			toast.error((e as Error).message);
		}
	}

	const toolLabel: Record<string, string> = {
		opentofu: 'OpenTofu',
		terraform: 'Terraform',
		ansible: 'Ansible',
		pulumi: 'Pulumi'
	};
</script>

<div class="p-6 space-y-6">
	{#if loading}
		<div class="flex items-center justify-center py-20">
			<span class="text-zinc-500 text-sm">Loading…</span>
		</div>
	{:else if error}
		<div class="rounded-lg border border-red-800 bg-red-950 px-4 py-3 text-sm text-red-300">{error}</div>
	{:else if detail}
		<!-- Header -->
		<div class="flex items-start justify-between gap-4">
			<div class="min-w-0">
				<div class="flex items-center gap-3">
					<a href="/projects" class="text-zinc-500 hover:text-zinc-300 text-sm transition-colors">Projects</a>
					<svg class="h-3 w-3 text-zinc-600 flex-shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="m9 18 6-6-6-6"/></svg>
					<h1 class="text-lg font-semibold text-white truncate">{detail.name}</h1>
					<span class="text-[10px] font-mono text-zinc-600 bg-zinc-800 rounded px-1.5 py-0.5 flex-shrink-0">{detail.slug}</span>
				</div>
				{#if detail.description}
					<p class="text-sm text-zinc-500 mt-1">{detail.description}</p>
				{/if}
			</div>
			{#if isAdmin}
				<div class="flex gap-2 flex-shrink-0">
					<button
						onclick={() => { editing = !editing; if (!editing) { form = { name: detail!.name, description: detail!.description }; editError = null; } }}
						class="rounded-lg border border-zinc-700 px-3 py-1.5 text-sm text-zinc-300 hover:bg-zinc-800 transition-colors">
						{editing ? 'Cancel' : 'Edit'}
					</button>
					<button
						onclick={deleteProject}
						class="rounded-lg border border-red-800 px-3 py-1.5 text-sm text-red-400 hover:bg-red-950 transition-colors">
						Delete
					</button>
				</div>
			{/if}
		</div>

		<!-- Edit form -->
		{#if editing}
			<div class="rounded-xl border border-zinc-800 p-5 space-y-4">
				<h2 class="text-sm font-medium text-zinc-300">Edit project</h2>
				{#if editError}
					<div class="rounded-lg border border-red-800 bg-red-950 px-4 py-3 text-sm text-red-300">{editError}</div>
				{/if}
				<form onsubmit={saveEdit} class="space-y-4">
					<div class="grid grid-cols-2 gap-4">
						<div class="space-y-1.5">
							<label class="field-label" for="edit-name">Name <span class="text-red-400">*</span></label>
							<input id="edit-name" class="field-input" bind:value={form.name} required />
						</div>
						<div class="space-y-1.5">
							<label class="field-label" for="edit-desc">Description</label>
							<input id="edit-desc" class="field-input" bind:value={form.description} />
						</div>
					</div>
					<div class="flex justify-end gap-3">
						<button type="button" onclick={() => { editing = false; editError = null; form = { name: detail!.name, description: detail!.description }; }} class="rounded-lg border border-zinc-700 px-4 py-1.5 text-sm text-zinc-300 hover:bg-zinc-800">Cancel</button>
						<button type="submit" disabled={saving} class="rounded-lg bg-teal-600 px-4 py-1.5 text-sm text-white hover:bg-teal-500 disabled:opacity-50">
							{saving ? 'Saving…' : 'Save changes'}
						</button>
					</div>
				</form>
			</div>
		{/if}

		<!-- Tabs -->
		<div class="border-b border-zinc-800">
			<div class="flex gap-1">
				{#each [['stacks', `Stacks (${detail.stack_count})`], ['members', `Members (${detail.member_count})`]] as [tab, label]}
					<button
						onclick={() => { activeTab = tab as 'stacks' | 'members'; }}
						class="px-4 py-2.5 text-sm font-medium transition-colors border-b-2 -mb-px"
						style={activeTab === tab
							? 'color: var(--accent); border-color: var(--accent);'
							: 'color: var(--color-zinc-500); border-color: transparent;'}
					>{label}</button>
				{/each}
			</div>
		</div>

		<!-- Stacks tab -->
		{#if activeTab === 'stacks'}
			{#if detail.stacks.length === 0}
				<div class="rounded-xl border border-zinc-800 p-8 text-center space-y-2">
					<p class="text-zinc-400 text-sm font-medium">No stacks in this project</p>
					<p class="text-zinc-600 text-xs">Assign stacks to this project from the stack's settings page.</p>
				</div>
			{:else}
				<div class="rounded-xl border border-zinc-800 overflow-hidden">
					<table class="w-full text-sm">
						<thead>
							<tr class="border-b border-zinc-800 text-left text-xs text-zinc-500 uppercase tracking-wider">
								<th class="px-4 py-3 font-medium">Name</th>
								<th class="px-4 py-3 font-medium">Tool</th>
								<th class="px-4 py-3 font-medium">Branch</th>
								<th class="px-4 py-3 font-medium">Updated</th>
							</tr>
						</thead>
						<tbody>
							{#each detail.stacks as s (s.id)}
								<tr class="border-b border-zinc-800 last:border-0 hover:bg-zinc-800/40 transition-colors">
									<td class="px-4 py-3">
										<a href="/stacks/{s.id}" class="font-medium text-white hover:text-teal-400 transition-colors">{s.name}</a>
										{#if s.description}
											<div class="text-xs text-zinc-500 mt-0.5 truncate max-w-xs">{s.description}</div>
										{/if}
									</td>
									<td class="px-4 py-3 text-zinc-400">{toolLabel[s.tool] ?? s.tool}</td>
									<td class="px-4 py-3 font-mono text-xs text-zinc-400">{s.repo_branch}</td>
									<td class="px-4 py-3 text-zinc-500">{new Date(s.updated_at).toLocaleDateString()}</td>
								</tr>
							{/each}
						</tbody>
					</table>
				</div>
			{/if}
		{/if}

		<!-- Members tab -->
		{#if activeTab === 'members'}
			<div class="space-y-4">
				<div class="flex items-center justify-between">
					<p class="text-sm text-zinc-500">
						{detail.members.length === 0
							? 'No explicit members — all org members inherit access at their org role.'
							: `${detail.members.length} explicit ${detail.members.length === 1 ? 'member' : 'members'}.`}
					</p>
					{#if isAdmin}
						<button
							onclick={showAddMember}
							class="rounded-lg bg-teal-600 px-3 py-1.5 text-sm text-white hover:bg-teal-500 transition-colors">
							Add member
						</button>
					{/if}
				</div>

				{#if addingMember}
					<div class="rounded-xl border border-zinc-800 p-5 space-y-4">
						<h2 class="text-sm font-medium text-zinc-300">Add member</h2>
						{#if memberError}
							<div class="rounded-lg border border-red-800 bg-red-950 px-4 py-3 text-sm text-red-300">{memberError}</div>
						{/if}
						<form onsubmit={addMember} class="flex items-end gap-3">
							<div class="flex-1 space-y-1.5">
								<label class="field-label" for="add-user">Org member <span class="text-red-400">*</span></label>
								<select id="add-user" class="field-input" bind:value={addMemberUserID} required>
									<option value="">Select a member…</option>
									{#each availableOrgMembers as m (m.user_id)}
										<option value={m.user_id}>{m.name || m.email} ({m.email})</option>
									{/each}
								</select>
							</div>
							<div class="w-36 space-y-1.5">
								<label class="field-label" for="add-role">Role</label>
								<select id="add-role" class="field-input" bind:value={addMemberRole}>
									<option value="admin">admin</option>
									<option value="member">member</option>
									<option value="viewer">viewer</option>
								</select>
							</div>
							<div class="flex gap-2 flex-shrink-0">
								<button type="button" onclick={() => { addingMember = false; memberError = null; }} class="rounded-lg border border-zinc-700 px-3 py-1.5 text-sm text-zinc-300 hover:bg-zinc-800">Cancel</button>
								<button type="submit" disabled={savingMember} class="rounded-lg bg-teal-600 px-3 py-1.5 text-sm text-white hover:bg-teal-500 disabled:opacity-50">
									{savingMember ? 'Adding…' : 'Add'}
								</button>
							</div>
						</form>
					</div>
				{/if}

				{#if detail.members.length > 0}
					<div class="rounded-xl border border-zinc-800 overflow-hidden">
						<table class="w-full text-sm">
							<thead>
								<tr class="border-b border-zinc-800 text-left text-xs text-zinc-500 uppercase tracking-wider">
									<th class="px-4 py-3 font-medium">Member</th>
									<th class="px-4 py-3 font-medium">Role</th>
									<th class="px-4 py-3 font-medium">Added</th>
									{#if isAdmin}<th class="px-4 py-3"></th>{/if}
								</tr>
							</thead>
							<tbody>
								{#each detail.members as m (m.user_id)}
									<tr class="border-b border-zinc-800 last:border-0">
										<td class="px-4 py-3">
											<p class="text-zinc-100">{m.name || m.email}</p>
											{#if m.name}<p class="text-xs text-zinc-500">{m.email}</p>{/if}
										</td>
										<td class="px-4 py-3">
											{#if isAdmin}
												<select
													value={m.role}
													onchange={(e) => changeRole(m.user_id, (e.target as HTMLSelectElement).value)}
													class="field-input py-1 text-xs w-28">
													<option value="admin">admin</option>
													<option value="member">member</option>
													<option value="viewer">viewer</option>
												</select>
											{:else}
												<span class="text-xs text-zinc-400">{m.role}</span>
											{/if}
										</td>
										<td class="px-4 py-3 text-zinc-500">{new Date(m.added_at).toLocaleDateString()}</td>
										{#if isAdmin}
											<td class="px-4 py-3 text-right">
												<button
													onclick={() => removeMember(m.user_id)}
													class="text-xs text-red-400 hover:text-red-300 transition-colors">Remove</button>
											</td>
										{/if}
									</tr>
								{/each}
							</tbody>
						</table>
					</div>
				{/if}
			</div>
		{/if}
	{/if}
</div>
