<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { api, ApiError, fmtBytes, fmtCpu } from '$lib/api/client';
	import { can, Perm, type Bot } from '$lib/api/types';
	import { power, type PowerAction } from '$lib/api/bots';
	import { describe, isStopped, mayBeLive } from '$lib/status';
	import { session } from '$lib/session.svelte';
	import Icon, { type IconName } from '$lib/components/ui/Icon.svelte';
	import Menu, { type MenuItem } from '$lib/components/ui/Menu.svelte';
	import Notice from '$lib/components/ui/Notice.svelte';
	import StatusBadge from '$lib/components/ui/StatusBadge.svelte';
	import Skeleton from '$lib/components/ui/Skeleton.svelte';
	import ResourceStrip from '$lib/components/ResourceStrip.svelte';
	import Overview from '$lib/components/Overview.svelte';
	import Console from '$lib/components/Console.svelte';
	import Files from '$lib/components/Files.svelte';
	import Env from '$lib/components/Env.svelte';
	import Startup from '$lib/components/Startup.svelte';
	import Packages from '$lib/components/Packages.svelte';
	import PageStudio from '$lib/components/PageStudio.svelte';
	import Network from '$lib/components/Network.svelte';
	import Backups from '$lib/components/Backups.svelte';
	import Deploy from '$lib/components/Deploy.svelte';
	import Users from '$lib/components/Users.svelte';
	import Settings from '$lib/components/Settings.svelte';
	import Schedules from '$lib/components/Schedules.svelte';
	import Alerts from '$lib/components/Alerts.svelte';
	import AIOperator from '$lib/components/AIOperator.svelte';

	let { id }: { id: string } = $props();

	let bot = $state<Bot | null>(null);
	let missing = $state(false);
	let loadError = $state('');
	let now = $state(Date.now());
	let acting = $state(false);

	type Section = { id: string; label: string; icon: IconName; perm: number };
	// Grouped by what the user is doing. perm -1 = owner or administrator only;
	// 0 = anyone with access. The server enforces the same rules.
	const groups: { label: string; items: Section[] }[] = [
		{
			label: 'Operate',
			items: [
				{ id: 'overview', label: 'Overview', icon: 'overview', perm: 0 },
				{ id: 'ai', label: 'AI operator', icon: 'bolt', perm: 0 },
				{ id: 'console', label: 'Console', icon: 'terminal', perm: Perm.console },
				{ id: 'page', label: 'Public page', icon: 'globe', perm: Perm.console },
				{ id: 'alerts', label: 'Health & alerts', icon: 'activity', perm: Perm.console }
			]
		},
		{
			label: 'Code',
			items: [
				{ id: 'files', label: 'Files', icon: 'file', perm: Perm.files },
				{ id: 'packages', label: 'Packages', icon: 'package', perm: Perm.files },
				{ id: 'deploy', label: 'Deployments', icon: 'rocket', perm: Perm.files }
			]
		},
		{
			label: 'Configure',
			items: [
				{ id: 'env', label: 'Environment', icon: 'key', perm: Perm.env },
				{ id: 'startup', label: 'Startup', icon: 'sliders', perm: Perm.admin },
				{ id: 'network', label: 'Network', icon: 'network', perm: Perm.admin }
			]
		},
		{
			label: 'Manage',
			items: [
				{ id: 'backups', label: 'Backups', icon: 'archive', perm: Perm.files },
				{ id: 'schedules', label: 'Schedules', icon: 'clock', perm: 0 },
				{ id: 'users', label: 'Access', icon: 'users', perm: -1 },
				{ id: 'settings', label: 'Settings', icon: 'gear', perm: Perm.admin }
			]
		}
	];
	// Sections for features this panel does not run are left out entirely.
	const available: Record<string, keyof typeof session.features> = { ai: 'ai', files: 'files', packages: 'files', backups: 'backups', schedules: 'schedules', alerts: 'health' };
	const allowed = (s: Section) =>
		!!bot && (!available[s.id] || session.features[available[s.id]]) && (s.perm === 0 || (s.perm === -1 ? !bot.shared : can(bot, s.perm)));
	const visibleGroups = $derived(groups.map((g) => ({ ...g, items: g.items.filter(allowed) })).filter((g) => g.items.length));
	const flat = $derived(visibleGroups.flatMap((g) => g.items));
	const requested = $derived(page.url.searchParams.get('tab') === 'analytics' ? 'page' : (page.url.searchParams.get('tab') ?? 'overview'));
	const tab = $derived(flat.some((s) => s.id === requested) ? requested : 'overview');
	const current = $derived(flat.find((s) => s.id === tab));
	const idx = $derived(flat.findIndex((s) => s.id === tab));

	function sectionHref(sid: string) {
		return `/bots/${id}?tab=${sid}`;
	}
	function pick(sid: string) {
		goto(sectionHref(sid), { noScroll: true, keepFocus: true });
	}

	async function refresh() {
		try {
			bot = await api<Bot>('GET', `/bots/${id}`);
			loadError = '';
		} catch (e) {
			if (e instanceof ApiError && e.status === 404) missing = true;
			else loadError = e instanceof ApiError ? e.message : 'Connection to the panel was lost. Retrying…';
		}
	}
	onMount(() => {
		refresh();
		const poll = setInterval(() => document.visibilityState === 'visible' && refresh(), 2000);
		const tick = setInterval(() => (now = Date.now()), 1000);
		return () => {
			clearInterval(poll);
			clearInterval(tick);
		};
	});

	async function act(a: PowerAction) {
		if (!bot) return;
		acting = true;
		if (await power(bot, a)) await refresh();
		acting = false;
	}

	const d = $derived(bot ? describe(bot, now) : null);
	const stopped = $derived(bot ? isStopped(bot) : false);
	const isOwner = $derived(bot ? !bot.shared : false);
	const admin = $derived(bot ? can(bot, Perm.admin) : false);
	// Without a runner, lifecycle requests can only fail: offer none.
	const canPower = $derived(bot ? can(bot, Perm.power) && session.features.runner : false);
	const running = $derived(bot?.desired_state === 'running');
	// A bot that settled after exiting (gave up, exited cleanly) is wanted
	// running but idle: the useful action is to start it again, not Stop.
	const settledIdle = $derived(!!bot && running && (bot.phase === 'failed' || bot.phase === 'exited'));
	const configSection = $derived(['env', 'startup', 'network', 'settings'].includes(tab));

	const powerMenu = $derived.by((): MenuItem[] => {
		if (!bot) return [];
		const items: MenuItem[] = [];
		if (settledIdle) items.push({ label: 'Stop', onselect: () => act('stop'), hint: 'Stop trying and mark it stopped' });
		else if (running) items.push({ label: 'Restart', onselect: () => act('restart') }, { label: 'Stop', onselect: () => act('stop') });
		else items.push({ label: 'Start', onselect: () => act('start') });
		items.push('separator', { label: 'Kill', danger: true, disabled: !mayBeLive(bot), hint: 'Ends the process immediately', onselect: () => act('kill') });
		return items;
	});
</script>

<svelte:head><title>{bot?.name ?? 'Bot'} · BotForge</title></svelte:head>

{#if missing}
	<h1 class="text-page">Bot not found</h1>
	<p class="mt-1 text-muted">It was deleted, or it belongs to another account. <a class="link" href="/dashboard">Back to your bots</a></p>
{:else if !bot || !d}
	{#if loadError}<Notice tone="fail">{loadError}</Notice>{:else}<Skeleton rows={4} label="Loading bot" />{/if}
{:else}
	<a href="/dashboard" class="inline-flex items-center gap-1 text-small text-muted hover:text-ink"><Icon name="chevronLeft" size={14} />Bots</a>

	<header class="spine sticky top-0 z-20 -mx-4 mt-1 border-b border-rule-soft bg-paper/95 py-3 pr-4 pl-7 backdrop-blur-sm sm:mx-0 sm:pl-5" data-tone={d.tone} data-busy={d.busy}>
		<div class="flex items-start gap-3">
			<div class="min-w-0 flex-1">
				<h1 class="truncate text-section sm:text-page">{bot.name}</h1>
				<div class="mt-0.5 flex flex-wrap items-center gap-x-3 gap-y-0.5">
					<StatusBadge {bot} {now} />
					{#if bot.shared}<span class="text-small text-muted">Shared with you</span>{/if}
				</div>
				{#if d.detail}<p class="mt-0.5 hidden max-w-3xl text-small sm:block {d.tone === 'fail' ? 'text-fail' : 'text-muted'}" aria-live="polite">{d.detail}</p>{/if}
			</div>
			{#if canPower}
				<div class="flex shrink-0 items-center gap-1.5" role="group" aria-label="Power controls">
					{#if settledIdle}
						<button class="btn btn-primary" disabled={acting} onclick={() => act('start')}><Icon name="play" size={12} />Start again</button>
					{:else if running}
						<button class="btn hidden sm:inline-flex" disabled={acting} onclick={() => act('restart')}><Icon name="restart" />Restart</button>
						<button class="btn" disabled={acting} onclick={() => act('stop')}><Icon name="stop" size={12} />Stop</button>
					{:else}
						<button class="btn btn-primary" disabled={acting} onclick={() => act('start')}><Icon name="play" size={12} />Start</button>
					{/if}
					<Menu label="More power actions" items={powerMenu} />
				</div>
			{/if}
		</div>
	</header>

	<div class="mt-3 flex flex-wrap items-center justify-between gap-x-6 gap-y-2">
		<p class="text-small text-muted">
			{bot.runtime}, {fmtBytes(bot.memory_bytes)} memory, {fmtCpu(bot.nano_cpus)}{#if bot.source_type === 'github'}, deployed from GitHub{/if}
		</p>
		{#if can(bot, Perm.console) && session.features.stats}<ResourceStrip botId={id} cpuLimit={bot.nano_cpus / 1e9} />{/if}
	</div>
	{#if loadError}<Notice tone="warn" class="mt-3">{loadError}</Notice>{/if}

	<div class="mt-6 lg:grid lg:grid-cols-[12.5rem_minmax(0,1fr)] lg:gap-8">
		<nav class="hidden lg:block" aria-label="Bot sections">
			<div class="sticky top-28 space-y-5">
				{#each visibleGroups as g (g.label)}
					<div>
						<p class="eyebrow px-2 pb-1.5">{g.label}</p>
						<ul>
							{#each g.items as s (s.id)}
								<li>
									<a
										href={sectionHref(s.id)}
										data-sveltekit-noscroll
										data-sveltekit-keepfocus
										class="flex items-center gap-2.5 rounded-control px-2 py-1.5 {tab === s.id ? 'bg-panel font-medium text-ink shadow-[inset_3px_0_0_var(--color-action)]' : 'text-ink/75 hover:bg-panel/70 hover:text-ink'}"
										aria-current={tab === s.id ? 'page' : undefined}
									>
										<Icon name={s.icon} class={tab === s.id ? 'text-action' : 'text-muted'} />{s.label}
									</a>
								</li>
							{/each}
						</ul>
					</div>
				{/each}
			</div>
		</nav>

		<div class="mb-4 lg:hidden">
			<label class="block">
				<span class="sr-only">Section</span>
				<select class="field font-medium" value={tab} onchange={(e) => pick(e.currentTarget.value)}>
					{#each visibleGroups as g (g.label)}
						<optgroup label={g.label}>
							{#each g.items as s (s.id)}<option value={s.id}>{s.label}</option>{/each}
						</optgroup>
					{/each}
				</select>
			</label>
		</div>

		<section class="min-w-0" aria-labelledby="section-title">
			<h2 id="section-title" class="sr-only">{current?.label}</h2>
			{#if configSection && !stopped}
				<Notice tone="warn" class="mb-4" title="Stop the bot to change these settings">
					Changes need a stopped bot and apply the next time it starts.
					{#snippet action()}
						{#if canPower}<button class="btn btn-sm" disabled={acting} onclick={() => act('stop')}>Stop bot</button>{/if}
					{/snippet}
				</Notice>
			{/if}

			{#if tab === 'overview'}
				<Overview {bot} {now} onSaved={(b) => (bot = b)} onAct={act} {acting} />
			{:else if tab === 'ai'}
				<AIOperator botId={id} targetName={bot.name} />
			{:else if tab === 'console'}
				<Console {bot} />
			{:else if tab === 'page'}
				<PageStudio botId={id} botName={bot.name} canEdit={can(bot, Perm.files)} canAdmin={admin} {stopped} />
			{:else if tab === 'files'}
				<Files botId={id} running={!stopped} />
			{:else if tab === 'packages'}
				<Packages botId={id} running={!stopped} />
			{:else if tab === 'deploy'}
				<Deploy {bot} {admin} />
			{:else if tab === 'env'}
				<Env {bot} running={!stopped} />
			{:else if tab === 'startup'}
				<Startup {bot} {stopped} onSaved={(b) => (bot = b)} />
			{:else if tab === 'network'}
				<Network {bot} {stopped} owner={isOwner} onSaved={(b) => (bot = b)} />
			{:else if tab === 'backups'}
				<Backups {bot} {stopped} {admin} onSaved={(b) => (bot = b)} />
			{:else if tab === 'alerts'}
				<Alerts {bot} {admin} />
			{:else if tab === 'schedules'}
				<Schedules {bot} />
			{:else if tab === 'users'}
				<Users botId={id} botName={bot.name} />
			{:else if tab === 'settings'}
				<Settings {bot} {stopped} onSaved={(b) => (bot = b)} />
			{/if}

			<div class="mt-10 flex justify-between gap-2 border-t border-rule-soft pt-3 lg:hidden">
				{#if idx > 0}<a class="btn btn-quiet" href={sectionHref(flat[idx - 1].id)} data-sveltekit-noscroll><Icon name="chevronLeft" size={14} />{flat[idx - 1].label}</a>{:else}<span></span>{/if}
				{#if idx >= 0 && idx < flat.length - 1}<a class="btn btn-quiet" href={sectionHref(flat[idx + 1].id)} data-sveltekit-noscroll>{flat[idx + 1].label}<Icon name="chevronRight" size={14} /></a>{/if}
			</div>
		</section>
	</div>
{/if}
