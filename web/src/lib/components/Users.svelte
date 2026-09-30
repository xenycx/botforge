<script lang="ts">
	import { onMount } from 'svelte';
	import { api, ApiError } from '$lib/api/client';
	import { Perm, type SubUser } from '$lib/api/types';
	import { confirmDialog } from '$lib/ui/dialogs.svelte';
	import { toast } from '$lib/ui/toast.svelte';
	import Notice from '$lib/components/ui/Notice.svelte';
	import ChangeList from '$lib/components/ChangeList.svelte';
	import type { AuditEvent, AuditPage } from '$lib/audit';
	import { confirmWithOption } from '$lib/ui/dialogs.svelte';
	import { goto } from '$app/navigation';
	import { session } from '$lib/session.svelte';

	let { botId, botName }: { botId: string; botName: string } = $props();

	const options = [
		{ bit: Perm.console, label: 'View console', hint: 'Live output, analytics and resource usage' },
		{ bit: Perm.power, label: 'Start / stop', hint: 'Start, stop, restart, kill and send console input' },
		{ bit: Perm.files, label: 'Edit files', hint: 'File manager, SFTP, packages, deployments and backups' },
		{ bit: Perm.env, label: 'Manage env vars', hint: 'Read and change environment variables, including secrets' },
		{ bit: Perm.admin, label: 'Full admin', hint: 'Everything above plus settings, startup, restore and delete backups' }
	];

	let users = $state<SubUser[]>([]);
	let email = $state('');
	let perms = $state<number>(Perm.console);
	let error = $state('');
	const path = `/bots/${botId}/users`;
	const msg = (e: unknown) => (e instanceof ApiError ? e.message : 'The request failed.');

	async function load() {
		users = (await api<{ users: SubUser[] }>('GET', path)).users;
	}
	let changes = $state<AuditEvent[] | null>(null);
	let transferTo = $state('');
	onMount(() => {
		load().catch((e) => (error = msg(e)));
		api<AuditPage>('GET', `/bots/${botId}/changes?limit=8`).then((r) => (changes = r.events)).catch(() => (changes = []));
	});

	async function transfer(e: SubmitEvent) {
		e.preventDefault();
		const r = await confirmWithOption({
			title: `Transfer ${botName} to ${transferTo}?`,
			body: 'They become the owner: they can delete the bot, share it and publish ports. A linked GitHub repository is unlinked, because it uses your GitHub access; the new owner links it again with theirs.',
			confirmLabel: 'Transfer ownership',
			tone: 'danger',
			checkbox: { label: 'Keep full access for me', checked: true }
		});
		if (!r) return;
		try {
			const res = await api<{ repository_unlinked: boolean }>('POST', `/bots/${botId}/transfer`, { email: transferTo, keep_access: r.checked });
			toast(`${botName} now belongs to ${transferTo}${res.repository_unlinked ? '. The repository link was removed.' : ''}`);
			if (!r.checked && session.user?.role !== 'admin') await goto('/dashboard');
			else location.reload();
		} catch (err) {
			error = msg(err);
		}
	}

	function toggle(mask: number, bit: number): number {
		if (bit === Perm.admin) return mask & Perm.admin ? mask & ~Perm.admin : 31;
		const next = mask ^ bit;
		return next & Perm.admin ? next & ~Perm.admin : next;
	}

	async function save(em: string, p: number, added = false) {
		error = '';
		try {
			await api('PUT', path, { email: em, permissions: p });
			await load();
			toast(added ? `Shared with ${em}` : `Updated access for ${em}`);
		} catch (e) {
			error = msg(e);
		}
	}
	async function add(e: SubmitEvent) {
		e.preventDefault();
		await save(email, perms, true);
		if (!error) email = '';
	}
	// Invitation links: shared by the owner, accepted once by whoever signs in.
	type Invite = { id: string; permissions: number; created_by: string; created_at_ms: number; expires_at_ms: number };
	let invites = $state<Invite[]>([]);
	let invPerms = $state<number>(Perm.console);
	let invDays = $state(3);
	let invLink = $state('');
	async function loadInvites() {
		invites = (await api<{ invites: Invite[] }>('GET', `/bots/${botId}/invites`)).invites;
	}
	onMount(() => {
		loadInvites().catch(() => {});
	});
	async function createInvite(e: SubmitEvent) {
		e.preventDefault();
		error = '';
		try {
			const r = await api<{ path: string }>('POST', `/bots/${botId}/invites`, { permissions: invPerms, expires_in_days: invDays });
			invLink = location.origin + r.path;
			await loadInvites();
		} catch (err) {
			error = msg(err);
		}
	}
	async function revokeInvite(v: Invite) {
		try {
			await api('DELETE', `/bots/${botId}/invites/${v.id}`);
			toast('Invitation revoked');
			await loadInvites();
		} catch (err) {
			toast(msg(err), 'fail');
		}
	}
	const permNames = (m: number) => (m & Perm.admin ? 'Full admin' : options.filter((o) => m & o.bit).map((o) => o.label).join(', '));

	async function remove(u: SubUser) {
		const ok = await confirmDialog({ title: `Remove ${u.email}?`, body: 'They lose access to this bot immediately, including open consoles and SFTP.', confirmLabel: 'Remove access', tone: 'danger' });
		if (!ok) return;
		error = '';
		try {
			await api('DELETE', `${path}/${u.user_id}`);
			await load();
			toast(`Removed ${u.email}`);
		} catch (e) {
			error = msg(e);
		}
	}
</script>

<p class="mb-4 max-w-prose text-muted">Give other people with an account on this panel access to this bot. The owner and administrators always have full access. Anyone who can change files or environment variables can make the bot reveal its secrets, so share those only with people you trust.</p>

<ul class="divide-y divide-rule-soft border-y border-rule-soft">
	{#each users as u (u.user_id)}
		<li class="grid gap-2 bg-panel px-3 py-3 sm:grid-cols-[minmax(0,1fr)_auto] sm:items-center">
			<div class="min-w-0">
				<div class="font-medium">{u.email}</div>
				<div class="mt-1 flex flex-wrap gap-x-4 gap-y-1">
					{#each options as o (o.bit)}
						<label class="flex items-center gap-1.5" title={o.hint}>
							<input type="checkbox" checked={(u.permissions & o.bit) !== 0} onchange={() => save(u.email, toggle(u.permissions, o.bit))} />{o.label}
						</label>
					{/each}
				</div>
			</div>
			<button class="btn btn-sm btn-danger" onclick={() => remove(u)}>Remove access</button>
		</li>
	{:else}
		<li class="py-4 text-muted">This bot is not shared with anyone.</li>
	{/each}
</ul>

<form class="mt-8 max-w-2xl" onsubmit={add}>
	<h3 class="text-title font-semibold">Share with someone</h3>
	<label class="mt-2 block"><span class="label">Email of a panel user</span><input class="field" type="email" required bind:value={email} placeholder="friend@example.com" /></label>
	<div class="mt-3 grid gap-3 sm:grid-cols-2">
		{#each options as o (o.bit)}
			<label class="flex items-start gap-2"><input type="checkbox" class="mt-0.5" checked={(perms & o.bit) !== 0} onchange={() => (perms = toggle(perms, o.bit))} /><span>{o.label}<span class="help">{o.hint}</span></span></label>
		{/each}
	</div>
	{#if error}<Notice tone="fail" class="mt-3" live>{error}</Notice>{/if}
	<button class="btn btn-primary mt-4" disabled={perms === 0}>Share bot</button>
</form>

<section class="mt-12 max-w-2xl" aria-labelledby="inv-h">
	<h3 id="inv-h" class="text-title font-semibold">Invite with a link</h3>
	<p class="mt-1 text-muted">For someone who has an account here but whose email you do not know. The link works once, for the first person who opens it and accepts.</p>
	<form class="mt-3 grid gap-3" onsubmit={createInvite}>
		<div class="grid gap-3 sm:grid-cols-2">
			{#each options as o (o.bit)}
				<label class="flex items-start gap-2"><input type="checkbox" class="mt-0.5" checked={(invPerms & o.bit) !== 0} onchange={() => (invPerms = toggle(invPerms, o.bit))} /><span>{o.label}</span></label>
			{/each}
		</div>
		<div class="flex flex-wrap items-end gap-2">
			<label class="block"><span class="label">Expires after</span>
				<select class="field w-auto" bind:value={invDays}>{#each [1, 3, 7, 14] as d (d)}<option value={d}>{d} day{d === 1 ? '' : 's'}</option>{/each}</select>
			</label>
			<button class="btn" disabled={invPerms === 0}>Create invitation link</button>
		</div>
	</form>
	{#if invLink}
		<Notice tone="warn" class="mt-3" title="Copy the link now">
			It is shown only once. Anyone who has it can accept it.<br /><code class="break-all">{invLink}</code>
			{#snippet action()}<button class="btn btn-sm" onclick={() => navigator.clipboard?.writeText(invLink).then(() => toast('Link copied', 'success'))}>Copy</button>{/snippet}
		</Notice>
	{/if}
	{#if invites.length}
		<ul class="mt-3 divide-y divide-rule-soft border-y border-rule-soft">
			{#each invites as v (v.id)}
				<li class="flex flex-wrap items-center gap-3 py-2">
					<div class="min-w-0 flex-1">
						<p>{permNames(v.permissions)}</p>
						<p class="text-small text-muted">Created by {v.created_by} · expires {new Date(v.expires_at_ms).toLocaleString()}</p>
					</div>
					<button class="btn btn-sm btn-danger" onclick={() => revokeInvite(v)}>Revoke</button>
				</li>
			{/each}
		</ul>
	{/if}
</section>

<section class="mt-12 max-w-2xl" aria-labelledby="hist-h">
	<div class="flex items-baseline justify-between gap-2">
		<h3 id="hist-h" class="text-title font-semibold">Recent changes</h3>
		<a class="link text-small" href="/activity?bot={botId}&view=changes">Full history</a>
	</div>
	{#if changes === null}<p class="mt-2 text-muted">Loading…</p>{:else if changes.length}<div class="mt-2"><ChangeList events={changes} /></div>{:else}<p class="mt-2 text-muted">Nothing recorded yet.</p>{/if}
</section>

<section class="mt-12 max-w-2xl border-t border-rule-soft pt-5" aria-labelledby="tr-h">
	<h3 id="tr-h" class="text-title font-semibold">Transfer ownership</h3>
	<p class="mt-1 text-muted">Give this bot to another account on the panel, for example when someone leaves the team.</p>
	<form class="mt-3 flex flex-wrap items-end gap-2" onsubmit={transfer}>
		<label class="block min-w-0 flex-1 basis-64"><span class="label">Email of the new owner</span><input class="field" type="email" required bind:value={transferTo} /></label>
		<button class="btn btn-danger">Transfer…</button>
	</form>
</section>
