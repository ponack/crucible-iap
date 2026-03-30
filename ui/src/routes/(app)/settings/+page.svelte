<script lang="ts">
	import { auth } from '$lib/stores/auth.svelte';
</script>

<div class="p-8 max-w-2xl">
	<h1 class="text-xl font-semibold text-white mb-6">Settings</h1>

	<div class="bg-zinc-900 border border-zinc-800 rounded-xl divide-y divide-zinc-800">
		<div class="px-6 py-4">
			<p class="text-xs text-zinc-500 uppercase tracking-widest mb-3">Account</p>
			<div class="space-y-1">
				<p class="text-sm text-zinc-100">{auth.user?.name || auth.user?.email}</p>
				<p class="text-xs text-zinc-500">{auth.user?.email}</p>
			</div>
		</div>

		<div class="px-6 py-4">
			<p class="text-xs text-zinc-500 uppercase tracking-widest mb-3">Version</p>
			<p class="text-sm text-zinc-400">Crucible IAP — self-hosted infrastructure automation</p>
		</div>
	</div>
</div>
