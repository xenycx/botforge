<script lang="ts">
	import { onMount, onDestroy } from 'svelte';
	import { api, ApiError, fetchRevision, putText, uploadProgress, fmtBytes } from '$lib/api/client';
	import type { FileEntry } from '$lib/api/types';
	import type { EditorHandle } from '$lib/editor/editor';
	import { chooseDialog, confirmDialog, promptDialog } from '$lib/ui/dialogs.svelte';
	import { registerDirty } from '$lib/ui/guard.svelte';
	import { toast } from '$lib/ui/toast.svelte';
	import Icon from '$lib/components/ui/Icon.svelte';
	import Menu from '$lib/components/ui/Menu.svelte';
	import Notice from '$lib/components/ui/Notice.svelte';

	let { botId, running }: { botId: string; running: boolean } = $props();

	let cwd = $state('');
	let entries = $state<FileEntry[] | null>(null);
	let listError = $state('');
	let filter = $state('');

	let open = $state<string | null>(null);
	let etag: string | null = null;
	let saved = ''; // text as last loaded/saved, to know whether edits remain
	let dirty = $state(false);
	let saving = $state(false);
	let saveError = $state('');
	let notEditable = $state('');
	let loadingFile = $state(false);
	let host: HTMLDivElement | undefined = $state();
	let editor: EditorHandle | null = null;

	type Upload = { name: string; progress: number; error?: string; ctrl: AbortController };
	let uploads = $state<Upload[]>([]);

	const base = (id: string) => `/bots/${id}/files`;
	const q = (p: string) => encodeURIComponent(p);
	const join = (dir: string, name: string) => (dir ? `${dir}/${name}` : name);
	const err = (e: unknown, fallback: string) => (e instanceof ApiError ? e.message : fallback);
	const validName = (v: string) => (/[\\/\0]/.test(v) || v === '.' || v === '..' ? 'Use a name without slashes.' : null);

	async function list(dir = cwd) {
		try {
			entries = (await api<{ entries: FileEntry[] }>('GET', `${base(botId)}?path=${q(dir || '.')}`)).entries;
			cwd = dir;
			filter = '';
			listError = '';
		} catch (e) {
			listError = err(e, 'This folder could not be listed.');
		}
	}
	onMount(() => {
		list('');
		const off = registerDirty({
			get label() {
				return open ?? 'The open file';
			},
			isDirty: () => dirty,
			save: () => save()
		});
		return off;
	});
	onDestroy(() => editor?.destroy());

	const crumbs = $derived(cwd ? cwd.split('/').map((name, i, a) => ({ name, path: a.slice(0, i + 1).join('/') })) : []);
	const shownEntries = $derived((entries ?? []).filter((e) => !filter.trim() || e.name.toLowerCase().includes(filter.trim().toLowerCase())));
	const language = $derived.by(() => {
		const ext = open?.split('.').pop()?.toLowerCase() ?? '';
		return ({ js: 'JavaScript', mjs: 'JavaScript', cjs: 'JavaScript', ts: 'TypeScript', py: 'Python', rs: 'Rust', go: 'Go', java: 'Java', rb: 'Ruby', json: 'JSON', md: 'Markdown', yml: 'YAML', yaml: 'YAML', toml: 'TOML', sh: 'Shell' } as Record<string, string>)[ext] ?? 'Plain text';
	});

	async function openFile(path: string) {
		if (path === open) return;
		if (dirty) {
			const choice = await chooseDialog({
				title: 'Unsaved changes',
				body: `${open} has changes that are not saved yet.`,
				cancelLabel: 'Keep editing',
				choices: [
					{ value: 'discard', label: 'Discard changes', tone: 'danger' },
					{ value: 'save', label: 'Save and open', tone: 'primary' }
				]
			});
			if (choice === null) return;
			if (choice === 'save' && !(await save())) return;
		}
		notEditable = saveError = '';
		editor?.destroy();
		editor = null;
		dirty = false;
		open = path;
		loadingFile = true;
		let text: string;
		try {
			const r = await fetchRevision(`${base(botId)}/content?path=${q(path)}`);
			text = r.text;
			etag = r.etag;
		} catch (e) {
			loadingFile = false;
			notEditable = e instanceof ApiError && e.status === 413 ? 'This file is too large for the editor. Download it to work on it locally.' : err(e, 'This file could not be opened.');
			return;
		}
		if (text.includes('\u0000')) {
			loadingFile = false;
			notEditable = 'This looks like a binary file, so it can only be downloaded.';
			return;
		}
		saved = text;
		const { createEditor } = await import('$lib/editor/editor');
		loadingFile = false;
		await Promise.resolve(); // let the editor container render
		if (open !== path || !host) return;
		editor = await createEditor(host, text, path, {
			onChange: () => {
				const t = editor?.getText() ?? '';
				dirty = t.length !== saved.length || t !== saved;
			},
			onSave: () => void save()
		});
	}

	function closeFile() {
		editor?.destroy();
		editor = null;
		open = null;
		dirty = false;
		saveError = notEditable = '';
	}

	/** Saves with the loaded revision; resolves true on success. The text stays in the editor on any failure. */
	async function save(force = false): Promise<boolean> {
		if (!open || !editor || saving) return false;
		const path = open;
		const text = editor.getText();
		saving = true;
		saveError = '';
		try {
			etag = await putText(`${base(botId)}/content?path=${q(path)}`, text, { ifMatch: force ? null : etag });
			saved = text;
			dirty = editor?.getText() !== saved;
			toast(running ? `Saved ${path}. Restart the bot to use it.` : `Saved ${path}`);
			return true;
		} catch (e) {
			if (e instanceof ApiError && e.status === 412) return await conflict(path, text);
			saveError = `Not saved: ${err(e, 'the connection failed')}. Your changes are still here.`;
			return false;
		} finally {
			saving = false;
		}
	}

	async function conflict(path: string, mine: string): Promise<boolean> {
		const choice = await chooseDialog({
			title: 'This file changed',
			body: `${path} was changed after you opened it, by SFTP, a deployment or another editor. Your version is still in the editor.`,
			cancelLabel: 'Keep editing',
			choices: [
				{ value: 'download', label: 'Download my version' },
				{ value: 'reload', label: 'Load their version' },
				{ value: 'overwrite', label: 'Replace with mine', tone: 'danger' }
			]
		});
		if (choice === 'download') {
			downloadText(path.split('/').pop() ?? 'file.txt', mine);
			return false;
		}
		if (choice === 'reload') {
			const ok = await confirmDialog({ title: 'Discard your changes?', body: 'Your edits are replaced with the current file.', confirmLabel: 'Discard and reload', tone: 'danger' });
			if (!ok) return false;
			const r = await fetchRevision(`${base(botId)}/content?path=${q(path)}`);
			etag = r.etag;
			saved = r.text;
			editor?.setText(r.text);
			dirty = false;
			return false;
		}
		if (choice === 'overwrite') return await save(true);
		return false;
	}

	function downloadText(name: string, text: string) {
		const a = document.createElement('a');
		a.href = URL.createObjectURL(new Blob([text], { type: 'text/plain' }));
		a.download = name;
		a.click();
		setTimeout(() => URL.revokeObjectURL(a.href), 1000);
	}

	async function newFile() {
		const name = await promptDialog({ title: 'New file', label: 'File name', placeholder: 'index.js', confirmLabel: 'Create file', mono: true, validate: validName });
		if (!name) return;
		const p = join(cwd, name);
		try {
			await putText(`${base(botId)}/content?path=${q(p)}`, '', { createOnly: true });
			await list();
			await openFile(p);
		} catch (e) {
			toast(e instanceof ApiError && e.status === 412 ? `${name} already exists.` : err(e, 'The file could not be created.'), 'fail');
		}
	}
	async function newFolder() {
		const name = await promptDialog({ title: 'New folder', label: 'Folder name', confirmLabel: 'Create folder', mono: true, validate: validName });
		if (!name) return;
		try {
			await api('POST', `${base(botId)}/mkdir`, { path: join(cwd, name) });
			await list();
		} catch (e) {
			toast(err(e, 'The folder could not be created.'), 'fail');
		}
	}
	async function rename(en: FileEntry) {
		const name = await promptDialog({ title: `Rename ${en.name}`, label: 'New name', value: en.name, confirmLabel: 'Rename', mono: true, validate: validName });
		if (!name || name === en.name) return;
		const from = join(cwd, en.name);
		try {
			await api('POST', `${base(botId)}/move`, { from, to: join(cwd, name) });
			if (open === from) open = join(cwd, name);
			await list();
			toast(`Renamed to ${name}`);
		} catch (e) {
			toast(err(e, 'It could not be renamed.'), 'fail');
		}
	}
	async function remove(en: FileEntry) {
		const ok = await confirmDialog({
			title: `Delete ${en.name}?`,
			body: en.type === 'dir' ? 'The folder and everything in it are deleted permanently.' : 'The file is deleted permanently.',
			confirmLabel: 'Delete',
			tone: 'danger'
		});
		if (!ok) return;
		const p = join(cwd, en.name);
		try {
			await api('DELETE', `${base(botId)}?path=${q(p)}`);
			if (open === p || open?.startsWith(p + '/')) closeFile();
			await list();
			toast(`Deleted ${en.name}`);
		} catch (e) {
			toast(err(e, 'It could not be deleted.'), 'fail');
		}
	}

	async function uploadFiles(ev: Event) {
		const input = ev.currentTarget as HTMLInputElement;
		const files = [...(input.files ?? [])];
		input.value = '';
		const dir = cwd;
		for (const f of files) {
			const u: Upload = { name: f.name, progress: 0, ctrl: new AbortController() };
			uploads.push(u);
			const item = uploads[uploads.length - 1];
			try {
				await uploadProgress(`${base(botId)}/content?path=${q(join(dir, f.name))}`, f, (p) => (item.progress = p), u.ctrl.signal);
				item.progress = 1;
			} catch (e) {
				item.error = err(e, 'upload failed');
			}
		}
		const failed = uploads.filter((x) => x.error && x.error !== 'Upload cancelled.').length;
		if (!failed) uploads = [];
		toast(failed ? `${failed} of ${files.length} uploads failed` : `Uploaded ${files.length} file${files.length === 1 ? '' : 's'}`, failed ? 'fail' : 'success');
		await list(dir);
	}

	async function uploadZip(ev: Event) {
		const input = ev.currentTarget as HTMLInputElement;
		const f = input.files?.[0];
		input.value = '';
		if (!f) return;
		const u: Upload = { name: `${f.name} (extracting)`, progress: 0, ctrl: new AbortController() };
		uploads.push(u);
		const item = uploads[uploads.length - 1];
		try {
			const r = await uploadProgress(`${base(botId)}/extract?path=${q(cwd || '.')}`, f, (p) => (item.progress = p), u.ctrl.signal, 'application/zip');
			const j = (await r.json()) as { extracted: number };
			uploads = uploads.filter((x) => x !== item);
			toast(`Extracted ${j.extracted} files from ${f.name}`);
			await list();
		} catch (e) {
			item.error = err(e, 'the archive could not be extracted');
		}
	}
	const icon = (e: FileEntry) => (e.type === 'dir' ? 'folder' : e.type === 'symlink' ? 'link' : 'file');
</script>

<!-- Explorer and editor side by side from lg up; one at a time on narrow screens. -->
<div class="grid gap-4 lg:grid-cols-[18rem_minmax(0,1fr)]">
	<div class="min-w-0 {open ? 'hidden lg:block' : ''}">
		<div class="flex flex-wrap items-center gap-1.5">
			<button class="btn btn-sm" onclick={newFile}><Icon name="plus" size={14} />File</button>
			<button class="btn btn-sm" onclick={newFolder}><Icon name="folder" size={14} />Folder</button>
			<label class="btn btn-sm cursor-pointer focus-within:outline-2 focus-within:outline-action"><Icon name="upload" size={14} />Upload<input type="file" multiple class="sr-only" onchange={uploadFiles} /></label>
			<label class="btn btn-sm cursor-pointer focus-within:outline-2 focus-within:outline-action" title="Extracts into the current folder"><Icon name="archive" size={14} />Zip<input type="file" accept=".zip,application/zip" class="sr-only" onchange={uploadZip} /></label>
		</div>

		<nav class="mt-3 flex min-w-0 flex-wrap items-center gap-0.5 text-small" aria-label="Folder">
			<button class="rounded-control px-1 py-0.5 font-mono hover:bg-paper-2 {cwd ? 'text-action' : 'font-semibold'}" onclick={() => list('')} aria-current={cwd ? undefined : 'location'}>workspace</button>
			{#each crumbs as c, i (c.path)}
				<Icon name="chevronRight" size={12} class="text-muted" />
				<button class="max-w-40 truncate rounded-control px-1 py-0.5 font-mono hover:bg-paper-2 {i === crumbs.length - 1 ? 'font-semibold' : 'text-action'}" onclick={() => list(c.path)} aria-current={i === crumbs.length - 1 ? 'location' : undefined}>{c.name}</button>
			{/each}
		</nav>
		{#if (entries?.length ?? 0) > 8}
			<label class="relative mt-2 block">
				<span class="sr-only">Filter this folder</span>
				<Icon name="search" size={14} class="pointer-events-none absolute top-1/2 left-2.5 -translate-y-1/2 text-muted" />
				<input class="field pl-8 text-small" type="search" placeholder="Filter this folder" bind:value={filter} />
			</label>
		{/if}
		{#if listError}<Notice tone="fail" class="mt-2">{listError}</Notice>{/if}

		{#if uploads.length}
			<ul class="mt-2 space-y-1.5 border border-rule-soft bg-panel p-2 text-small" aria-label="Uploads">
				{#each uploads as u, i (i)}
					<li>
						<div class="flex items-center gap-2">
							<span class="min-w-0 flex-1 truncate">{u.name}</span>
							{#if u.error}<span class="text-fail">{u.error}</span>{:else if u.progress < 1}<button class="text-action underline" onclick={() => u.ctrl.abort()}>Cancel</button>{:else}<Icon name="check" size={13} class="text-run" />{/if}
						</div>
						{#if !u.error}<div class="mt-1 h-1 bg-paper-2"><div class="h-1 bg-action" style="width: {Math.round(u.progress * 100)}%"></div></div>{/if}
					</li>
				{/each}
				{#if uploads.every((u) => u.error || u.progress >= 1)}<li><button class="text-action underline" onclick={() => (uploads = [])}>Clear</button></li>{/if}
			</ul>
		{/if}

		<ul class="mt-2 max-h-[calc(100dvh-20rem)] min-h-40 overflow-auto border-y border-rule-soft">
			{#if cwd}
				<li><button class="flex w-full items-center gap-2 px-2 py-1.5 text-left text-muted hover:bg-paper-2" onclick={() => list(cwd.includes('/') ? cwd.slice(0, cwd.lastIndexOf('/')) : '')}><Icon name="chevronLeft" size={14} />Up one folder</button></li>
			{/if}
			{#if entries === null && !listError}
				<li class="px-2 py-3 text-muted">Loading…</li>
			{:else}
				{#each shownEntries as en (en.name)}
					{@const p = join(cwd, en.name)}
					<li class="flex items-center {open === p ? 'bg-panel shadow-[inset_3px_0_0_var(--color-action)]' : 'hover:bg-paper-2/60'}">
						{#if en.type === 'dir'}
							<button class="flex min-w-0 flex-1 items-center gap-2 px-2 py-1.5 text-left font-medium" onclick={() => list(p)}><Icon name="folder" size={15} class="text-muted" /><span class="truncate">{en.name}</span></button>
						{:else if en.type === 'symlink'}
							<span class="flex min-w-0 flex-1 items-center gap-2 px-2 py-1.5 text-muted" title="Links are not followed"><Icon name="link" size={15} /><span class="truncate">{en.name}</span></span>
						{:else}
							<button class="flex min-w-0 flex-1 items-center gap-2 px-2 py-1.5 text-left" onclick={() => openFile(p)} aria-current={open === p ? 'true' : undefined}><Icon name={icon(en)} size={15} class="text-muted" /><span class="truncate">{en.name}</span></button>
							<span class="shrink-0 px-1 text-small text-muted tabular-nums">{fmtBytes(en.size)}</span>
						{/if}
						<Menu
							label="Actions for {en.name}"
							items={[
								...(en.type === 'file' ? [{ label: 'Download', onselect: () => window.open(`/api/v1${base(botId)}/content?path=${q(p)}&download=1`, '_self') }] : []),
								{ label: 'Rename', onselect: () => rename(en) },
								'separator',
								{ label: 'Delete', danger: true, onselect: () => remove(en) }
							]}
						/>
					</li>
				{:else}
					<li class="px-2 py-5 text-muted">
						{#if filter}No names match “{filter}”.{:else}This folder is empty. Upload your code, create a file, or deploy a repository under Deployments.{/if}
					</li>
				{/each}
			{/if}
		</ul>
	</div>

	<div class="min-w-0 {open ? '' : 'hidden lg:block'}">
		{#if open}
			<div class="flex flex-wrap items-center gap-2 pb-2">
				<button class="btn btn-sm btn-quiet lg:hidden" onclick={closeFile}><Icon name="chevronLeft" size={14} />Files</button>
				<div class="min-w-0 flex-1">
					<p class="truncate font-mono text-small font-medium" title={open}>{open}</p>
					<p class="text-small text-muted" aria-live="polite">{language}{#if !notEditable}, {saving ? 'saving…' : dirty ? 'unsaved changes' : 'saved'}{/if}</p>
				</div>
				<a class="btn btn-sm" href="/api/v1{base(botId)}/content?path={q(open)}&download=1" download><Icon name="download" size={14} />Download</a>
				{#if !notEditable}<button class="btn btn-sm btn-primary" onclick={() => save()} disabled={!dirty || saving} title="Save (Ctrl+S)">Save</button>{/if}
			</div>
			{#if saveError}<Notice tone="fail" class="mb-2" live>{saveError}{#snippet action()}<button class="btn btn-sm" onclick={() => save()}>Try again</button>{/snippet}</Notice>{/if}
			{#if notEditable}
				<div class="border border-dashed border-rule p-6 text-muted">{notEditable}</div>
			{:else}
				{#if loadingFile}<p class="py-2 text-muted">Opening…</p>{/if}
				<div bind:this={host} class="h-[max(22rem,calc(100dvh-22rem))] overflow-hidden border border-rule-soft"></div>
				{#if running && !dirty}<p class="mt-2 text-small text-muted">Saved changes apply after the bot restarts.</p>{/if}
			{/if}
		{:else}
			<div class="grid h-full min-h-60 place-items-center border border-dashed border-rule p-6 text-center text-muted">
				<p>Choose a file to edit it here.<br />Press Ctrl+S to save; changes apply the next time the bot starts.</p>
			</div>
		{/if}
	</div>
</div>
