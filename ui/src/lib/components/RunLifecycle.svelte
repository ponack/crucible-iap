<script lang="ts">
	interface Props {
		status: string;
	}
	const { status }: Props = $props();

	const steps = [
		{ label: 'Queued' },
		{ label: 'Planning' },
		{ label: 'Review' },
		{ label: 'Applying' },
		{ label: 'Done' }
	];

	const statusStep: Record<string, number> = {
		queued: 0,
		preparing: 1,
		planning: 1,
		unconfirmed: 2,
		pending_approval: 2,
		confirmed: 2,
		applying: 3,
		finished: 4,
		failed: 4,
		canceled: 4,
		discarded: 4
	};

	const isError = $derived(['failed', 'canceled', 'discarded'].includes(status));
	const activeStep = $derived(statusStep[status] ?? 0);
	const isDone = $derived(activeStep === 4);
</script>

<div class="flex items-center gap-0 px-6 py-3" style="border-bottom: 1px solid var(--color-zinc-800);">
	{#each steps as step, i}
		{@const isActive = i === activeStep && !isDone}
		{@const isPast = i < activeStep}
		{@const isFinal = i === 4 && isDone}

		<!-- Step node -->
		<div class="flex items-center gap-0">
			<div class="flex flex-col items-center gap-1">
				<div
					class="h-7 w-7 rounded-full flex items-center justify-center text-xs font-medium transition-all duration-200 relative"
					style={
						isFinal && isError
							? 'background: rgba(239,68,68,0.12); border: 1.5px solid rgb(239,68,68); color: rgb(248,113,113);'
							: isFinal
							? 'background: rgba(45,212,191,0.12); border: 1.5px solid var(--accent); color: var(--accent);'
							: isActive
							? 'background: var(--accent-muted); border: 1.5px solid var(--accent); color: var(--accent);'
							: isPast
							? 'background: rgba(45,212,191,0.08); border: 1.5px solid rgba(45,212,191,0.35); color: rgba(45,212,191,0.7);'
							: 'background: var(--color-zinc-800); border: 1.5px solid var(--color-zinc-700); color: var(--color-zinc-600);'
					}
				>
					{#if isFinal && isError}
						<!-- X mark -->
						<svg class="h-3.5 w-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round">
							<path d="M6 18 18 6M6 6l12 12"/>
						</svg>
					{:else if isFinal || isPast}
						<!-- Check mark -->
						<svg class="h-3.5 w-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round">
							<path d="m4.5 12.75 6 6 9-13.5"/>
						</svg>
					{:else if isActive}
						<!-- Pulse dot -->
						<span class="h-2 w-2 rounded-full animate-pulse" style="background: var(--accent);"></span>
					{:else}
						<span class="h-1.5 w-1.5 rounded-full" style="background: var(--color-zinc-600);"></span>
					{/if}
				</div>
				<span
					class="text-[10px] font-medium whitespace-nowrap"
					style={
						isFinal && isError
							? 'color: rgb(248,113,113);'
							: isFinal || isActive
							? 'color: var(--accent);'
							: isPast
							? 'color: rgba(45,212,191,0.6);'
							: 'color: var(--color-zinc-600);'
					}
				>
					{step.label}
				</span>
			</div>

			<!-- Connector line (not after last step) -->
			{#if i < steps.length - 1}
				<div
					class="w-10 h-px mx-1 mb-4 transition-colors duration-300"
					style={isPast || (isActive && i < activeStep)
						? 'background: rgba(45,212,191,0.35);'
						: 'background: var(--color-zinc-800);'}
				></div>
			{/if}
		</div>
	{/each}
</div>
