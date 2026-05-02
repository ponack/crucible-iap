<script lang="ts">
	import { toast, dismiss, type Toast } from '$lib/stores/toasts.svelte';

	function icon(type: Toast['type']) {
		if (type === 'error') return 'M12 9v3.75m-9.303 3.376c-.866 1.5.217 3.374 1.948 3.374h14.71c1.73 0 2.813-1.874 1.948-3.374L13.949 3.378c-.866-1.5-3.032-1.5-3.898 0L2.697 16.126ZM12 15.75h.007v.008H12v-.008Z';
		if (type === 'success') return 'm4.5 12.75 6 6 9-13.5';
		return 'M11.25 11.25l.041-.02a.75.75 0 0 1 1.063.852l-.708 2.836a.75.75 0 0 0 1.063.853l.041-.021M21 12a9 9 0 1 1-18 0 9 9 0 0 1 18 0Zm-9-3.75h.008v.008H12V8.25Z';
	}
</script>

<div class="fixed bottom-4 right-4 z-50 flex flex-col gap-2 pointer-events-none" aria-live="polite">
	{#each toast.list as t (t.id)}
		<div
			class="pointer-events-auto flex items-start gap-3 rounded-xl px-4 py-3 shadow-lg text-sm max-w-sm w-full"
			style={
				t.type === 'error'
					? 'background: #2d0f0f; border: 1px solid #7f1d1d; color: #fca5a5;'
					: t.type === 'success'
					? 'background: #0d2320; border: 1px solid var(--accent-border); color: var(--accent);'
					: 'background: var(--color-zinc-800); border: 1px solid var(--color-zinc-700); color: var(--color-zinc-200);'
			}
		>
			<svg class="h-4 w-4 flex-shrink-0 mt-0.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round">
				<path d={icon(t.type)}/>
			</svg>
			<span class="flex-1 leading-snug">{t.message}</span>
			<button
				onclick={() => dismiss(t.id)}
				class="flex-shrink-0 opacity-50 hover:opacity-100 transition-opacity"
				aria-label="Dismiss"
			>
				<svg class="h-3.5 w-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
					<path d="M6 18 18 6M6 6l12 12"/>
				</svg>
			</button>
		</div>
	{/each}
</div>
