<script lang="ts">
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { api, ApiError } from '$lib/api/client';
	import { oauthErrors, type Connection, type Provider } from '$lib/api/types';
	import { confirmDialog } from '$lib/ui/dialogs.svelte';
	import { toast } from '$lib/ui/toast.svelte';
	import Icon from '$lib/components/ui/Icon.svelte';
	import Notice from '$lib/components/ui/Notice.svelte';

	const meta: Record<Provider, { name: string; blurb: string }> = {
		discord: { name: 'Discord', blurb: 'Connect to receive deployment notifications via Discord.' },
		github: { name: 'GitHub', blurb: 'Connect to deploy from private repositories without re-prompting.' }
	};

	let connections = $state<Connection[]>([]);
	let loaded = $state(false);
	let error = $state('');
	let notice = $state('');
	let busy = $state<string>('');

	async function load() {
		connections = (await api<{ connections: Connection[] }>('GET', '/me/connections')).connections;
		loaded = true;
	}

	onMount(async () => {
		const code = page.url.searchParams.get('error');
		if (code) error = oauthErrors[code] ?? 'Could not connect the account.';
		const linked = page.url.searchParams.get('linked');
		if (linked) notice = `${meta[linked as Provider]?.name ?? linked} connected.`;
		try {
			await load();
		} catch {
			error = error || 'Could not load your connected accounts.';
		}
	});

	async function connect(p: Provider, notifications = false, repoAccess = false) {
		busy = p;
		error = '';
		try {
			const r = await api<{ url: string }>('POST', `/me/connections/${p}/start`, { notifications, repo_access: repoAccess });
			window.location.assign(r.url); // full navigation to the provider
		} catch (err) {
			error = err instanceof ApiError ? err.message : 'Could not start the connection.';
			busy = '';
		}
	}

	async function disconnect(c: Connection) {
		const ok = await confirmDialog({
			title: `Disconnect ${meta[c.provider].name}?`,
			body: c.provider === 'github' ? 'You can no longer sign in with GitHub, and deployments that use your GitHub access stop working until you connect again.' : 'You can no longer sign in with Discord, and Discord notifications stop.',
			confirmLabel: 'Disconnect',
			tone: 'danger'
		});
		if (!ok) return;
		busy = c.provider;
		error = notice = '';
		try {
			await api('DELETE', `/me/connections/${c.provider}`);
			await load();
			toast(`${meta[c.provider].name} disconnected`);
		} catch (err) {
			error = err instanceof ApiError ? err.message : 'Could not disconnect.';
		} finally {
			busy = '';
		}
	}

	const description = (c: Connection) =>
		c.linked
			? c.provider === 'discord'
				? `Connected as ${c.username}. Notifications ${c.notifications ? 'are on' : 'are off'}.`
				: `Connected as ${c.username}. ${c.repo_access ? 'Private repositories and webhooks are enabled.' : 'Public repositories only.'}`
			: meta[c.provider].blurb;
</script>

<svelte:head><title>Connected accounts · BotForge</title></svelte:head>

<h2 class="text-section">Connected accounts</h2>
<p class="mt-1 max-w-prose text-muted">Sign in with GitHub or Discord instead of a password, deploy from your repositories, and receive notifications in Discord. Accounts are only linked from here, never matched by email address.</p>
{#if notice}<Notice tone="success" class="mt-4" live>{notice}</Notice>{/if}
{#if error}<Notice tone="fail" class="mt-4" live>{error}</Notice>{/if}

<ul class="mt-5 divide-y divide-rule-soft border-y border-rule-soft bg-panel">
	{#each connections as c (c.provider)}
		<li class="flex flex-wrap items-center gap-4 px-4 py-4">
			<span class="grid size-10 shrink-0 place-items-center rounded-control {c.provider === 'discord' ? 'bg-[#5865f2] text-white' : 'bg-ink text-paper'}" aria-hidden="true"><Icon name={c.provider} size={20} /></span>
			<div class="min-w-0 flex-1 basis-56">
				<p class="flex flex-wrap items-center gap-2">
					<span class="font-semibold">{meta[c.provider].name}</span>
					<span class="rounded-control px-1.5 py-px text-small font-medium {c.linked ? 'bg-run/12 text-run' : 'bg-paper-2 text-muted'}">{c.linked ? 'Linked' : c.configured ? 'Not linked' : 'Not set up on this panel'}</span>
				</p>
				<p class="text-muted">{c.configured ? description(c) : `The administrator has to configure ${meta[c.provider].name} sign-in first (docs/oauth.md).`}</p>
			</div>
			<div class="flex flex-wrap gap-2">
				{#if c.linked && c.provider === 'discord' && !c.notifications}
					<button class="btn" disabled={busy !== ''} onclick={() => connect('discord', true)}>Turn on notifications</button>
				{/if}
				{#if c.linked && c.provider === 'github' && !c.repo_access}
					<button class="btn" disabled={busy !== ''} onclick={() => connect('github', false, true)} title="Needed to deploy private repositories and add webhooks">Grant repository access</button>
				{/if}
				{#if c.linked}
					<button class="btn btn-danger" disabled={!c.can_disconnect || busy !== ''} title={c.can_disconnect ? '' : 'This is your only way to sign in. Add a password first.'} onclick={() => disconnect(c)}>Disconnect</button>
				{:else}
					<button class="btn btn-primary" disabled={!c.configured || busy !== ''} onclick={() => connect(c.provider, c.provider === 'discord')}>Connect</button>
				{/if}
			</div>
		</li>
	{:else}
		{#if loaded}<li class="px-4 py-5 text-muted">No sign-in providers are available on this panel.</li>{/if}
	{/each}
</ul>
<p class="mt-3 max-w-prose text-small text-muted">You always keep at least one way to sign in: Disconnect is unavailable for your only method. A password counts; add one under Security.</p>
