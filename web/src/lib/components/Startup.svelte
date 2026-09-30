<script lang="ts">
	import { api, ApiError } from '$lib/api/client';
	import { joinArgs, splitArgs } from '$lib/args';
	import type { Bot, RuntimeInfo } from '$lib/api/types';
	import { onMount } from 'svelte';
	import { registerDirty } from '$lib/ui/guard.svelte';
	import { toast } from '$lib/ui/toast.svelte';
	import Notice from '$lib/components/ui/Notice.svelte';

	let { bot, stopped, onSaved }: { bot: Bot; stopped: boolean; onSaved: (b: Bot) => void } = $props();

	let runtimes = $state<RuntimeInfo[]>([]);
	let runtime = $state(bot.runtime);
	let command = $state(joinArgs(bot.argv));
	let entrypoint = $state(joinArgs(bot.entrypoint));
	let policy = $state(bot.restart_policy);
	let maxAttempts = $state(bot.restart_max_attempts);
	let backoffInit = $state(bot.restart_backoff_initial_ms / 1000);
	let backoffMax = $state(bot.restart_backoff_max_ms / 1000);
	let error = $state('');
	const snapshot = () => JSON.stringify([runtime, command, entrypoint, policy, maxAttempts, backoffInit, backoffMax]);
	let base = $state(snapshot());
	const dirty = $derived(snapshot() !== base);

	onMount(() => {
		api<{ runtimes: RuntimeInfo[] }>('GET', '/runtimes').then((r) => (runtimes = r.runtimes));
		return registerDirty({ label: 'Startup settings', isDirty: () => dirty, save: async () => await save() });
	});
	const chosen = $derived(runtimes.find((r) => r.id === runtime));
	const changedRuntime = $derived(runtime !== bot.runtime);

	function useDefault() {
		if (chosen) {
			command = joinArgs(chosen.default_argv);
			entrypoint = '';
		}
	}

	async function save(e?: SubmitEvent): Promise<boolean> {
		e?.preventDefault();
		error = '';
		const body: Record<string, unknown> = {
			entrypoint: splitArgs(entrypoint),
			restart_policy: policy,
			restart_max_attempts: maxAttempts,
			restart_backoff_initial_ms: Math.round(backoffInit * 1000),
			restart_backoff_max_ms: Math.round(backoffMax * 1000)
		};
		const argv = splitArgs(command);
		// Switching runtime without typing a command adopts the new runtime's default.
		if (!(changedRuntime && command === joinArgs(bot.argv))) body.argv = argv;
		if (changedRuntime) body.runtime = runtime;
		try {
			onSaved(await api<Bot>('PATCH', `/bots/${bot.id}`, body));
			base = snapshot();
			toast('Startup settings saved');
			return true;
		} catch (err) {
			error = err instanceof ApiError ? err.message : 'The startup settings could not be saved.';
			return false;
		}
	}
</script>

<form class="grid max-w-2xl gap-6" onsubmit={save}>
	<label class="block">
		<span class="label">Runtime</span>
		<select class="field" bind:value={runtime} disabled={!stopped}>
			{#each runtimes as r (r.id)}<option value={r.id}>{r.display_name}</option>{/each}
		</select>
		{#if changedRuntime}<span class="help text-warn!">Changing the runtime switches the image and resets the start command to its default unless you edit it.</span>{/if}
	</label>

	<label class="block">
		<span class="label">Start command</span>
		<input class="field font-mono text-[13px]" bind:value={command} disabled={!stopped} spellcheck="false" placeholder="node index.js" />
		<span class="help">Program and arguments, for example <code class="font-mono">node index.js</code>, <code class="font-mono">python bot.py</code> or <code class="font-mono">./app</code>. Quote arguments containing spaces. It runs directly, not through a shell, and only programs approved for the runtime are accepted.</span>
		{#if chosen}<button type="button" class="btn btn-quiet mt-1 px-0 text-action" onclick={useDefault} disabled={!stopped}>Use the default: {joinArgs(chosen.default_argv)}</button>{/if}
	</label>

	<label class="block">
		<span class="label">Custom entrypoint <span class="font-normal text-muted">(optional)</span></span>
		<input class="field font-mono text-[13px]" bind:value={entrypoint} disabled={!stopped} spellcheck="false" placeholder="node --enable-source-maps" />
		<span class="help">Overrides the container entrypoint; the start command above then becomes its arguments. Leave empty to run the command directly.</span>
	</label>

	<fieldset class="grid gap-4 border border-rule-soft bg-panel p-4 sm:grid-cols-2" disabled={!stopped}>
		<legend class="px-1 font-medium">Auto-restart</legend>
		<label class="block sm:col-span-2">
			<span class="mb-1 block">When the bot exits</span>
			<select class="field" bind:value={policy}>
				<option value="on_failure">Restart it after a crash (non-zero exit)</option>
				<option value="never">Never restart it automatically</option>
			</select>
			<span class="help">A clean exit (code 0) is never restarted. Pressing Start again always retries.</span>
		</label>
		<label class="block"><span class="mb-1 block">Give up after (crashes in a row)</span><input class="field" type="number" step="1" min="0" max="100" bind:value={maxAttempts} /><span class="help">0 keeps trying forever.</span></label>
		<div class="grid grid-cols-2 gap-3">
			<label class="block"><span class="mb-1 block">First delay (s)</span><input class="field" type="number" min="0.1" step="any" bind:value={backoffInit} /></label>
			<label class="block"><span class="mb-1 block">Longest delay (s)</span><input class="field" type="number" min="0.1" step="any" bind:value={backoffMax} /></label>
		</div>
		<p class="text-muted sm:col-span-2">The delay doubles after every crash, up to the longest delay, and starts over after the bot runs stably for a minute.</p>
	</fieldset>

	{#if error}<Notice tone="fail" live>{error}</Notice>{/if}
	<div class="sticky bottom-0 flex items-center gap-3 border-t border-rule-soft bg-paper/95 py-3 backdrop-blur-sm">
		<button class="btn btn-primary" disabled={!stopped || !dirty}>Save startup settings</button>
		{#if dirty}<span class="text-small text-warn">Unsaved changes</span>{/if}
	</div>
</form>
