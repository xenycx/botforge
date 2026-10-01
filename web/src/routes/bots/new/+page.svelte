<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { api, ApiError, fmtBytes, MiB } from '$lib/api/client';
	import type { Bot, Connection, GitHubRepo, Limits, RuntimeInfo, Template } from '$lib/api/types';
	import { toast } from '$lib/ui/toast.svelte';
	import { session } from '$lib/session.svelte';
	import Icon from '$lib/components/ui/Icon.svelte';
	import Notice from '$lib/components/ui/Notice.svelte';
	import Skeleton from '$lib/components/ui/Skeleton.svelte';
	import { creatable, loadWorkspaces, workspaces } from '$lib/workspaces.svelte';

	type Source = 'template' | 'github' | 'blank';
	const steps = ['Source', 'Details', 'Setup values', 'Review'];

	const initial = page.url.searchParams.get('source') as Source | null;
	let source = $state<Source>(initial && ['template', 'github', 'blank'].includes(initial) ? initial : 'template');
	let step = $state(initial ? 1 : 0);

	let templates = $state<Template[] | null>(null);
	let runtimes = $state<RuntimeInfo[]>([]);
	let limits = $state<Limits | null>(null);
	let gh = $state<Connection | null>(null);
	let repos = $state<GitHubRepo[] | null>(null);
	let branches = $state<string[]>([]);
	let loadError = $state('');

	let templateId = $state(page.url.searchParams.get('template') ?? '');
	let name = $state('');
	let nameTouched = $state(false);
	let runtime = $state('nodejs');
	let repo = $state({ full_name: '', branch: '', root_dir: '', auto_deploy: true });
	let env = $state<Record<string, string>>({});
	let extra = $state<{ name: string; value: string }[]>([]);
	let shown = $state<Record<string, boolean>>({});
	let memoryMiB = $state<number | null>(null);
	let cpus = $state<number | null>(null);
	let startNow = $state(true);
	// New bots go into the workspace selected in the sidebar (personal when "all").
	let workspaceId = $state(workspaces.selected !== 'all' ? workspaces.selected : '');
	$effect(() => {
		loadWorkspaces();
	});
	// The personal workspace is the server default, so it is sent as "".
	$effect(() => {
		if (workspaceId && workspaces.list.find((w) => w.id === workspaceId)?.personal) workspaceId = '';
	});
	const targets = $derived(creatable());

	let problems = $state<Record<string, string>>({});
	let submitError = $state('');
	let busy = $state(false);
	let heading: HTMLHeadingElement | undefined = $state();

	onMount(async () => {
		try {
			const [t, r] = await Promise.all([
				api<{ templates: Template[] }>('GET', '/templates'),
				api<{ runtimes: RuntimeInfo[]; limits: Limits }>('GET', '/runtimes')
			]);
			templates = t.templates;
			runtimes = r.runtimes;
			limits = r.limits;
		} catch (e) {
			loadError = e instanceof ApiError ? e.message : 'The templates could not be loaded.';
			templates = [];
		}
		try {
			gh = (await api<{ connections: Connection[] }>('GET', '/me/connections')).connections.find((c) => c.provider === 'github') ?? null;
		} catch {
			gh = null;
		}
		if (source === 'github') loadRepos();
	});

	const tpl = $derived(templates?.find((t) => t.id === templateId));
	const rtId = $derived(source === 'template' ? (tpl?.runtime ?? '') : runtime);
	const rt = $derived(runtimes.find((r) => r.id === rtId));
	const required = $derived(tpl?.env ?? []);
	const effMemory = $derived(memoryMiB && memoryMiB > 0 ? Math.round(memoryMiB * MiB) : (rt?.default_memory_bytes ?? 0));
	const effCpu = $derived(cpus && cpus > 0 ? Math.round(cpus * 1e9) : (rt?.default_nano_cpus ?? 0));
	const buildMemory = $derived(source === 'template' ? (tpl?.build_memory_bytes ?? 0) : rt?.has_build ? rt.build_memory_bytes : 0);
	const missingRequired = $derived(required.filter((v) => v.required && !(env[v.name] ?? '').trim()));

	async function loadRepos() {
		if (repos || !gh?.linked) return;
		try {
			repos = (await api<{ repos: GitHubRepo[] }>('GET', '/me/github/repos')).repos;
		} catch (e) {
			loadError = e instanceof ApiError ? e.message : 'Your repositories could not be loaded.';
			repos = [];
		}
	}
	async function pickRepo() {
		branches = [];
		repo.branch = repos?.find((r) => r.full_name === repo.full_name)?.default_branch ?? '';
		if (!nameTouched) name = repo.full_name.split('/')[1] ?? '';
		if (!repo.full_name) return;
		try {
			branches = (await api<{ branches: string[] }>('GET', `/me/github/branches?repo=${encodeURIComponent(repo.full_name)}`)).branches;
		} catch (e) {
			problems.repo = e instanceof ApiError ? e.message : 'Branches could not be loaded.';
		}
	}
	function pickTemplate(t: Template) {
		templateId = t.id;
		if (!nameTouched) name = t.name.replace(/ starter$/i, '').replace(/ \(.*\)$/, '') + ' bot';
		for (const v of t.env) if (v.default && !(v.name in env)) env[v.name] = v.default;
		memoryMiB = null;
		problems = {};
	}
	function chooseSource(s: Source) {
		source = s;
		problems = {};
		if (s === 'github') loadRepos();
	}

	function validate(i: number): boolean {
		const p: Record<string, string> = {};
		if (i === 1) {
			if (source === 'template' && !templateId) p.template = 'Choose a template.';
			if (source === 'github' && gh?.linked) {
				if (!repo.full_name) p.repo = 'Choose a repository.';
				else if (!repo.branch) p.branch = 'Choose a branch.';
			}
			if (source === 'github' && !gh?.linked) p.repo = 'Connect GitHub first.';
			if (!name.trim()) p.name = 'Give the bot a name.';
			else if (name.trim().length > 64) p.name = 'Use at most 64 characters.';
		}
		if (i === 2) {
			for (const v of missingRequired) p['env.' + v.name] = `${v.label || v.name} is required to run this template.`;
			const seen = new Set(required.map((v) => v.name));
			extra.forEach((x, n) => {
				if (!x.name && !x.value) return;
				if (!/^[A-Za-z_][A-Za-z0-9_]*$/.test(x.name)) p['extra.' + n] = 'Use letters, digits and underscores; start with a letter.';
				else if (/^(BOTPANEL_|LD_)|^PATH$/i.test(x.name)) p['extra.' + n] = 'This name is reserved by the panel.';
				else if (seen.has(x.name)) p['extra.' + n] = 'This variable is already listed.';
				seen.add(x.name);
			});
		}
		if (i === 3 && limits) {
			if (effMemory < Math.max(limits.min_memory_bytes, rt?.min_memory_bytes ?? 0) || effMemory > limits.max_memory_bytes)
				p.memory = `Choose between ${fmtBytes(Math.max(limits.min_memory_bytes, rt?.min_memory_bytes ?? 0))} and ${fmtBytes(limits.max_memory_bytes)}.`;
			if (effCpu < limits.min_nano_cpus || effCpu > limits.max_nano_cpus)
				p.cpu = `Choose between ${limits.min_nano_cpus / 1e9} and ${limits.max_nano_cpus / 1e9} CPU.`;
		}
		problems = p;
		return Object.keys(p).length === 0;
	}

	async function go(to: number) {
		if (to > step) for (let i = step; i < to; i++) if (!validate(i)) return focusProblem();
		problems = {};
		step = to;
		await Promise.resolve();
		heading?.focus();
	}
	async function focusProblem() {
		await Promise.resolve();
		document.querySelector<HTMLElement>('[aria-invalid="true"], [data-problem]')?.focus();
	}

	async function create() {
		for (let i = 1; i <= 3; i++)
			if (!validate(i)) {
				step = i;
				return focusProblem();
			}
		busy = true;
		submitError = '';
		const vars: Record<string, string> = {};
		for (const [k, v] of Object.entries(env)) if (v.trim()) vars[k] = v;
		for (const x of extra) if (x.name) vars[x.name] = x.value;
		const body: Record<string, unknown> = { name: name.trim(), runtime: rtId };
		if (memoryMiB && memoryMiB > 0) body.memory_bytes = effMemory;
		if (cpus && cpus > 0) body.nano_cpus = effCpu;
		if (Object.keys(vars).length) body.env = vars;
		if (source === 'template') body.template_id = templateId;
		if (source === 'github') body.github = repo;
		if (workspaceId) body.workspace_id = workspaceId;
		try {
			const b = await api<Bot>('POST', '/bots', body);
			if (startNow && canStart) {
				try {
					await api('POST', `/bots/${b.id}/start`);
				} catch (e) {
					toast(`Created ${b.name}, but it could not be started: ${e instanceof ApiError ? e.message : 'request failed'}`, 'warn');
				}
			}
			toast(`Created ${b.name}`);
			await goto(`/bots/${b.id}?tab=overview`);
		} catch (e) {
			// Keep everything the user entered so they can correct it.
			submitError = e instanceof ApiError ? e.message : 'The bot could not be created. Check your connection and try again.';
			busy = false;
		}
	}

	const canStart = $derived(session.features.runner && source !== 'blank' && missingRequired.length === 0 && source !== 'github');
	const sourceLabel = $derived(source === 'template' ? (tpl?.name ?? 'Template') : source === 'github' ? 'GitHub repository' : 'Empty workspace');
	const secretLike = (n: string) => /token|secret|key|password|pass|auth/i.test(n);
</script>

<svelte:head><title>New bot · BotForge</title></svelte:head>

<div class="mx-auto max-w-3xl">
	<a href="/dashboard" class="inline-flex items-center gap-1 text-muted hover:text-ink"><Icon name="chevronLeft" size={14} />Bots</a>
	<h1 class="mt-1 text-page">New bot</h1>

	<ol class="mt-5 hidden grid-cols-4 gap-2 sm:grid" aria-label="Steps">
		{#each steps as s, i (s)}
			<li>
				<button
					class="w-full border-t-[3px] pt-2 text-left {i === step ? 'border-action' : i < step ? 'border-ink/60' : 'border-rule'}"
					aria-current={i === step ? 'step' : undefined}
					disabled={i > step}
					onclick={() => go(i)}
				>
					<span class="block text-small text-muted">Step {i + 1}</span>
					<span class="font-medium {i === step ? '' : i < step ? 'text-ink/80' : 'text-muted'}">{s}</span>
				</button>
			</li>
		{/each}
	</ol>
	<p class="mt-4 text-muted sm:hidden">Step {step + 1} of 4</p>

	<section class="mt-6" aria-labelledby="step-h">
		<h2 id="step-h" bind:this={heading} tabindex="-1" class="text-section outline-none">
			{['Where does the code come from?', source === 'template' ? 'Choose a template and name the bot' : source === 'github' ? 'Choose a repository' : 'Choose a language and name the bot', 'Add the values the bot needs', 'Review and create'][step]}
		</h2>

		{#if loadError}<Notice tone="fail" class="mt-3">{loadError}</Notice>{/if}

		{#if step === 0}
			<div class="mt-4 grid gap-3" role="radiogroup" aria-label="Source">
				{#each [{ id: 'template', title: 'Start from a template', body: 'A working slash-command bot in JavaScript, Python, Rust, Java or Go. Best for a first bot: you only add the token.', icon: 'box' }, { id: 'github', title: 'Deploy from GitHub', body: 'Use an existing repository. The panel downloads a branch and can redeploy on every push.', icon: 'github' }, { id: 'blank', title: 'Empty bot', body: 'Choose a language and upload your code through Files or SFTP.', icon: 'folder' }] as o (o.id)}
					<button
						role="radio"
						aria-checked={source === o.id}
						class="flex items-start gap-3 rounded-tile border bg-panel p-4 text-left transition-colors {source === o.id ? 'border-action ring-1 ring-action' : 'border-rule-soft hover:border-rule'}"
						onclick={() => chooseSource(o.id as Source)}
					>
						<span class="mt-0.5 grid size-8 shrink-0 place-items-center {source === o.id ? 'bg-action text-white' : 'bg-paper text-ink'}"><Icon name={o.icon as 'box'} /></span>
						<span>
							<span class="block text-title font-semibold">{o.title}{#if o.id === 'template'}<span class="ml-2 text-small font-normal text-run">Recommended</span>{/if}</span>
							<span class="mt-0.5 block text-muted">{o.body}</span>
						</span>
					</button>
				{/each}
			</div>
		{:else if step === 1}
			{#if source === 'template'}
				{#if templates === null}
					<div class="mt-4"><Skeleton rows={3} /></div>
				{:else}
					<div class="mt-4 grid gap-3 sm:grid-cols-2" role="radiogroup" aria-label="Templates" aria-invalid={problems.template ? 'true' : undefined} tabindex={problems.template ? -1 : undefined} data-problem={problems.template ? '' : undefined}>
						{#each templates as t (t.id)}
							<button
								role="radio"
								aria-checked={templateId === t.id}
								class="flex flex-col rounded-tile border bg-panel p-4 text-left {templateId === t.id ? 'border-action ring-1 ring-action' : 'border-rule-soft hover:border-rule'}"
								onclick={() => pickTemplate(t)}
							>
								<span class="flex items-baseline justify-between gap-2"><span class="text-title font-semibold">{t.name}</span><span class="text-small text-muted">{t.language}</span></span>
								<span class="mt-1 text-muted">{t.description}</span>
								<span class="mt-3 text-small text-muted">
									Needs {t.env.filter((v) => v.required).map((v) => v.name).join(', ') || 'no values'}.
									{#if t.has_build}Builds with up to {fmtBytes(t.build_memory_bytes)} of memory.{/if}
								</span>
							</button>
						{/each}
					</div>
					{#if problems.template}<p class="mt-2 text-small text-fail">{problems.template}</p>{/if}
				{/if}
			{:else if source === 'github'}
				{#if !gh?.linked}
					<Notice tone="warn" class="mt-4" title="Connect GitHub first">
						<p>The panel uses your GitHub connection to list repositories and download code. <a href="/settings/connected-accounts">Open connected accounts</a>, connect GitHub, then come back here.</p>
					</Notice>
				{:else}
					{#if !gh.repo_access}
						<Notice class="mt-4">Only public repositories are listed until you grant repository access in <a href="/settings/connected-accounts">Connected accounts</a>.</Notice>
					{/if}
					<div class="mt-4 grid gap-4 sm:grid-cols-2">
						<label class="block sm:col-span-2">
							<span class="label">Repository</span>
							<select class="field" bind:value={repo.full_name} onchange={pickRepo} aria-invalid={problems.repo ? 'true' : undefined} disabled={repos === null}>
								<option value="" disabled>{repos === null ? 'Loading repositories…' : 'Select a repository'}</option>
								{#each repos ?? [] as r (r.full_name)}<option value={r.full_name}>{r.full_name}{r.private ? ' (private)' : ''}</option>{/each}
							</select>
							{#if problems.repo}<span class="help text-fail!">{problems.repo}</span>{/if}
						</label>
						<label class="block">
							<span class="label">Branch</span>
							<select class="field" bind:value={repo.branch} aria-invalid={problems.branch ? 'true' : undefined} disabled={!repo.full_name}>
								{#if repo.branch && !branches.includes(repo.branch)}<option value={repo.branch}>{repo.branch}</option>{/if}
								{#each branches as b (b)}<option value={b}>{b}</option>{/each}
							</select>
							{#if problems.branch}<span class="help text-fail!">{problems.branch}</span>{/if}
						</label>
						<label class="block">
							<span class="label">Folder inside the repository</span>
							<input class="field font-mono" bind:value={repo.root_dir} placeholder="/" maxlength="200" />
							<span class="help">Leave empty to deploy the whole repository.</span>
						</label>
						<label class="flex items-start gap-2.5 sm:col-span-2">
							<input type="checkbox" class="mt-0.5" bind:checked={repo.auto_deploy} />
							<span>Deploy automatically when this branch changes<span class="help">The panel adds a webhook to the repository.</span></span>
						</label>
						<label class="block">
							<span class="label">Language</span>
							<select class="field" bind:value={runtime}>{#each runtimes as r (r.id)}<option value={r.id}>{r.display_name}</option>{/each}</select>
						</label>
					</div>
				{/if}
			{:else}
				<label class="mt-4 block max-w-xs">
					<span class="label">Language</span>
					<select class="field" bind:value={runtime}>{#each runtimes as r (r.id)}<option value={r.id}>{r.display_name}</option>{/each}</select>
					{#if rt}<span class="help">Starts with <code>{rt.default_argv.join(' ')}</code>. You can change this later under Startup.</span>{/if}
				</label>
			{/if}
			<label class="mt-5 block max-w-md">
				<span class="label">Name</span>
				<input class="field" maxlength="64" bind:value={name} oninput={() => (nameTouched = true)} placeholder="Music bot" aria-invalid={problems.name ? 'true' : undefined} aria-describedby="name-help" />
				<span id="name-help" class="help {problems.name ? 'text-fail!' : ''}">{problems.name || 'Only members of its workspace and people you share the bot with see this name.'}</span>
			</label>
			{#if targets.length > 1}
				<label class="mt-4 block max-w-md">
					<span class="label">Workspace</span>
					<select class="field" bind:value={workspaceId}>
						{#each targets as w (w.id)}<option value={w.personal ? '' : w.id}>{w.personal ? 'Personal' : w.name}</option>{/each}
					</select>
					<span class="help">Members of the workspace get access according to their role.</span>
				</label>
			{/if}
		{:else if step === 2}
			{#if tpl}
				<div class="mt-4 surface p-4">
					<h3 class="font-semibold">Before the first start</h3>
					<ol class="mt-2 list-decimal space-y-1 pl-5 text-ink/90">
						{#each tpl.setup as s, i (i)}<li>{s}</li>{/each}
					</ol>
					{#if tpl.privileged_intents.length}
						<p class="mt-2 text-muted">Enable these privileged intents on the Bot page: {tpl.privileged_intents.join(', ')}.</p>
					{:else}
						<p class="mt-2 text-small text-muted">This template needs no privileged gateway intents.</p>
					{/if}
				</div>
			{/if}
			<div class="mt-5 grid gap-4">
				{#each required as v (v.name)}
					<label class="block">
						<span class="label">{v.label || v.name} <code class="ml-1 text-small font-normal text-muted">{v.name}</code>{#if !v.required}<span class="ml-1 font-normal text-muted">(optional)</span>{/if}</span>
						<span class="relative block">
							<input
								class="field pr-10 font-mono"
								type={v.secret && !shown[v.name] ? 'password' : 'text'}
								bind:value={env[v.name]}
								autocomplete="off"
								spellcheck="false"
								aria-invalid={problems['env.' + v.name] ? 'true' : undefined}
								aria-describedby="env-{v.name}-help"
							/>
							{#if v.secret}
								<button type="button" class="btn btn-quiet btn-icon btn-sm absolute top-1/2 right-1 -translate-y-1/2" aria-label={shown[v.name] ? `Hide ${v.name}` : `Show ${v.name}`} aria-pressed={!!shown[v.name]} onclick={() => (shown[v.name] = !shown[v.name])}>
									<Icon name={shown[v.name] ? 'eyeOff' : 'eye'} />
								</button>
							{/if}
						</span>
						<span id="env-{v.name}-help" class="help {problems['env.' + v.name] ? 'text-fail!' : ''}">{problems['env.' + v.name] || v.description}</span>
					</label>
				{/each}
				{#if source !== 'template'}
					<p class="text-muted">Add the variables your code reads, such as <code>DISCORD_TOKEN</code>. You can also add them later under Environment.</p>
				{/if}
				{#each extra as x, i (i)}
					<div class="grid gap-2 sm:grid-cols-[minmax(0,1fr)_minmax(0,1.6fr)_auto] sm:items-start">
						<label class="block"><span class="sr-only">Variable name</span><input class="field font-mono" placeholder="NAME" bind:value={x.name} autocomplete="off" spellcheck="false" aria-invalid={problems['extra.' + i] ? 'true' : undefined} /></label>
						<label class="block"><span class="sr-only">Value of {x.name || 'variable'}</span><input class="field font-mono" placeholder="value" type={secretLike(x.name) ? 'password' : 'text'} bind:value={x.value} autocomplete="off" spellcheck="false" /></label>
						<button type="button" class="btn btn-quiet" onclick={() => (extra = extra.filter((_, j) => j !== i))}>Remove</button>
						{#if problems['extra.' + i]}<p class="text-small text-fail sm:col-span-3">{problems['extra.' + i]}</p>{/if}
					</div>
				{/each}
				<div><button type="button" class="btn" onclick={() => (extra = [...extra, { name: extra.length || source === 'template' ? '' : 'DISCORD_TOKEN', value: '' }])}><Icon name="plus" />Add variable</button></div>
				<p class="text-small text-muted">Values are sent once over this connection and stored encrypted. They are never put in the page address or saved in this browser.</p>
			</div>
		{:else}
			<dl class="mt-4 grid grid-cols-[auto_minmax(0,1fr)] gap-x-6 gap-y-2 border-y border-rule-soft py-3">
				<dt class="text-muted">Name</dt><dd class="font-medium">{name}</dd>
				{#if targets.length > 1}<dt class="text-muted">Workspace</dt><dd>{targets.find((w) => (w.personal ? '' : w.id) === workspaceId)?.name ?? 'Personal'}</dd>{/if}
				<dt class="text-muted">Source</dt><dd>{sourceLabel}{#if source === 'github'}: <code>{repo.full_name}</code> on <code>{repo.branch}</code>{/if}</dd>
				<dt class="text-muted">Language</dt><dd>{rt?.display_name ?? rtId}</dd>
				<dt class="text-muted">Variables</dt>
				<dd>{Object.keys(env).filter((k) => env[k]?.trim()).concat(extra.filter((x) => x.name).map((x) => x.name)).join(', ') || 'None yet'}</dd>
			</dl>
			<div class="mt-5 grid gap-4 sm:grid-cols-2">
				<label class="block">
					<span class="label">Memory limit</span>
					<span class="flex items-center gap-2"><input class="field" type="number" step="any" min="0" bind:value={memoryMiB} placeholder={rt ? String(Math.round(rt.default_memory_bytes / MiB)) : ''} aria-invalid={problems.memory ? 'true' : undefined} /><span class="text-muted">MiB</span></span>
					<span class="help {problems.memory ? 'text-fail!' : ''}">{problems.memory || `Default ${fmtBytes(rt?.default_memory_bytes ?? 0)}. The process is stopped if it uses more.`}</span>
				</label>
				<label class="block">
					<span class="label">CPU limit</span>
					<span class="flex items-center gap-2"><input class="field" type="number" step="any" min="0" bind:value={cpus} placeholder={rt ? String(rt.default_nano_cpus / 1e9) : ''} aria-invalid={problems.cpu ? 'true' : undefined} /><span class="text-muted">cores</span></span>
					<span class="help {problems.cpu ? 'text-fail!' : ''}">{problems.cpu || `Default ${(rt?.default_nano_cpus ?? 0) / 1e9} cores.`}</span>
				</label>
			</div>
			{#if buildMemory > 0}
				<Notice class="mt-4" title="Build memory is separate">
					The first start runs a build container that can use up to {fmtBytes(buildMemory)} of memory{#if buildMemory > effMemory}, more than the {fmtBytes(effMemory)} the bot itself gets{/if}. {tpl?.first_start ?? ''}
				</Notice>
			{/if}
			{#if canStart}
				<label class="mt-5 flex items-start gap-2.5"><input type="checkbox" class="mt-0.5" bind:checked={startNow} /><span>Start the bot after creating it<span class="help">You can follow the build on its overview page.</span></span></label>
			{:else if !session.features.runner}
				<Notice class="mt-5">This panel runs without Docker, so the bot is created but cannot be started here.</Notice>
			{:else if source === 'template'}
				<Notice tone="warn" class="mt-5">The bot is created stopped because required values are missing.</Notice>
			{/if}
			{#if submitError}<Notice tone="fail" class="mt-4" live>{submitError}</Notice>{/if}
		{/if}
	</section>

	<div class="sticky bottom-0 mt-8 flex items-center gap-2 border-t border-rule-soft bg-paper/95 py-3 backdrop-blur-sm">
		{#if step > 0}<button class="btn" onclick={() => go(step - 1)}><Icon name="chevronLeft" size={14} />Back</button>{/if}
		<a href="/dashboard" class="btn btn-quiet">Cancel</a>
		<span class="flex-1"></span>
		{#if step < 3}
			<button class="btn btn-primary" onclick={() => go(step + 1)} disabled={source === 'github' && step === 1 && !gh?.linked}>Continue<Icon name="chevronRight" size={14} /></button>
		{:else}
			<button class="btn btn-primary" onclick={create} disabled={busy}>{busy ? 'Creating…' : startNow && canStart ? 'Create and start' : 'Create bot'}</button>
		{/if}
	</div>
</div>
