<script lang="ts">
	import { onMount } from 'svelte';
	import { api, ApiError, fmtBytes } from '$lib/api/client';
	import type { Site, SitesInfo } from '$lib/api/types';
	import { fmtAgo } from '$lib/args';
	import { confirmDialog } from '$lib/ui/dialogs.svelte';
	import { toast } from '$lib/ui/toast.svelte';
	import Icon from '$lib/components/ui/Icon.svelte';
	import Notice from '$lib/components/ui/Notice.svelte';
	import Skeleton from '$lib/components/ui/Skeleton.svelte';

	let info = $state<SitesInfo | null>(null);
	let sites = $state<Site[] | null>(null);
	let error = $state('');
	let q = $state('');
	let served = $state<'all' | 'live' | 'suspended'>('all');

	async function load() {
		try {
			info = await api<SitesInfo>('GET', '/sites-info');
			if (info.enabled) sites = (await api<{ sites: Site[] }>('GET', '/admin/sites')).sites;
		} catch (e) {
			error = e instanceof ApiError ? e.message : 'Sites could not be loaded.';
		}
	}
	onMount(load);
	const shown = $derived(
		(sites ?? []).filter(
			(s) =>
				(served === 'all' || (served === 'suspended') === s.disabled) &&
				(!q.trim() || `${s.name} ${s.slug} ${s.owner_email} ${s.workspace_name}`.toLowerCase().includes(q.trim().toLowerCase()))
		)
	);
	const total = $derived((sites ?? []).reduce((n, s) => n + s.release_bytes, 0));
	const domains = $derived((sites ?? []).reduce((n, s) => n + s.domains, 0));
	const suspended = $derived((sites ?? []).filter((s) => s.disabled).length);

	async function toggle(s: Site) {
		if (!s.disabled) {
			const ok = await confirmDialog({ title: `Suspend ${s.name}?`, body: 'Visitors get an “unavailable” page on every address of the site until you restore it. Its members can still manage it.', confirmLabel: 'Suspend site', tone: 'danger' });
			if (!ok) return;
		}
		try {
			await api('PATCH', `/admin/sites/${s.id}`, { disabled: !s.disabled });
			toast(s.disabled ? `Restored ${s.name}` : `Suspended ${s.name}`);
			await load();
		} catch (e) {
			toast(e instanceof ApiError ? e.message : 'The site could not be changed.', 'fail');
		}
	}
</script>

<svelte:head><title>Sites · Administration · BotForge</title></svelte:head>

<h2 class="text-section">Sites and domains</h2>
<p class="mt-1 max-w-3xl text-muted">Every hosted site on this panel. Suspending a site stops it from being served on all its addresses without deleting anything.</p>
{#if error}<Notice tone="fail" class="mt-4">{error}</Notice>{/if}

{#if info && !info.enabled}
	<Notice class="mt-4" title="Site hosting is off">
		Set <code>BOTPANEL_SITES_LISTEN</code> (for example <code>127.0.0.1:8081</code>) and <code>BOTPANEL_SITES_BASE_URL</code> (for example <code>https://sites.example.com</code>), route <code>*.sites.example.com</code> and custom domains to that listener in your reverse proxy, then restart. See <a href="/docs#sites">the hosting guide</a>.
	</Notice>
{:else if sites === null && !error}
	<div class="mt-4"><Skeleton rows={3} /></div>
{:else if sites}
	<dl class="mt-5 grid grid-cols-2 gap-3 md:grid-cols-4">
		{#each [['Sites', `${sites.length}`, `under ${info?.domain ?? ''}`], ['Served', fmtBytes(total), 'current releases'], ['Custom domains', `${domains}`, 'across all sites'], ['Suspended', `${suspended}`, suspended ? 'not being served' : 'none']] as [k, v, h] (k)}
			<div class="stat"><dt class="eyebrow">{k}</dt><dd class="mt-1 font-mono text-title font-semibold">{v}</dd><dd class="truncate text-small text-muted" title={h}>{h}</dd></div>
		{/each}
	</dl>

	<div class="mt-5 flex flex-wrap items-center gap-2">
		<label class="relative min-w-0 flex-1 basis-60">
			<span class="sr-only">Search sites</span>
			<Icon name="search" class="pointer-events-none absolute top-1/2 left-2.5 -translate-y-1/2 text-muted" />
			<input class="field pl-8" type="search" placeholder="Search by name, address, owner or workspace" bind:value={q} />
		</label>
		<select class="field w-auto" bind:value={served} aria-label="State"><option value="all">All sites</option><option value="live">Being served</option><option value="suspended">Suspended</option></select>
	</div>
	<div class="card mt-3 overflow-x-auto">
		<table class="w-full min-w-[46rem] text-left">
			<thead class="border-b border-rule-soft">
				<tr class="[&>th]:px-4 [&>th]:py-2.5 [&>th]:font-normal">
					<th class="eyebrow">Site</th><th class="eyebrow">Workspace</th><th class="eyebrow">Created by</th><th class="eyebrow text-right">Size</th><th class="eyebrow text-right">Domains</th><th class="eyebrow text-right">Updated</th><th></th>
				</tr>
			</thead>
			<tbody class="divide-y divide-rule-soft">
				{#each shown as s (s.id)}
					<tr class="[&>td]:px-4 [&>td]:py-2.5 hover:bg-paper/40">
						<td class="max-w-64">
							<a class="block truncate font-medium hover:underline" href="/sites/{s.id}">{s.name}</a>
							<a class="block truncate font-mono text-[12px] text-action hover:underline" href={s.url} target="_blank" rel="noopener">{s.slug}</a>
						</td>
						<td class="max-w-40 truncate text-small"><a class="hover:underline" href="/admin/workspaces/{s.workspace_id}">{s.workspace_name}</a></td>
						<td class="max-w-48 truncate text-small"><a class="hover:underline" href="/admin/users/{s.owner_id}">{s.owner_email}</a></td>
						<td class="text-right font-mono text-small">{s.current_release ? fmtBytes(s.release_bytes) : '—'}</td>
						<td class="text-right font-mono text-small">{s.domains}</td>
						<td class="text-right text-small text-muted">{fmtAgo(s.updated_at_ms)}</td>
						<td class="text-right"><button class="btn btn-sm {s.disabled ? '' : 'btn-danger'}" onclick={() => toggle(s)}>{s.disabled ? 'Restore' : 'Suspend'}</button></td>
					</tr>
				{:else}
					<tr><td colspan="7" class="px-4 py-4 text-small text-muted">No sites{q || served !== 'all' ? ' match' : ' yet'}.</td></tr>
				{/each}
			</tbody>
		</table>
	</div>
{/if}
