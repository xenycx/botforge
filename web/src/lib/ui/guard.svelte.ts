// Unsaved-change protection. Editors register a probe; route changes, bot
// section changes and page exit ask before discarding work.
import { chooseDialog } from './dialogs.svelte';

export type DirtyEntry = {
	/** What has unsaved changes, e.g. "index.js" or "Startup settings". */
	label: string;
	isDirty: () => boolean;
	/** Saves; resolves true on success. Omit when saving here is not possible. */
	save?: () => Promise<boolean>;
};

const entries = new Set<DirtyEntry>();

/** Registers an entry; call the returned function on destroy. */
export function registerDirty(e: DirtyEntry): () => void {
	entries.add(e);
	return () => entries.delete(e);
}

export function dirtyEntries(): DirtyEntry[] {
	return [...entries].filter((e) => e.isDirty());
}

/**
 * Resolves true when it is fine to leave: nothing is dirty, the user saved
 * successfully, or chose to discard. Cancel resolves false.
 */
export async function confirmLeave(): Promise<boolean> {
	const dirty = dirtyEntries();
	if (!dirty.length) return true;
	const names = dirty.map((d) => d.label);
	const canSave = dirty.every((d) => d.save);
	const choice = await chooseDialog({
		title: 'Unsaved changes',
		body:
			names.length === 1
				? `${names[0]} has changes that are not saved yet.`
				: `These have changes that are not saved yet: ${names.join(', ')}.`,
		cancelLabel: 'Keep editing',
		choices: [
			{ value: 'discard', label: 'Discard changes', tone: 'danger' },
			...(canSave ? [{ value: 'save', label: 'Save and continue', tone: 'primary' as const }] : [])
		]
	});
	if (choice === 'discard') return true;
	if (choice === 'save') {
		for (const d of dirty) if (!(await d.save!())) return false;
		return true;
	}
	return false;
}
