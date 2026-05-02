<script lang="ts">
	import { onMount, onDestroy } from 'svelte';
	import { EditorView, basicSetup } from 'codemirror';
	import { EditorState } from '@codemirror/state';
	import { oneDark } from '@codemirror/theme-one-dark';
	import { StreamLanguage, StringStream } from '@codemirror/language';

	// Minimal Rego stream tokenizer for syntax highlighting
	const regoLanguage = StreamLanguage.define({
		token(stream: StringStream) {
			if (stream.eatSpace()) return null;
			// Comments
			if (stream.match(/^#.*/)) return 'comment';
			// Strings
			if (stream.match(/^"(?:[^"\\]|\\.)*"/)) return 'string';
			// Numbers
			if (stream.match(/^\d+(\.\d+)?/)) return 'number';
			// Keywords
			if (
				stream.match(
					/^(?:package|import|default|if|else|true|false|null|as|not|with|every|some|in|contains)\b/
				)
			)
				return 'keyword';
			// Operators
			if (stream.match(/^(?::=|==|!=|<=|>=|[+\-*/%<>=!|&])/)) return 'operator';
			// Punctuation
			if (stream.match(/^[{}[\]().,;]/)) return 'punctuation';
			// Identifiers
			if (stream.match(/^[a-zA-Z_]\w*/)) return 'variableName';
			stream.next();
			return null;
		}
	});

	let {
		value = $bindable(''),
		readonly = false,
		minLines = 14
	}: {
		value?: string;
		readonly?: boolean;
		minLines?: number;
	} = $props();

	let container: HTMLDivElement;
	let view: EditorView | null = null;
	// Prevent feedback loop: track whether the last change came from inside the editor
	let internalChange = false;

	onMount(() => {
		view = new EditorView({
			state: EditorState.create({
				doc: value,
				extensions: [
					basicSetup,
					oneDark,
					regoLanguage,
					EditorView.lineWrapping,
					EditorView.editable.of(!readonly),
					EditorView.updateListener.of((update) => {
						if (update.docChanged) {
							internalChange = true;
							value = update.state.doc.toString();
							internalChange = false;
						}
					}),
					EditorView.theme({
						'&': { minHeight: `${minLines * 1.6}rem` },
						'.cm-content': { minHeight: `${minLines * 1.6}rem` },
						'.cm-scroller': {
							overflow: 'auto',
							fontFamily: 'ui-monospace, SFMono-Regular, Menlo, monospace',
							fontSize: '12px'
						},
						'&.cm-editor': { borderRadius: '0' },
						'&.cm-focused': { outline: 'none' }
					})
				]
			}),
			parent: container
		});
	});

	// Sync external value changes (e.g. template selection) into the editor
	$effect(() => {
		if (view && !internalChange && value !== view.state.doc.toString()) {
			view.dispatch({
				changes: { from: 0, to: view.state.doc.length, insert: value }
			});
		}
	});

	onDestroy(() => {
		view?.destroy();
	});
</script>

<div
	bind:this={container}
	class="overflow-hidden rounded-lg border border-zinc-700 transition-colors focus-within:border-teal-500"
></div>
