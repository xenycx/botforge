<script lang="ts">
	import { onMount } from 'svelte';
	import { api, ApiError } from '$lib/api/client';
	import { fmtAgo } from '$lib/args';
	import type { Bot } from '$lib/api/types';
	import { toast } from '$lib/ui/toast.svelte';
	import Icon from '$lib/components/ui/Icon.svelte';
	import Notice from '$lib/components/ui/Notice.svelte';
	import Skeleton from '$lib/components/ui/Skeleton.svelte';

	let { bot, admin }: { bot: Bot; admin: boolean } = $props();

	type Prefs = { crash: boolean; deploy: boolean; backup: boolean; recovery: boolean; heartbeat_after_s: number };
	type Health = { state: 'unknown' | 'ok' | 'stale' | 'not_ready'; last_seen_at_ms: number | null; ready: boolean | null; alerts: Prefs; webhook: boolean };
	type Probe = { kind: ''|'tcp'|'http'; host_port: number; path: string; interval_s: number; timeout_ms: number; failure_threshold: number; success_threshold: number; startup_grace_s: number; restart_unhealthy: boolean; status: 'disabled'|'unknown'|'starting'|'healthy'|'unhealthy'; consecutive_failures: number; consecutive_successes: number; last_checked_at_ms: number|null; last_error: string|null };
	let health = $state<Health | null>(null);
	let prefs = $state<Prefs | null>(null);
	let probe = $state<Probe | null>(null);
	let probeDraft = $state<Probe | null>(null);
	let error = $state('');
	let saving = $state(false);
	let now = $state(Date.now());
	const msg = (e: unknown) => (e instanceof ApiError ? e.message.charAt(0).toUpperCase() + e.message.slice(1) : 'The request failed.');
	const path = $derived(`/bots/${bot.id}`);

	async function load(first = false) {
		try {
			const [h,p] = await Promise.all([api<Health>('GET', `${path}/health`), api<Probe>('GET', `${path}/health-probe`)]);
			health = h; probe = p;
			if (first) { prefs = { ...health.alerts }; probeDraft = { ...p }; }
			error = '';
		} catch (e) {
			error = msg(e);
		}
	}
	onMount(() => {
		load(true);
		const t = setInterval(() => {
			now = Date.now();
			if (document.visibilityState === 'visible') load();
		}, 10000);
		return () => clearInterval(t);
	});

	const dirty = $derived(!!prefs && !!health && JSON.stringify(prefs) !== JSON.stringify(health.alerts));
	const probeDirty = $derived(!!probe && !!probeDraft && JSON.stringify({ ...probeDraft, status:probe.status, consecutive_failures:probe.consecutive_failures, consecutive_successes:probe.consecutive_successes, last_checked_at_ms:probe.last_checked_at_ms, last_error:probe.last_error }) !== JSON.stringify(probe));
	async function save() {
		if (!prefs) return;
		saving = true;
		try {
			const p = await api<Prefs>('PUT', `${path}/alerts`, prefs);
			if (health) health.alerts = p;
			prefs = { ...p };
			toast('Alert settings saved', 'success');
		} catch (e) {
			toast(msg(e), 'fail');
		} finally {
			saving = false;
		}
	}
	async function test() {
		try {
			await api('POST', `${path}/alerts/test`);
			toast('Test message sent to Discord', 'success');
		} catch (e) {
			toast(msg(e), 'fail');
		}
	}
	async function saveProbe() {
		if (!probeDraft) return; saving=true;
		try { probe = await api<Probe>('PUT', `${path}/health-probe`, probeDraft); probeDraft={...probe}; toast(probe.kind ? 'Health probe saved' : 'Health probe disabled','success'); }
		catch(e){toast(msg(e),'fail')} finally {saving=false}
	}

	const stateText = {
		unknown: ['Unknown', 'The bot has not reported yet. Add the BotForge SDK (Analytics section) to see whether it is connected to Discord, not just whether its process runs.'],
		ok: ['Reporting', 'The bot is pushing heartbeats.'],
		stale: ['Not reporting', 'The container runs, but the bot stopped pushing. It may be stuck or disconnected from Discord.'],
		not_ready: ['Not ready', 'The bot reports that it is not connected to Discord.']
	} as const;
	const tone = $derived(health ? ({ unknown: 'idle', ok: 'run', stale: 'fail', not_ready: 'warn' } as const)[health.state] : 'idle');
	const kinds: { key: 'crash' | 'deploy' | 'backup' | 'recovery'; label: string; hint: string }[] = [
		{ key: 'crash', label: 'Crashes', hint: 'When the bot fails and needs attention (at most one per 10 minutes)' },
		{ key: 'deploy', label: 'Deployments', hint: 'When a GitHub deployment succeeds or fails' },
		{ key: 'backup', label: 'Backup failures', hint: 'When a backup of this bot fails' },
		{ key: 'recovery', label: 'Recoveries', hint: 'When heartbeats resume after an alert' }
	];
	const thresholds = [0, 60, 120, 300, 600, 1800, 3600];
	const thrLabel = (s: number) => (s === 0 ? 'Off' : s < 3600 ? `${s / 60} minute${s === 60 ? '' : 's'}` : '1 hour');
	const tcpPorts = $derived(bot.ports.filter((p)=>p.protocol==='tcp'));
	const probeTone = $derived(probe?.status==='healthy'?'run':probe?.status==='unhealthy'?'fail':probe?.status==='starting'?'warn':'idle');
</script>

{#if error}<Notice tone="fail">{error}</Notice>{/if}
{#if !health && !error}
	<Skeleton rows={3} label="Loading health" />
{:else if health && prefs}
	<section aria-label="Application health" class="spine border-y border-rule-soft bg-panel py-3 pr-4 pl-5" data-tone={tone}>
		<p class="eyebrow">Application health</p>
		<p class="mt-1 text-title font-semibold">{stateText[health.state][0]}{#if health.last_seen_at_ms}<span class="ml-2 text-small font-normal text-muted">last push {fmtAgo(health.last_seen_at_ms, now)}</span>{/if}</p>
		<p class="mt-0.5 max-w-prose text-muted">{stateText[health.state][1]}</p>
		{#if bot.restart_count}<p class="mt-1 text-small text-warn">{bot.restart_count} crash{bot.restart_count === 1 ? '' : 'es'} in a row in the current run.</p>{/if}
	</section>

	<section aria-labelledby="probe-h" class="mt-8 max-w-2xl">
		<div class="spine border-y border-rule-soft bg-panel py-3 pr-4 pl-5" data-tone={probeTone}>
			<p class="eyebrow">TCP / HTTP probe</p>
			<p class="mt-1 text-title font-semibold capitalize">{probe?.status ?? 'Disabled'}{#if probe?.last_checked_at_ms}<span class="ml-2 text-small font-normal text-muted">checked {fmtAgo(probe.last_checked_at_ms,now)}</span>{/if}</p>
			<p class="mt-0.5 text-muted">Checks a published TCP port from the host. Failed checks can restart a stuck process even when it has not exited.</p>
			{#if probe?.last_error}<p class="mt-1 text-small text-fail">{probe.last_error}</p>{/if}
		</div>
		{#if probeDraft}
			<div class="mt-4 grid gap-4 sm:grid-cols-2">
				<label><span class="label">Probe type</span><select class="field" bind:value={probeDraft.kind} disabled={!admin}><option value="">Disabled</option><option value="tcp">TCP connection</option><option value="http">HTTP GET</option></select></label>
				<label><span class="label">Published port</span><select class="field" bind:value={probeDraft.host_port} disabled={!admin||!probeDraft.kind}><option value={0}>Choose a TCP port</option>{#each tcpPorts as p}<option value={p.host_port}>{p.host_port} → container {p.container_port}</option>{/each}</select></label>
				{#if probeDraft.kind==='http'}<label class="sm:col-span-2"><span class="label">HTTP path</span><input class="field" maxlength="256" placeholder="/healthz" bind:value={probeDraft.path} disabled={!admin} /></label>{/if}
				<label><span class="label">Check every</span><select class="field" bind:value={probeDraft.interval_s} disabled={!admin||!probeDraft.kind}>{#each [5,10,15,30,60,120,300] as s}<option value={s}>{s} seconds</option>{/each}</select></label>
				<label><span class="label">Timeout</span><select class="field" bind:value={probeDraft.timeout_ms} disabled={!admin||!probeDraft.kind}>{#each [500,1000,2000,5000,10000] as ms}<option value={ms}>{ms/1000} seconds</option>{/each}</select></label>
				<label><span class="label">Failures before unhealthy</span><input class="field" type="number" min="1" max="10" bind:value={probeDraft.failure_threshold} disabled={!admin||!probeDraft.kind} /></label>
				<label><span class="label">Startup grace</span><select class="field" bind:value={probeDraft.startup_grace_s} disabled={!admin||!probeDraft.kind}>{#each [0,10,30,60,120,300,600] as s}<option value={s}>{s} seconds</option>{/each}</select></label>
			</div>
			<label class="mt-4 flex items-start gap-2.5"><input type="checkbox" class="mt-0.5" bind:checked={probeDraft.restart_unhealthy} disabled={!admin||!probeDraft.kind} /><span>Restart when unhealthy<span class="help mt-0">Restarts once when the failure threshold is crossed. Startup grace prevents deploy loops.</span></span></label>
			{#if !tcpPorts.length}<Notice tone="warn" class="mt-3">Publish a TCP port in Network before enabling a probe.</Notice>{/if}
			{#if admin}<div class="mt-4 flex gap-2"><button class="btn btn-primary" disabled={!probeDirty||saving} onclick={saveProbe}>Save health probe</button>{#if probeDirty}<button class="btn btn-quiet" onclick={()=>probe&&(probeDraft={...probe})}>Discard</button>{/if}</div>{/if}
		{/if}
	</section>

	<section aria-labelledby="al-h" class="mt-8 max-w-2xl">
		<div class="flex flex-wrap items-end justify-between gap-2">
			<div>
				<h3 id="al-h" class="text-title font-semibold">Discord notifications</h3>
				<p class="mt-0.5 text-muted">
					{#if health.webhook}Sent to the owner's Discord channel.{:else}The owner has not turned on Discord notifications yet (Settings → Connected accounts → Discord). Preferences are kept for when they do.{/if}
				</p>
			</div>
			{#if admin && health.webhook}<button class="btn btn-sm" onclick={test}><Icon name="bolt" size={14} />Send a test</button>{/if}
		</div>
		<fieldset class="mt-3 grid gap-1.5 sm:grid-cols-2" disabled={!admin}>
			{#each kinds as k (k.key)}
				<label class="flex items-start gap-2.5 rounded-control border border-rule-soft bg-panel px-3 py-2">
					<input type="checkbox" class="mt-0.5" bind:checked={prefs[k.key]} />
					<span>{k.label}<span class="help mt-0">{k.hint}</span></span>
				</label>
			{/each}
		</fieldset>
		<label class="mt-4 block max-w-sm">
			<span class="label">Alert when the bot stops reporting for</span>
			<select class="field" bind:value={prefs.heartbeat_after_s} disabled={!admin}>
				{#each thresholds as s (s)}<option value={s}>{thrLabel(s)}</option>{/each}
			</select>
			<span class="help">Needs the SDK. Bots that never reported are treated as unknown and never alert.</span>
		</label>
		{#if admin}
			<div class="mt-4 flex gap-2">
				<button class="btn btn-primary" disabled={!dirty || saving} onclick={save}>Save alert settings</button>
				{#if dirty}<button class="btn btn-quiet" onclick={() => health && (prefs = { ...health.alerts })}>Discard</button>{/if}
			</div>
		{:else}
			<p class="mt-3 text-small text-muted">Only someone with full control of this bot can change these.</p>
		{/if}
	</section>
{/if}
