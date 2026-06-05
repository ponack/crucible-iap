<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';

	// `g` prefix navigation — second key arrives within this window or is dropped.
	const PREFIX_TIMEOUT_MS = 1500;

	const goTargets: Record<string, { href: string; label: string }> = {
		d: { href: '/dashboard', label: 'Dashboard' },
		p: { href: '/projects', label: 'Projects' },
		s: { href: '/stacks', label: 'Stacks' },
		r: { href: '/runs', label: 'Runs' },
		l: { href: '/policies', label: 'Policies' },
		a: { href: '/audit', label: 'Audit Log' },
		o: { href: '/worker-pools', label: 'Worker Pools' },
		',': { href: '/settings', label: 'Settings' }
	};

	let helpOpen = $state(false);
	let pendingPrefix = $state<string | null>(null);
	let prefixTimer: ReturnType<typeof setTimeout> | null = null;

	function clearPrefix() {
		pendingPrefix = null;
		if (prefixTimer) {
			clearTimeout(prefixTimer);
			prefixTimer = null;
		}
	}

	function isTypingTarget(el: EventTarget | null): boolean {
		if (!(el instanceof HTMLElement)) return false;
		const tag = el.tagName;
		if (tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT') return true;
		return el.isContentEditable;
	}

	function focusSearch(): boolean {
		const input = document.querySelector<HTMLInputElement>('input[type="search"]');
		if (!input) return false;
		input.focus();
		input.select();
		return true;
	}

	function onKey(e: KeyboardEvent) {
		// Modifier combos belong to the browser / OS — never intercept.
		if (e.ctrlKey || e.metaKey || e.altKey) return;

		// Esc always closes the help modal, even from inputs.
		if (e.key === 'Escape' && helpOpen) {
			helpOpen = false;
			e.preventDefault();
			return;
		}

		if (isTypingTarget(e.target)) return;

		if (pendingPrefix === 'g') {
			const target = goTargets[e.key];
			clearPrefix();
			if (target) {
				e.preventDefault();
				goto(target.href);
			}
			return;
		}

		if (e.key === 'g') {
			pendingPrefix = 'g';
			prefixTimer = setTimeout(clearPrefix, PREFIX_TIMEOUT_MS);
			e.preventDefault();
			return;
		}

		if (e.key === '?') {
			helpOpen = !helpOpen;
			e.preventDefault();
			return;
		}

		if (e.key === '/') {
			if (focusSearch()) e.preventDefault();
			return;
		}
	}

	onMount(() => {
		window.addEventListener('keydown', onKey);
		return () => {
			window.removeEventListener('keydown', onKey);
			if (prefixTimer) clearTimeout(prefixTimer);
		};
	});

	const sections: { title: string; rows: { keys: string[]; label: string }[] }[] = [
		{
			title: 'Navigation',
			rows: [
				{ keys: ['g', 'd'], label: 'Go to Dashboard' },
				{ keys: ['g', 'p'], label: 'Go to Projects' },
				{ keys: ['g', 's'], label: 'Go to Stacks' },
				{ keys: ['g', 'r'], label: 'Go to Runs' },
				{ keys: ['g', 'l'], label: 'Go to Policies' },
				{ keys: ['g', 'a'], label: 'Go to Audit Log' },
				{ keys: ['g', 'o'], label: 'Go to Worker Pools' },
				{ keys: ['g', ','], label: 'Go to Settings' }
			]
		},
		{
			title: 'On the current page',
			rows: [
				{ keys: ['/'], label: 'Focus search / filter' },
				{ keys: ['Esc'], label: 'Close this dialog' }
			]
		},
		{
			title: 'Help',
			rows: [{ keys: ['?'], label: 'Show this shortcuts list' }]
		}
	];
</script>

{#if pendingPrefix}
	<div class="fixed bottom-4 left-4 z-40 rounded-lg border border-zinc-700 bg-zinc-900 px-3 py-1.5 text-xs text-zinc-300 shadow-lg">
		<kbd class="font-mono font-semibold text-white">{pendingPrefix}</kbd>
		<span class="text-zinc-500">…</span>
	</div>
{/if}

{#if helpOpen}
	<!-- svelte-ignore a11y_click_events_have_key_events a11y_no_static_element_interactions -->
	<div
		class="fixed inset-0 z-50 flex items-center justify-center bg-black/70 px-4"
		onclick={() => (helpOpen = false)}
	>
		<!-- svelte-ignore a11y_click_events_have_key_events a11y_no_static_element_interactions -->
		<div
			class="w-full max-w-lg space-y-4 rounded-2xl border border-zinc-700 bg-zinc-900 p-6 shadow-2xl"
			onclick={(e) => e.stopPropagation()}
		>
			<div class="flex items-start justify-between">
				<div>
					<h2 class="text-base font-semibold text-white">Keyboard shortcuts</h2>
					<p class="mt-0.5 text-xs text-zinc-500">Press <kbd class="kbd">?</kbd> any time to open this list.</p>
				</div>
				<button
					onclick={() => (helpOpen = false)}
					class="text-zinc-500 transition-colors hover:text-zinc-200"
					aria-label="Close"
				>
					<svg class="h-4 w-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
						<path d="M18 6 6 18M6 6l12 12" />
					</svg>
				</button>
			</div>

			{#each sections as section}
				<div class="space-y-1.5">
					<p class="text-xs font-medium uppercase tracking-wide text-zinc-500">{section.title}</p>
					<ul class="space-y-1.5">
						{#each section.rows as row}
							<li class="flex items-center justify-between text-sm">
								<span class="text-zinc-300">{row.label}</span>
								<span class="flex items-center gap-1">
									{#each row.keys as k}
										<kbd class="kbd">{k}</kbd>
									{/each}
								</span>
							</li>
						{/each}
					</ul>
				</div>
			{/each}
		</div>
	</div>
{/if}

<style>
	:global(.kbd) {
		display: inline-flex;
		align-items: center;
		justify-content: center;
		min-width: 1.5rem;
		padding: 0.125rem 0.4rem;
		font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
		font-size: 0.75rem;
		color: #fff;
		background: var(--color-zinc-800);
		border: 1px solid var(--color-zinc-700);
		border-bottom-width: 2px;
		border-radius: 0.375rem;
	}
</style>
