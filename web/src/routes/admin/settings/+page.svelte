<script lang="ts">
	import { onMount } from 'svelte';
	import { api, ApiError } from '$lib/api/client';
	import { loadSession } from '$lib/session.svelte';
	import { toast } from '$lib/ui/toast.svelte';
	import Icon from '$lib/components/ui/Icon.svelte';
	import Notice from '$lib/components/ui/Notice.svelte';
	import Skeleton from '$lib/components/ui/Skeleton.svelte';
	import ProviderSetup from '$lib/components/ProviderSetup.svelte';
	import AISettings from '$lib/components/AISettings.svelte';

	type View = {
		public_url: string;
		github_client_id: string;
		github_secret_set: boolean;
		discord_client_id: string;
		discord_secret_set: boolean;
		oauth_allow_signup: boolean;
		locked: Record<string, boolean>;
		github_enabled: boolean;
		discord_enabled: boolean;
	};
	let v = $state<View | null>(null);
	let error = $state('');
	let warning = $state('');
	let saving = $state(false);
	let publicUrl = $state('');
	let ghId = $state('');
	let ghSecret = $state('');
	let dcId = $state('');
	let dcSecret = $state('');
	let signup = $state(false);

	function fill(x: View) {
		v = x;
		publicUrl = x.public_url;
		ghId = x.github_client_id;
		dcId = x.discord_client_id;
		ghSecret = dcSecret = '';
		signup = x.oauth_allow_signup;
	}
	onMount(async () => {
		try {
			warning = sessionStorage.getItem('botpanel.setupWarning') ?? '';
			sessionStorage.removeItem('botpanel.setupWarning');
		} catch {
			/* ignore */
		}
		try {
			fill(await api<View>('GET', '/admin/settings'));
		} catch (e) {
			error = e instanceof ApiError ? e.message : 'Settings could not be loaded.';
		}
	});

	async function save(e: SubmitEvent) {
		e.preventDefault();
		if (!v) return;
		saving = true;
		error = '';
		const body: Record<string, unknown> = {};
		if (!v.locked.public_url) body.public_url = publicUrl;
		if (!v.locked.github_client_id) body.github_client_id = ghId;
		if (!v.locked.github_client_secret && ghSecret) body.github_client_secret = ghSecret;
		if (!v.locked.discord_client_id) body.discord_client_id = dcId;
		if (!v.locked.discord_client_secret && dcSecret) body.discord_client_secret = dcSecret;
		if (!v.locked.oauth_allow_signup) body.oauth_allow_signup = signup;
		try {
			fill(await api<View>('PUT', '/admin/settings', body));
			await loadSession(); // feature flags follow the new providers
			warning = '';
			toast('Settings saved and applied', 'success');
		} catch (err) {
			error = err instanceof ApiError ? err.message.charAt(0).toUpperCase() + err.message.slice(1) : 'The settings could not be saved.';
		} finally {
			saving = false;
		}
	}
	async function disable(p: 'github' | 'discord') {
		try {
			fill(await api<View>('PUT', '/admin/settings', { [`${p}_client_secret`]: '', [`${p}_client_id`]: '' }));
			await loadSession();
			toast(`${p === 'github' ? 'GitHub' : 'Discord'} sign-in turned off`);
		} catch (err) {
			toast(err instanceof ApiError ? err.message : 'The request failed.', 'fail');
		}
	}
</script>

<svelte:head><title>Panel settings · BotForge</title></svelte:head>

<h2 class="text-section">Panel settings</h2>
<p class="mt-1 max-w-3xl text-muted">Applied immediately, without a restart. Values set in the environment file are shown read-only and always win.</p>
{#if warning}<Notice tone="warn" class="mt-3" title="Setup finished, but some settings were not saved">{warning}</Notice>{/if}
{#if error}<Notice tone="fail" class="mt-3" live>{error}</Notice>{/if}

{#if !v && !error}
	<div class="mt-4"><Skeleton rows={4} label="Loading settings" /></div>
{:else if v}
	<form class="mt-6 grid w-full min-w-0 grid-cols-[minmax(0,1fr)] gap-4 xl:grid-cols-2" onsubmit={save}>
		<section class="card flex flex-col p-5 sm:p-6">
			<div class="flex items-center gap-2"><Icon name="globe" size={18} class="text-muted" /><h3 class="text-title font-semibold">Panel address</h3></div>
			<p class="mt-1 text-small text-muted">The https origin people use to reach this panel. Sign-in providers return to it, and bots send statistics to it.</p>
			<label class="mt-auto block pt-4">
				<span class="sr-only">Panel address</span>
				<input class="field font-mono" bind:value={publicUrl} disabled={v.locked.public_url} placeholder="https://panel.example.com" />
				{#if v.locked.public_url}<span class="help">Set in the environment file (BOTPANEL_PUBLIC_URL).</span>{/if}
			</label>
		</section>

		<section class="card flex flex-col p-5 sm:p-6">
			<div class="flex items-center gap-2"><Icon name="users" size={18} class="text-muted" /><h3 class="text-title font-semibold">Registration</h3><span class="pill ml-auto" data-tone={signup ? 'run' : 'idle'}>{signup ? 'Open' : 'Closed'}</span></div>
			<p class="mt-1 text-small text-muted">Who can create an account without an administrator.</p>
			<label class="mt-auto flex items-start gap-2.5 pt-4">
				<input type="checkbox" class="mt-0.5" bind:checked={signup} disabled={v.locked.oauth_allow_signup} />
				<span>Allow new accounts from invitations, GitHub, or Discord<span class="help">Turn this off to stop every self-registration path. Administrators can still create accounts directly. Provider identities are never merged into an account by matching email.</span></span>
			</label>
		</section>

		{#each [{ id: 'github', label: 'GitHub', on: v.github_enabled, icon: 'github' }, { id: 'discord', label: 'Discord', on: v.discord_enabled, icon: 'discord' }] as p (p.id)}
			<section class="card p-5 sm:p-6">
				<div class="flex flex-wrap items-center gap-2">
					<Icon name={p.icon as 'github'} size={20} />
					<h3 class="text-title font-semibold">{p.label} sign-in</h3>
					<span class="pill" data-tone={p.on ? 'run' : 'idle'}>{p.on ? 'On' : 'Off'}</span>
					<span class="flex-1"></span>
					{#if p.on && !v.locked[`${p.id}_client_id`]}<button type="button" class="btn btn-sm btn-danger" onclick={() => disable(p.id as 'github')}>Turn off</button>{/if}
				</div>
				<p class="mt-1 text-small text-muted">{p.id === 'github' ? 'Sign-in with GitHub, repository pickers and deployments on push.' : 'Sign-in with Discord and alerts to a Discord channel.'}</p>
				<div class="mt-4">
					{#if p.id === 'github'}
						<ProviderSetup provider="github" {publicUrl} bind:clientId={ghId} bind:secret={ghSecret} secretSet={v.github_secret_set} locked={!!v.locked.github_client_id} />
					{:else}
						<ProviderSetup provider="discord" {publicUrl} bind:clientId={dcId} bind:secret={dcSecret} secretSet={v.discord_secret_set} locked={!!v.locked.discord_client_id} />
					{/if}
				</div>
			</section>
		{/each}

		<div class="sticky bottom-4 flex justify-end xl:col-span-2"><button class="btn btn-primary shadow-overlay" disabled={saving}>{saving ? 'Saving…' : 'Save and apply'}</button></div>
	</form>
	<div class="mt-10 border-t border-rule-soft pt-8">
		<p class="eyebrow">AI operator</p>
		<h2 class="mt-1 text-section">AI assistant</h2>
		<p class="mt-1 max-w-3xl text-muted">The assistant behind Ask AI. It needs one provider; web research is an optional extra. Nothing here affects hosting.</p>
		<div class="mt-4"><AISettings /></div>
	</div>
{/if}
