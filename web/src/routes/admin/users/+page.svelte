<script lang="ts">
	import { onMount } from 'svelte';
	import { api, ApiError } from '$lib/api/client';
	import type { User } from '$lib/api/types';
	import { fmtWhen } from '$lib/args';
	import { session } from '$lib/session.svelte';
	import { confirmDialog, confirmWithOption } from '$lib/ui/dialogs.svelte';
	import { toast } from '$lib/ui/toast.svelte';
	import Dialog from '$lib/components/ui/Dialog.svelte';
	import Icon from '$lib/components/ui/Icon.svelte';
	import Menu, { type MenuItem } from '$lib/components/ui/Menu.svelte';
	import Notice from '$lib/components/ui/Notice.svelte';
	import Skeleton from '$lib/components/ui/Skeleton.svelte';

	type AdminUser = User & { bots: number; has_password: boolean };
	let users = $state<AdminUser[] | null>(null);
	let error = $state('');
	let q = $state('');
	let addOpen = $state(false);
	let inviteOpen = $state(false);
	let inviteForm = $state({ email: '', role: 'user', expires_in_days: 7 });
	let inviteLink = $state('');
	let form = $state({ email: '', password: '', role: 'user' });
	let addError = $state('');
	let busy = $state(false);

	const msg = (e: unknown) => (e instanceof ApiError ? e.message : 'The request failed.');
	async function load() {
		try {
			users = (await api<{ users: AdminUser[] }>('GET', '/users')).users;
			error = '';
		} catch (e) {
			error = msg(e);
		}
	}
	onMount(load);

	const shown = $derived((users ?? []).filter((u) => !q || u.email.toLowerCase().includes(q.toLowerCase())));
	const admins = $derived((users ?? []).filter((u) => u.role === 'admin' && !u.disabled).length);

	async function add(e: SubmitEvent) {
		e.preventDefault();
		addError = '';
		busy = true;
		try {
			await api('POST', '/users', form);
			toast(`Created ${form.email}`);
			form = { email: '', password: '', role: 'user' };
			addOpen = false;
			await load();
		} catch (err) {
			addError = msg(err);
		} finally {
			busy = false;
		}
	}
	async function invite(e: SubmitEvent) {
		e.preventDefault(); addError = ''; busy = true;
		try {
			const r = await api<{ path: string }>('POST', '/admin/account-invites', inviteForm);
			inviteLink = location.origin + r.path; toast('Invitation created', 'success');
		} catch (err) { addError = msg(err); } finally { busy = false; }
	}
	async function copyInvite() { await navigator.clipboard.writeText(inviteLink); toast('Invitation link copied'); }
	async function patch(u: AdminUser, body: Record<string, unknown>, done: string) {
		try {
			await api('PATCH', `/users/${u.id}`, body);
			toast(done);
			await load();
		} catch (e) {
			toast(msg(e), 'fail');
		}
	}
	async function disable(u: AdminUser) {
		const r = await confirmWithOption({
			title: `Disable ${u.email}?`,
			body: 'They are signed out everywhere at once: browser sessions, consoles and SFTP. Their bots stay as they are unless you stop them here.',
			confirmLabel: 'Disable account',
			tone: 'danger',
			checkbox: u.bots ? { label: `Also stop the ${u.bots} bot${u.bots === 1 ? '' : 's'} they own`, checked: false } : undefined
		});
		if (!r) return;
		await patch(u, { disabled: true, stop_bots: r.checked }, `Disabled ${u.email}`);
	}
	async function setRole(u: AdminUser, role: 'admin' | 'user') {
		const ok = await confirmDialog({
			title: role === 'admin' ? `Make ${u.email} an administrator?` : `Remove administrator rights from ${u.email}?`,
			body: role === 'admin' ? 'Administrators can see and change every bot and every account on this panel.' : 'They keep their own bots and shared access.',
			confirmLabel: role === 'admin' ? 'Make administrator' : 'Remove rights',
			tone: role === 'admin' ? 'primary' : 'danger'
		});
		if (ok) await patch(u, { role }, role === 'admin' ? `${u.email} is now an administrator` : `${u.email} is now a regular user`);
	}
	function menu(u: AdminUser): MenuItem[] {
		const self = u.id === session.user?.id;
		const lastAdmin = u.role === 'admin' && !u.disabled && admins <= 1;
		return [
			u.role === 'admin'
				? { label: 'Remove administrator rights', disabled: lastAdmin, hint: lastAdmin ? 'The only administrator' : undefined, onselect: () => setRole(u, 'user') }
				: { label: 'Make administrator', onselect: () => setRole(u, 'admin') },
			'separator',
			u.disabled
				? { label: 'Enable account', onselect: () => patch(u, { disabled: false }, `Enabled ${u.email}`) }
				: { label: 'Disable account', danger: true, disabled: self || lastAdmin, hint: self ? 'You cannot disable yourself' : undefined, onselect: () => disable(u) }
		];
	}
</script>

<svelte:head><title>Users · BotForge</title></svelte:head>
<div class="flex flex-wrap items-end justify-between gap-3">
	<div>
		<h2 class="text-section">Users</h2>
		<p class="mt-1 text-muted">{users?.length ?? '…'} account{users?.length === 1 ? '' : 's'}, {admins} administrator{admins === 1 ? '' : 's'}.</p>
	</div>
	<div class="flex gap-2"><button class="btn" onclick={() => { inviteOpen = true; inviteLink = ''; addError = ''; }}><Icon name="link" />Invite user</button><button class="btn btn-primary" onclick={() => (addOpen = true)}><Icon name="plus" />Add user</button></div>
</div>

{#if (users?.length ?? 0) > 8}
	<label class="relative mt-4 block max-w-sm">
		<span class="sr-only">Search users</span>
		<Icon name="search" class="pointer-events-none absolute top-1/2 left-2.5 -translate-y-1/2 text-muted" />
		<input class="field pl-8" type="search" placeholder="Search by email" bind:value={q} />
	</label>
{/if}
{#if error}<Notice tone="fail" class="mt-4">{error}</Notice>{/if}

<div class="mt-4">
	{#if users === null && !error}
		<Skeleton rows={3} />
	{:else}
		<ul class="border-y border-rule-soft bg-panel [&>li+li]:border-t [&>li+li]:border-rule-soft">
			{#each shown as u (u.id)}
				<li class="flex flex-wrap items-center gap-x-4 gap-y-1 px-3 py-3 {u.disabled ? 'opacity-70' : ''}">
					<div class="min-w-0 flex-1 basis-60">
						<p class="font-medium break-all">{u.email}{#if u.id === session.user?.id}<span class="ml-2 text-small font-normal text-muted">You</span>{/if}</p>
						<p class="text-small text-muted">
							{u.role === 'admin' ? 'Administrator' : 'User'}{u.disabled ? ', disabled' : ''}. {u.bots} bot{u.bots === 1 ? '' : 's'}. {u.has_password ? 'Password' : 'Provider sign-in only'}. Joined {fmtWhen(u.created_at_ms)}.
						</p>
					</div>
					<Menu label="Actions for {u.email}" items={menu(u)} />
				</li>
			{/each}
		</ul>
	{/if}
</div>
<p class="mt-3 max-w-prose text-small text-muted">A locked-out administrator can set a new password on the host with <code>botpanel reset-password EMAIL</code>.</p>

<Dialog bind:open={addOpen} title="Add a user" size="sm">
	<form id="user-add" class="grid gap-4" onsubmit={add}>
		<label class="block"><span class="label">Email</span><input class="field" type="email" required bind:value={form.email} autocomplete="off" /></label>
		<label class="block">
			<span class="label">Initial password</span>
			<input class="field" type="password" required minlength="12" autocomplete="new-password" bind:value={form.password} />
			<span class="help">At least 12 characters. They can change it under Settings.</span>
		</label>
		<label class="block">
			<span class="label">Role</span>
			<select class="field" bind:value={form.role}><option value="user">User</option><option value="admin">Administrator</option></select>
		</label>
		{#if addError}<p class="text-small text-fail" role="alert">{addError}</p>{/if}
	</form>
	{#snippet footer()}
		<button class="btn" onclick={() => (addOpen = false)}>Cancel</button>
		<button class="btn btn-primary" type="submit" form="user-add" disabled={busy}>Add user</button>
	{/snippet}
</Dialog>

<Dialog bind:open={inviteOpen} title="Invite a user" size="sm">
	{#if inviteLink}
		<p class="text-muted">This link is shown once. Send it to the person you want to invite.</p>
		<div class="mt-3 flex gap-2"><input class="field font-mono text-small" readonly value={inviteLink} /><button class="btn" onclick={copyInvite}>Copy</button></div>
	{:else}
		<form id="user-invite" class="grid gap-4" onsubmit={invite}>
			<label><span class="label">Email (optional)</span><input class="field" type="email" bind:value={inviteForm.email} placeholder="Locks the link to this address" /></label>
			<label><span class="label">Role</span><select class="field" bind:value={inviteForm.role}><option value="user">User</option><option value="admin">Administrator</option></select></label>
			<label><span class="label">Expires after</span><select class="field" bind:value={inviteForm.expires_in_days}><option value={1}>1 day</option><option value={3}>3 days</option><option value={7}>7 days</option><option value={14}>14 days</option></select></label>
			{#if addError}<p class="text-small text-fail">{addError}</p>{/if}
		</form>
	{/if}
	{#snippet footer()}<button class="btn" onclick={() => (inviteOpen = false)}>{inviteLink ? 'Done' : 'Cancel'}</button>{#if !inviteLink}<button class="btn btn-primary" type="submit" form="user-invite" disabled={busy}>Create invitation</button>{/if}{/snippet}
</Dialog>
