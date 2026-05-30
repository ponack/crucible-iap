<script lang="ts">
	// Generic typed-confirmation modal. Caller supplies the expected string
	// (typically the resource's name) and an onConfirm action. The confirm
	// button stays disabled until the user types the expected string exactly.
	// Modelled on the stack-destroy modal in stacks/[id]/+page.svelte so the
	// look matches what users already know.
	interface Props {
		open: boolean;
		title: string;
		message: string;
		warning?: string;
		expected: string;
		confirmLabel: string;
		confirmingLabel?: string;
		confirming?: boolean;
		danger?: boolean;
		onConfirm: () => void | Promise<void>;
		onCancel: () => void;
	}

	let {
		open,
		title,
		message,
		warning = '',
		expected,
		confirmLabel,
		confirmingLabel = 'Working…',
		confirming = false,
		danger = true,
		onConfirm,
		onCancel
	}: Props = $props();

	let typed = $state('');
	let inputEl = $state<HTMLInputElement | undefined>(undefined);

	$effect(() => {
		if (open) {
			typed = '';
			// Focus the input on next tick so it gets the cursor.
			queueMicrotask(() => inputEl?.focus());
		}
	});

	const accentBorder = $derived(danger ? 'border-red-900' : 'border-orange-900');
	const accentBg = $derived(danger ? 'bg-red-950/50 border-red-900 text-red-300' : 'bg-orange-950/50 border-orange-900 text-orange-300');
	const buttonBg = $derived(danger ? 'bg-red-700 hover:bg-red-600' : 'bg-orange-700 hover:bg-orange-600');
</script>

{#if open}
<div class="fixed inset-0 z-50 flex items-center justify-center bg-black/70 px-4">
	<div class="bg-zinc-900 border {accentBorder} rounded-2xl p-6 w-full max-w-md space-y-4 shadow-2xl">
		<div class="space-y-1">
			<h2 class="text-white font-semibold text-base">{title}</h2>
			<p class="text-zinc-400 text-sm">{message}</p>
		</div>
		{#if warning}
			<div class="rounded-lg px-4 py-3 text-xs space-y-1 border {accentBg}">
				<p>{warning}</p>
			</div>
		{/if}
		<div class="space-y-1.5">
			<label class="text-xs text-zinc-400" for="typed-confirm-input">
				Type <span class="font-mono text-white">{expected}</span> to confirm
			</label>
			<input
				id="typed-confirm-input"
				bind:this={inputEl}
				class="field-input"
				bind:value={typed}
				placeholder={expected}
				autocomplete="off"
				onkeydown={(e) => { if (e.key === 'Escape') onCancel(); }}
			/>
		</div>
		<div class="flex gap-3 pt-1">
			<button
				type="button"
				onclick={onConfirm}
				disabled={typed !== expected || confirming}
				class="flex-1 {buttonBg} disabled:opacity-40 disabled:cursor-not-allowed text-white text-sm px-4 py-2 rounded-lg transition-colors font-medium">
				{confirming ? confirmingLabel : confirmLabel}
			</button>
			<button
				type="button"
				onclick={onCancel}
				class="border border-zinc-700 hover:border-zinc-500 text-zinc-300 text-sm px-4 py-2 rounded-lg transition-colors">
				Cancel
			</button>
		</div>
	</div>
</div>
{/if}
