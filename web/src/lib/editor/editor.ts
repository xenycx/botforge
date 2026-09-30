// CodeMirror 6 is loaded on demand so the dashboard stays small.
import type { EditorView } from '@codemirror/view';

export type EditorHandle = {
	view: EditorView;
	getText(): string;
	setText(text: string): void;
	destroy(): void;
};

async function languageFor(name: string) {
	const ext = name.includes('.') ? name.split('.').pop()!.toLowerCase() : '';
	switch (ext) {
		case 'js':
		case 'mjs':
		case 'cjs':
		case 'jsx':
		case 'ts':
		case 'tsx': {
			const { javascript } = await import('@codemirror/lang-javascript');
			return javascript({ typescript: ext.startsWith('ts'), jsx: ext.endsWith('x') });
		}
		case 'py': {
			const { python } = await import('@codemirror/lang-python');
			return python();
		}
		case 'json': {
			const { json } = await import('@codemirror/lang-json');
			return json();
		}
		case 'rs': {
			const { rust } = await import('@codemirror/lang-rust');
			return rust();
		}
		case 'java': {
			const { java } = await import('@codemirror/lang-java');
			return java();
		}
		case 'yml':
		case 'yaml': {
			const { yaml } = await import('@codemirror/lang-yaml');
			return yaml();
		}
		case 'md': {
			const { markdown } = await import('@codemirror/lang-markdown');
			return markdown();
		}
		case 'go': {
			const [{ StreamLanguage }, { go }] = await Promise.all([import('@codemirror/language'), import('@codemirror/legacy-modes/mode/go')]);
			return StreamLanguage.define(go);
		}
		case 'rb': {
			const [{ StreamLanguage }, { ruby }] = await Promise.all([import('@codemirror/language'), import('@codemirror/legacy-modes/mode/ruby')]);
			return StreamLanguage.define(ruby);
		}
		case 'sh': {
			const [{ StreamLanguage }, { shell }] = await Promise.all([import('@codemirror/language'), import('@codemirror/legacy-modes/mode/shell')]);
			return StreamLanguage.define(shell);
		}
		case 'toml': {
			const [{ StreamLanguage }, { toml }] = await Promise.all([import('@codemirror/language'), import('@codemirror/legacy-modes/mode/toml')]);
			return StreamLanguage.define(toml);
		}
	}
	return null;
}

export async function createEditor(
	parent: HTMLElement,
	doc: string,
	filename: string,
	opts: { onChange(): void; onSave(): void }
): Promise<EditorHandle> {
	const [{ EditorView, keymap }, { EditorState }, { basicSetup }, lang, language, { tags: t }] = await Promise.all([
		import('@codemirror/view'),
		import('@codemirror/state'),
		import('codemirror'),
		languageFor(filename),
		import('@codemirror/language'),
		import('@lezer/highlight')
	]);
	const dark = document.documentElement.dataset.theme === 'dark';
	// Warm syntax colours for the dark theme; the light theme keeps CodeMirror's defaults.
	const darkHighlight = language.HighlightStyle.define([
		{ tag: [t.keyword, t.modifier, t.operatorKeyword], color: '#ff8a4c' },
		{ tag: [t.string, t.special(t.string), t.regexp], color: '#9fd89f' },
		{ tag: [t.number, t.bool, t.null, t.atom], color: '#f2c26b' },
		{ tag: [t.comment, t.meta], color: '#7d726b', fontStyle: 'italic' },
		{ tag: [t.function(t.variableName), t.function(t.propertyName)], color: '#7cc4ff' },
		{ tag: [t.typeName, t.className, t.namespace], color: '#e7b3ff' },
		{ tag: [t.propertyName, t.attributeName], color: '#f0d9c8' },
		{ tag: [t.definition(t.variableName)], color: '#ffd7b8' },
		{ tag: t.heading, color: '#ff8a4c', fontWeight: '600' },
		{ tag: t.link, color: '#7cc4ff', textDecoration: 'underline' },
		{ tag: t.invalid, color: '#ff6a55' }
	]);
	const theme = EditorView.theme({
		'&': { height: '100%', backgroundColor: 'var(--color-raised)', color: 'var(--color-ink)' },
		'.cm-scroller': { fontFamily: 'var(--font-mono)', fontSize: '13px', lineHeight: '1.6', fontVariantLigatures: 'none' },
		'.cm-gutters': { backgroundColor: 'var(--color-panel)', color: 'var(--color-muted)', borderRight: '1px solid var(--color-rule-soft)' },
		'.cm-activeLine': { backgroundColor: 'color-mix(in srgb, var(--color-action) 5%, transparent)' },
		'.cm-activeLineGutter': { backgroundColor: 'color-mix(in srgb, var(--color-action) 8%, transparent)' },
		'&.cm-focused': { outline: '2px solid var(--color-action)', outlineOffset: '-2px' },
		'.cm-cursor': { borderLeftColor: 'var(--color-ink)' },
		'&.cm-focused .cm-selectionBackground, .cm-selectionBackground, ::selection': { backgroundColor: 'color-mix(in srgb, var(--color-action) 28%, transparent)' }
	}, { dark });
	const view = new EditorView({
		parent,
		state: EditorState.create({
			doc,
			extensions: [
				basicSetup,
				theme,
				...(dark ? [language.syntaxHighlighting(darkHighlight)] : []),
				...(lang ? [lang] : []),
				keymap.of([{ key: 'Mod-s', preventDefault: true, run: () => (opts.onSave(), true) }]),
				EditorView.updateListener.of((u) => u.docChanged && opts.onChange())
			]
		})
	});
	return {
		view,
		getText: () => view.state.doc.toString(),
		setText: (text) => view.dispatch({ changes: { from: 0, to: view.state.doc.length, insert: text } }),
		destroy: () => view.destroy()
	};
}
