<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { api, ApiError, fmtBytes, MiB } from '$lib/api/client';
	import type { Bot, Limits, RuntimeInfo } from '$lib/api/types';
	import { confirmDialog } from '$lib/ui/dialogs.svelte';
	import { registerDirty } from '$lib/ui/guard.svelte';
	import { toast } from '$lib/ui/toast.svelte';
	import Notice from '$lib/components/ui/Notice.svelte';

	let { bot, stopped, onSaved }: { bot: Bot; stopped: boolean; onSaved: (b: Bot) => void } = $props();

	// Form state starts from the bot once; saving replaces the bot and resets it.
	const initial = () => ({ name: bot.name, memoryMiB: Math.round(bot.memory_bytes / MiB), cpus: bot.nano_cpus / 1e9, pids: bot.pids_limit });
	let form = $state(initial());
	let base = $state(initial());
	let limits = $state<Limits | null>(null);
	let rt = $state<RuntimeInfo | null>(null);
	let error = $state('');
	let saving = $state(false);
	// svelte-ignore state_referenced_locally
	let tagText = $state(bot.tags.join(', '));
	let tagError = $state('');

	async function saveTags(e: SubmitEvent) {
		e.preventDefault();
		tagError = '';
		try {
			const r = await api<{ tags: string[] }>('PUT', `/bots/${bot.id}/tags`, { tags: tagText.split(/[,\s]+/).filter(Boolean) });
			tagText = r.tags.join(', ');
			onSaved({ ...bot, tags: r.tags });
			toast(r.tags.length ? 'Tags saved' : 'Tags removed');
		} catch (err) {
			tagError = err instanceof ApiError ? err.message : 'The tags could not be saved.';
		}
	}

	onMount(() => {
		api<{ runtimes: RuntimeInfo[]; limits: Limits }>('GET', '/runtimes')
			.then((r) => {
				limits = r.limits;
				rt = r.runtimes.find((x) => x.id === bot.runtime) ?? null;
			})
			.catch(() => {});
		return registerDirty({ label: 'Bot settings', isDirty: () => dirty, save: async () => await save() });
	});

	const dirty = $derived(JSON.stringify(form) !== JSON.stringify(base));
	const minMem = $derived(Math.max(limits?.min_memory_bytes ?? 0, rt?.min_memory_bytes ?? 0));

	async function save(e?: SubmitEvent): Promise<boolean> {
		e?.preventDefault();
		error = '';
		saving = true;
		try {
			const b = await api<Bot>('PATCH', `/bots/${bot.id}`, {
				name: form.name,
				memory_bytes: Math.round(form.memoryMiB * MiB),
				nano_cpus: Math.round(form.cpus * 1e9),
				pids_limit: form.pids
			});
			onSaved(b);
			base = { ...form };
			toast('Settings saved');
			return true;
		} catch (err) {
			error = err instanceof ApiError ? err.message : 'The settings could not be saved.';
			return false;
		} finally {
			saving = false;
		}
	}

	async function remove() {
		const ok = await confirmDialog({
			title: `Delete ${bot.name}?`,
			body: 'The container, every file in the workspace, its environment variables, backups and history are removed permanently. This cannot be undone.',
			confirmLabel: 'Delete bot permanently',
			tone: 'danger',
			typeToConfirm: bot.name
		});
		if (!ok) return;
		try {
			await api('DELETE', `/bots/${bot.id}`);
			toast(`Deleted ${bot.name}`);
			await goto('/dashboard');
		} catch (err) {
			toast(err instanceof ApiError ? err.message : 'The bot could not be deleted.', 'fail');
		}
	}
</script>

<form class="max-w-2xl" onsubmit={save}>
	<fieldset class="grid gap-5" disabled={!stopped}>
		<label class="block">
			<span class="label">Name</span>
			<input class="field" required maxlength="64" bind:value={form.name} />
		</label>
		<div class="grid gap-4 sm:grid-cols-3">
			<label class="block">
				<span class="label">Memory</span>
				<span class="flex items-center gap-2"><input class="field" type="number" step="any" min="1" bind:value={form.memoryMiB} /><span class="text-muted">MiB</span></span>
				{#if limits}<span class="help">{fmtBytes(minMem)} to {fmtBytes(limits.max_memory_bytes)}</span>{/if}
			</label>
			<label class="block">
				<span class="label">CPU</span>
				<span class="flex items-center gap-2"><input class="field" type="number" step="any" min="0.01" bind:value={form.cpus} /><span class="text-muted">cores</span></span>
				{#if limits}<span class="help">{limits.min_nano_cpus / 1e9} to {limits.max_nano_cpus / 1e9} cores</span>{/if}
			</label>
			<label class="block">
				<span class="label">Processes</span>
				<input class="field" type="number" step="1" min="1" max="4096" bind:value={form.pids} />
				<span class="help">1 to 4096 at once</span>
			</label>
		</div>
		<p class="text-small text-muted">The host enforces these limits. A bot that uses more memory than its limit is stopped and restarted according to its restart policy.{#if rt?.has_build} Builds run separately with up to {fmtBytes(Math.max(rt.build_memory_bytes, form.memoryMiB * MiB))}.{/if}</p>
	</fieldset>
	{#if error}<Notice tone="fail" class="mt-4" live>{error}</Notice>{/if}
	<div class="sticky bottom-0 mt-6 flex items-center gap-3 border-t border-rule-soft bg-paper/95 py-3 backdrop-blur-sm">
		<button class="btn btn-primary" disabled={!stopped || !dirty || saving}>Save settings</button>
		{#if dirty}<button type="button" class="btn btn-quiet" onclick={() => (form = { ...base })}>Discard changes</button><span class="text-small text-warn">Unsaved changes</span>{/if}
	</div>
</form>

<section class="mt-10 max-w-2xl" aria-labelledby="tags-h">
	<h3 id="tags-h" class="text-title font-semibold">Tags</h3>
	<p class="mt-1 text-muted">Group bots on the Bots page, for example by team, server or environment. Everyone with access sees them.</p>
	<form class="mt-3 flex flex-wrap items-start gap-2" onsubmit={saveTags}>
		<label class="block min-w-0 flex-1 basis-64">
			<span class="sr-only">Tags</span>
			<input class="field" bind:value={tagText} placeholder="music, production" aria-invalid={tagError ? 'true' : undefined} aria-describedby="tags-help" />
			<span id="tags-help" class="help {tagError ? 'text-fail!' : ''}">{tagError || 'Separate with commas. Up to 8 lowercase words.'}</span>
		</label>
		<button class="btn">Save tags</button>
	</form>
</section>

{#if !bot.shared}
	<section class="mt-12 max-w-2xl border-t border-rule-soft pt-5" aria-labelledby="danger-h">
		<h3 id="danger-h" class="text-title font-semibold">Delete this bot</h3>
		<p class="mt-1 text-muted">Removes the container, all files, environment variables, backups and history. This cannot be undone; create and download a backup first if you might need it.</p>
		<button class="btn btn-danger mt-3" onclick={remove}>Delete bot…</button>
	</section>
{/if}
