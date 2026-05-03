<script lang="ts">
	import { orgTags, type Tag } from '$lib/api/client';
	import { onMount } from 'svelte';
	import { toast } from '$lib/stores/toasts.svelte';

	let tags = $state<Tag[]>([]);
	let loading = $state(true);

	// New tag form
	let newName = $state('');
	let newColor = $state('#6B7280');
	let creating = $state(false);

	// Inline edit state
	let editingID = $state<string | null>(null);
	let editName = $state('');
	let editColor = $state('');
	let saving = $state(false);

	const PRESET_COLORS = [
		'#6B7280', // zinc
		'#2DD4BF', // teal
		'#60A5FA', // blue
		'#818CF8', // indigo
		'#A78BFA', // violet
		'#F472B6', // pink
		'#FB923C', // orange
		'#FBBF24', // amber
		'#A3E635', // lime
		'#34D399', // emerald
		'#F87171', // red
		'#E05252'  // dark red
	];

	async function load() {
		loading = true;
		try {
			tags = await orgTags.list();
		} catch (e) {
			toast.error((e as Error).message);
		} finally {
			loading = false;
		}
	}

	onMount(load);

	async function create() {
		if (!newName.trim()) return;
		creating = true;
		try {
			await orgTags.create(newName.trim(), newColor);
			newName = '';
			newColor = '#6B7280';
			await load();
			toast.success('Tag created');
		} catch (e) {
			toast.error((e as Error).message);
		} finally {
			creating = false;
		}
	}

	function startEdit(tag: Tag) {
		editingID = tag.id;
		editName = tag.name;
		editColor = tag.color;
	}

	function cancelEdit() {
		editingID = null;
	}

	async function saveEdit() {
		if (!editingID) return;
		saving = true;
		try {
			await orgTags.update(editingID, { name: editName.trim(), color: editColor });
			await load();
			editingID = null;
			toast.success('Tag updated');
		} catch (e) {
			toast.error((e as Error).message);
		} finally {
			saving = false;
		}
	}

	async function deleteTag(tag: Tag) {
		if (!confirm(`Delete tag "${tag.name}"? It will be removed from all stacks.`)) return;
		try {
			await orgTags.delete(tag.id);
			await load();
			toast.success('Tag deleted');
		} catch (e) {
			toast.error((e as Error).message);
		}
	}
</script>

<div class="max-w-2xl space-y-6">
	<div>
		<h2 class="text-base font-semibold text-white">Tags</h2>
		<p class="text-sm text-zinc-400 mt-0.5">
			Org-wide labels for organising stacks. Tag stacks from the stack list or stack settings.
		</p>
	</div>

	<!-- Create tag -->
	<div class="border border-zinc-800 rounded-xl p-5 space-y-4">
		<p class="text-sm font-medium text-zinc-300">New tag</p>
		<div class="flex items-end gap-3 flex-wrap">
			<div class="flex-1 min-w-40 space-y-1">
				<label for="tag-name" class="text-xs text-zinc-500">Name</label>
				<input
					id="tag-name"
					type="text"
					bind:value={newName}
					placeholder="e.g. env:prod"
					onkeydown={(e) => e.key === 'Enter' && create()}
					class="field-input"
				/>
			</div>
			<div class="space-y-1">
				<label class="text-xs text-zinc-500">Color</label>
				<div class="flex items-center gap-1.5 flex-wrap">
					{#each PRESET_COLORS as c}
						<button
							onclick={() => (newColor = c)}
							class="w-5 h-5 rounded-full transition-all flex-shrink-0 {newColor === c ? 'ring-2 ring-offset-2' : 'opacity-60 hover:opacity-100'}"
							style="background: {c}; {newColor === c ? `ring-color: ${c}; ring-offset-color: var(--color-zinc-900);` : ''}"
							title={c}
						></button>
					{/each}
				</div>
			</div>
			<button onclick={create} disabled={creating || !newName.trim()}
				class="px-4 py-2 text-sm font-medium rounded-lg transition-colors disabled:opacity-50"
				style="background: var(--accent-muted); color: var(--accent); border: 1px solid var(--accent-border);">
				{creating ? 'Creating…' : 'Create'}
			</button>
		</div>
		<!-- Preview pill -->
		{#if newName.trim()}
			<div class="flex items-center gap-2">
				<span class="text-xs text-zinc-500">Preview:</span>
				<span class="inline-flex items-center gap-1 text-xs px-2.5 py-1 rounded-full border"
					style="border-color: {newColor}33; background: {newColor}18; color: var(--color-zinc-200);">
					<span class="w-2 h-2 rounded-full flex-shrink-0" style="background: {newColor};"></span>
					{newName.trim()}
				</span>
			</div>
		{/if}
	</div>

	<!-- Existing tags -->
	{#if loading}
		<p class="text-zinc-500 text-sm">Loading…</p>
	{:else if tags.length === 0}
		<p class="text-zinc-600 text-sm">No tags yet. Create your first tag above.</p>
	{:else}
		<div class="border border-zinc-800 rounded-xl overflow-hidden divide-y divide-zinc-800">
			{#each tags as tag (tag.id)}
				<div class="px-4 py-3">
					{#if editingID === tag.id}
						<!-- Edit row -->
						<div class="flex items-center gap-3 flex-wrap">
							<input type="text" bind:value={editName}
								onkeydown={(e) => e.key === 'Enter' && saveEdit()}
								class="field-input flex-1 min-w-32 py-1.5" />
							<div class="flex items-center gap-1.5 flex-wrap">
								{#each PRESET_COLORS as c}
									<button
										onclick={() => (editColor = c)}
										class="w-4 h-4 rounded-full transition-all flex-shrink-0 {editColor === c ? 'ring-2 ring-offset-1' : 'opacity-50 hover:opacity-100'}"
										style="background: {c}; {editColor === c ? `ring-color: ${c}; ring-offset-color: var(--color-zinc-900);` : ''}"
									></button>
								{/each}
							</div>
							<div class="flex items-center gap-2">
								<button onclick={saveEdit} disabled={saving}
									class="text-xs px-3 py-1.5 rounded-lg transition-colors disabled:opacity-50"
									style="background: var(--accent-muted); color: var(--accent); border: 1px solid var(--accent-border);">
									{saving ? 'Saving…' : 'Save'}
								</button>
								<button onclick={cancelEdit} class="text-xs text-zinc-500 hover:text-zinc-300 transition-colors px-2 py-1.5">
									Cancel
								</button>
							</div>
						</div>
					{:else}
						<!-- Display row -->
						<div class="flex items-center gap-3">
							<span class="inline-flex items-center gap-1.5 text-sm px-2.5 py-1 rounded-full border flex-shrink-0"
								style="border-color: {tag.color}33; background: {tag.color}18; color: var(--color-zinc-200);">
								<span class="w-2 h-2 rounded-full flex-shrink-0" style="background: {tag.color};"></span>
								{tag.name}
							</span>
							<span class="text-xs text-zinc-600 flex-1">
								{tag.stack_count} {tag.stack_count === 1 ? 'stack' : 'stacks'}
							</span>
							<div class="flex items-center gap-2">
								<button onclick={() => startEdit(tag)}
									class="text-xs text-zinc-500 hover:text-zinc-300 transition-colors">
									Edit
								</button>
								<button onclick={() => deleteTag(tag)}
									class="text-xs text-zinc-600 hover:text-red-400 transition-colors">
									Delete
								</button>
							</div>
						</div>
					{/if}
				</div>
			{/each}
		</div>
	{/if}
</div>
