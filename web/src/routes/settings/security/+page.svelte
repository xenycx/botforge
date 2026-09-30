<script lang="ts">
	import { onMount } from 'svelte';
	import { api, ApiError } from '$lib/api/client';
	import { fmtAgo, fmtWhen } from '$lib/args';
	import { session } from '$lib/session.svelte';
	import { confirmDialog } from '$lib/ui/dialogs.svelte';
	import { toast } from '$lib/ui/toast.svelte';
	import Notice from '$lib/components/ui/Notice.svelte';
	import Skeleton from '$lib/components/ui/Skeleton.svelte';
	import TwoStep from '$lib/components/TwoStep.svelte';

	type Sess = { id: string; device: string; created_at_ms: number; last_seen_at_ms: number; expires_at_ms: number; current: boolean };
	let sessions = $state<Sess[] | null>(null);
	let error = $state('');
	let current = $state('');
	let next = $state('');
	let confirm = $state('');
	let pwError = $state('');
	let saving = $state(false);

	async function load() {
		try {
			sessions = (await api<{ sessions: Sess[] }>('GET', '/me/sessions')).sessions;
		} catch (e) {
			error = e instanceof ApiError ? e.message : 'Sessions could not be loaded.';
		}
	}
	onMount(load);

	async function changePassword(e: SubmitEvent) {
		e.preventDefault();
		pwError = '';
		if (next.length < 12) return (pwError = 'Use at least 12 characters.');
		if (next !== confirm) return (pwError = 'The new passwords do not match.');
		saving = true;
		try {
			await api('POST', '/me/password', { current_password: current, new_password: next });
			const had = session.hasPassword;
			session.hasPassword = true;
			current = next = confirm = '';
			toast(had ? 'Password changed. Your other sessions were signed out.' : 'Password added. Your other sessions were signed out.');
			await load();
		} catch (err) {
			pwError = err instanceof ApiError ? err.message : 'The password could not be changed.';
		} finally {
			saving = false;
		}
	}

	async function revoke(s: Sess) {
		const ok = await confirmDialog({ title: `Sign out ${s.device || 'this session'}?`, body: 'That browser has to sign in again.', confirmLabel: 'Sign out session' });
		if (!ok) return;
		try {
			await api('DELETE', `/me/sessions/${s.id}`);
			toast('Session signed out');
			await load();
		} catch (e) {
			toast(e instanceof ApiError ? e.message : 'The session could not be signed out.', 'fail');
		}
	}
	async function revokeOthers() {
		const ok = await confirmDialog({ title: 'Sign out everywhere else?', body: 'Every other browser signed in to your account has to sign in again. This one stays signed in.', confirmLabel: 'Sign out other sessions' });
		if (!ok) return;
		try {
			const r = await api<{ revoked: number }>('POST', '/me/sessions/revoke-others');
			toast(`${r.revoked} session${r.revoked === 1 ? '' : 's'} signed out`);
			await load();
		} catch (e) {
			toast(e instanceof ApiError ? e.message : 'Sessions could not be signed out.', 'fail');
		}
	}
</script>

<svelte:head><title>Security · BotForge</title></svelte:head>

<section aria-labelledby="pw-h" class="max-w-xl">
	<h2 id="pw-h" class="text-section">{session.hasPassword ? 'Change password' : 'Add a password'}</h2>
	<p class="mt-1 text-muted">
		{#if session.hasPassword}Changing it signs out every other session. SFTP connections that use the password end too.{:else}You sign in with GitHub or Discord. A password lets you sign in without them and use SFTP with your password. For your protection this works within 10 minutes of signing in.{/if}
	</p>
	<form class="mt-4 grid gap-4" onsubmit={changePassword}>
		{#if session.hasPassword}
			<label class="block"><span class="label">Current password</span><input class="field" type="password" autocomplete="current-password" required bind:value={current} /></label>
		{/if}
		<label class="block"><span class="label">New password</span><input class="field" type="password" autocomplete="new-password" required minlength="12" bind:value={next} /><span class="help">At least 12 characters. A few unrelated words work well.</span></label>
		<label class="block"><span class="label">Repeat the new password</span><input class="field" type="password" autocomplete="new-password" required bind:value={confirm} /></label>
		{#if pwError}<Notice tone="fail" live>{pwError}</Notice>{/if}
		<div><button class="btn btn-primary" disabled={saving}>{session.hasPassword ? 'Change password' : 'Add password'}</button></div>
	</form>
</section>

{#if session.features.mfa}<div class="mt-12"><TwoStep /></div>{/if}

<section aria-labelledby="sess-h" class="mt-12">
	<div class="flex flex-wrap items-end justify-between gap-2">
		<div>
			<h2 id="sess-h" class="text-section">Signed-in sessions</h2>
			<p class="mt-1 text-muted">Browsers signed in to your account. Sign out any you do not recognize.</p>
		</div>
		{#if sessions && sessions.length > 1}<button class="btn" onclick={revokeOthers}>Sign out other sessions</button>{/if}
	</div>
	{#if error}<Notice tone="fail" class="mt-3">{error}</Notice>{/if}
	{#if sessions === null && !error}
		<div class="mt-3"><Skeleton rows={2} /></div>
	{:else if sessions}
		<ul class="mt-3 divide-y divide-rule-soft border-y border-rule-soft bg-panel">
			{#each sessions as s (s.id)}
				<li class="flex flex-wrap items-center gap-3 px-3 py-3">
					<div class="min-w-0 flex-1">
						<p class="font-medium">{s.device || 'Unknown device'}{#if s.current}<span class="ml-2 text-small font-normal text-run">This browser</span>{/if}</p>
						<p class="text-small text-muted">Signed in {fmtWhen(s.created_at_ms)}. Last active {fmtAgo(s.last_seen_at_ms)}. Expires {fmtWhen(s.expires_at_ms)}.</p>
					</div>
					{#if !s.current}<button class="btn btn-sm" onclick={() => revoke(s)}>Sign out</button>{/if}
				</li>
			{/each}
		</ul>
		<p class="mt-2 text-small text-muted">At most 20 sessions are kept; signing in again replaces the oldest.</p>
	{/if}
</section>
