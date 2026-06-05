<script lang="ts">
	import { onMount } from 'svelte';
	import {
		policies,
		complianceApi,
		type Policy,
		type StackPolicyRef,
		type PolicyPack,
		type CatalogEntry
	} from '$lib/api/client';
	import { toast } from '$lib/stores/toasts.svelte';

	interface Props {
		stackID: string;
	}

	let { stackID }: Props = $props();

	let stackPolicies = $state<StackPolicyRef[]>([]);
	let allPolicies = $state<Policy[]>([]);
	let stackPolicyPacks = $state<PolicyPack[]>([]);
	let catalogEntries = $state<CatalogEntry[]>([]);
	let attachingPolicy = $state('');
	let attachingPackID = $state('');

	const unattachedPolicies = $derived(
		allPolicies.filter((p) => !stackPolicies.some((sp) => sp.policy_id === p.id))
	);

	const unattachedPacks = $derived(
		catalogEntries
			.filter((e) => e.installed && !stackPolicyPacks.some((p) => p.id === e.installed!.id))
			.map((e) => e.installed!)
	);

	onMount(async () => {
		const [forStack, all] = await Promise.all([
			policies.forStack(stackID).catch(() => [] as StackPolicyRef[]),
			policies.list().catch(() => [] as Policy[])
		]);
		stackPolicies = forStack;
		allPolicies = all;
		complianceApi.listStackPacks(stackID).then((r) => (stackPolicyPacks = r)).catch(() => {});
		complianceApi.getCatalog().then((r) => (catalogEntries = r)).catch(() => {});
	});

	async function attachPolicy() {
		if (!attachingPolicy) return;
		try {
			await policies.attach(stackID, attachingPolicy);
			stackPolicies = await policies.forStack(stackID);
			attachingPolicy = '';
		} catch (e) {
			toast.error((e as Error).message);
		}
	}

	async function detachPolicy(policyID: string) {
		try {
			await policies.detach(stackID, policyID);
			stackPolicies = stackPolicies.filter((p) => p.policy_id !== policyID);
		} catch (e) {
			toast.error((e as Error).message);
		}
	}

	async function attachPack() {
		if (!attachingPackID) return;
		try {
			await complianceApi.attachPack(stackID, attachingPackID);
			stackPolicyPacks = await complianceApi.listStackPacks(stackID);
			attachingPackID = '';
		} catch (e) {
			toast.error((e as Error).message);
		}
	}

	async function detachPack(packID: string) {
		try {
			await complianceApi.detachPack(stackID, packID);
			stackPolicyPacks = stackPolicyPacks.filter((p) => p.id !== packID);
		} catch (e) {
			toast.error((e as Error).message);
		}
	}
</script>

<section class="space-y-3">
	<h2 class="text-sm font-medium text-zinc-400 uppercase tracking-wide">Policies</h2>
	{#if stackPolicies.length > 0}
		<div class="border border-zinc-800 rounded-xl overflow-hidden">
			<table class="w-full text-sm">
				<thead class="bg-zinc-900 text-zinc-500 text-xs uppercase tracking-wide">
					<tr>
						<th class="text-left px-4 py-2">Name</th>
						<th class="text-left px-4 py-2">Type</th>
						<th class="text-left px-4 py-2">Status</th>
						<th class="px-4 py-2"></th>
					</tr>
				</thead>
				<tbody class="divide-y divide-zinc-700">
					{#each stackPolicies as sp (sp.policy_id)}
						<tr>
							<td class="px-4 py-2.5">
								<a href="/policies/{sp.policy_id}" class="text-zinc-200 hover:text-white">{sp.name}</a>
							</td>
							<td class="px-4 py-2.5 text-zinc-500 text-xs">{sp.type}</td>
							<td class="px-4 py-2.5">
								<span class="text-xs {sp.is_active ? 'text-green-400' : 'text-zinc-500'}">
									{sp.is_active ? 'Active' : 'Inactive'}
								</span>
							</td>
							<td class="px-4 py-2.5 text-right">
								<button onclick={() => detachPolicy(sp.policy_id)}
									class="text-xs text-zinc-500 hover:text-red-400">Remove</button>
							</td>
						</tr>
					{/each}
				</tbody>
			</table>
		</div>
	{:else}
		<p class="text-zinc-600 text-sm">No policies attached.</p>
	{/if}

	{#if unattachedPolicies.length > 0}
		<div class="flex items-center gap-2">
			<select class="field-input w-64" bind:value={attachingPolicy}>
				<option value="">— attach a policy —</option>
				{#each unattachedPolicies as p (p.id)}
					<option value={p.id}>{p.name} ({p.type})</option>
				{/each}
			</select>
			<button onclick={attachPolicy} disabled={!attachingPolicy}
				class="border border-zinc-700 hover:border-zinc-500 disabled:opacity-40 text-zinc-300 text-sm px-3 py-1.5 rounded-lg transition-colors">
				Attach
			</button>
		</div>
	{/if}
</section>

<section class="space-y-3">
	<h2 class="text-sm font-medium text-zinc-400 uppercase tracking-wide">Compliance Policy Packs</h2>
	{#if stackPolicyPacks.length > 0}
		<div class="border border-zinc-800 rounded-xl overflow-hidden">
			<table class="w-full text-sm">
				<thead class="bg-zinc-900 text-zinc-500 text-xs uppercase tracking-wide">
					<tr>
						<th class="text-left px-4 py-2">Pack</th>
						<th class="text-left px-4 py-2">Synced</th>
						<th class="text-left px-4 py-2">Policies</th>
						<th class="px-4 py-2"></th>
					</tr>
				</thead>
				<tbody class="divide-y divide-zinc-700">
					{#each stackPolicyPacks as pack (pack.id)}
						<tr>
							<td class="px-4 py-2.5 text-zinc-200">{pack.name}</td>
							<td class="px-4 py-2.5 text-zinc-500 text-xs">{pack.last_synced_at ? new Date(pack.last_synced_at).toLocaleDateString() : '—'}</td>
							<td class="px-4 py-2.5 text-zinc-500 text-xs">{pack.policy_count}</td>
							<td class="px-4 py-2.5 text-right">
								<button onclick={() => detachPack(pack.id)}
									class="text-xs text-zinc-500 hover:text-red-400">Remove</button>
							</td>
						</tr>
					{/each}
				</tbody>
			</table>
		</div>
	{:else}
		<p class="text-zinc-600 text-sm">No compliance packs attached.</p>
	{/if}

	{#if unattachedPacks.length > 0}
		<div class="flex items-center gap-2">
			<select class="field-input w-64" bind:value={attachingPackID}>
				<option value="">— attach a pack —</option>
				{#each unattachedPacks as pack (pack.id)}
					<option value={pack.id}>{pack.name}</option>
				{/each}
			</select>
			<button onclick={attachPack} disabled={!attachingPackID}
				class="border border-zinc-700 hover:border-zinc-500 disabled:opacity-40 text-zinc-300 text-sm px-3 py-1.5 rounded-lg transition-colors">
				Attach
			</button>
		</div>
	{:else if catalogEntries.length > 0 && catalogEntries.every((e) => !e.installed)}
		<p class="text-xs text-zinc-500">
			No compliance packs installed for this org. <a href="/policies/compliance-packs" class="text-teal-400 hover:underline">Install from the catalog.</a>
		</p>
	{/if}
</section>
