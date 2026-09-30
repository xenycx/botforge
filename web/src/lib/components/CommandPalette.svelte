<script lang="ts">
	import { goto } from '$app/navigation';
	import { api } from '$lib/api/client';
	import type { Bot } from '$lib/api/types';
	import { describe } from '$lib/status';
	import { session } from '$lib/session.svelte';
	import Dialog from '$lib/components/ui/Dialog.svelte';
	import Icon from '$lib/components/ui/Icon.svelte';

	// Jump to any bot or page: Ctrl+K (Cmd+K) anywhere, or the header button.
	let { open = $bindable(false) }: { open?: boolean } = $props();

	let q = $state('');
	let bots = $state<Bot[]>([]);
	// Enter pressed before the bot list arrived waits for it, so fast typing
	// still lands on the bot rather than on nothing.
	let loading: Promise<void> = Promise.resolve();
	let active = $state(0);
	let list: HTMLUListElement | undefined = $state();

	type Item = { key: string; label: string; hint: string; href: string; icon: 'box' | 'plus' | 'activity' | 'gear' | 'shield' | 'key' | 'link' };
	const pages = $derived<Item[]>([
		{ key: 'p-new', label: 'New bot', hint: 'Create a bot', href: '/bots/new', icon: 'plus' },
		{ key: 'p-bots', label: 'Bots', hint: 'All bots', href: '/dashboard', icon: 'box' },
		{ key: 'p-activity', label: 'Activity', hint: 'Work and changes', href: '/activity', icon: 'activity' },
		{ key: 'p-security', label: 'Security', hint: 'Settings', href: '/settings/security', icon: 'shield' },
		{ key: 'p-accounts', label: 'Connected accounts', hint: 'Settings', href: '/settings/connected-accounts', icon: 'link' },
		{ key: 'p-sftp', label: 'SFTP and API keys', hint: 'Settings', href: '/settings/sftp', icon: 'key' },
		...(session.user?.role === 'admin'
			? [
					{ key: 'p-users', label: 'Users', hint: 'Administration', href: '/admin/users', icon: 'gear' as const },
					{ key: 'p-diag', label: 'Diagnostics', hint: 'Administration', href: '/admin/diagnostics', icon: 'shield' as const }
				]
			: [])
	]);

	$effect(() => {
		if (!open) return;
		q = '';
		active = 0;
		loading = api<{ bots: Bot[] }>('GET', '/bots')
			.then((r) => {
				bots = r.bots;
			})
			.catch(() => {});
	});

	// Simple subsequence match, best when the letters appear early and together.
	function score(text: string, needle: string): number {
		if (!needle) return 1;
		const t = text.toLowerCase();
		const i = t.indexOf(needle);
		if (i >= 0) return 100 - i;
		let pos = 0;
		for (const ch of needle) {
			pos = t.indexOf(ch, pos);
			if (pos < 0) return 0;
			pos++;
		}
		return 10;
	}
	const items = $derived.by(() => {
		const needle = q.trim().toLowerCase();
		const botItems: (Item & { s: number })[] = bots.map((b) => ({
			key: b.id,
			label: b.name,
			hint: `${describe(b).label}, ${b.runtime}${b.tags.length ? `, ${b.tags.join(' ')}` : ''}`,
			href: `/bots/${b.id}`,
			icon: 'box' as const,
			s: score(b.name + ' ' + b.tags.join(' '), needle) + (b.favorite ? 5 : 0)
		}));
		const pageItems = pages.map((p) => ({ ...p, s: score(p.label, needle) - 1 }));
		return [...botItems, ...pageItems].filter((x) => x.s > 0).sort((a, b) => b.s - a.s).slice(0, 12);
	});

	function choose(i: number) {
		const it = items[i];
		if (!it) return;
		open = false;
		goto(it.href);
	}
	function onkey(e: KeyboardEvent) {
		if (e.key === 'ArrowDown') {
			e.preventDefault();
			active = Math.min(items.length - 1, active + 1);
		} else if (e.key === 'ArrowUp') {
			e.preventDefault();
			active = Math.max(0, active - 1);
		} else if (e.key === 'Enter') {
			e.preventDefault();
			loading.then(() => choose(active));
		}
		queueMicrotask(() => list?.querySelector(`[data-i="${active}"]`)?.scrollIntoView({ block: 'nearest' }));
	}
	function global(e: KeyboardEvent) {
		if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 'k' && session.user) {
			e.preventDefault();
			open = !open;
		}
	}
</script>

<svelte:window onkeydown={global} />

<Dialog bind:open title="Go to" size="md">
	<label class="relative block">
		<span class="sr-only">Search bots and pages</span>
		<Icon name="search" class="pointer-events-none absolute top-1/2 left-2.5 -translate-y-1/2 text-muted" />
		<input
			class="field pl-8"
			placeholder="Bot name, tag or page"
			bind:value={q}
			oninput={() => (active = 0)}
			onkeydown={onkey}
			role="combobox"
			aria-expanded="true"
			aria-controls="palette-list"
			aria-activedescendant={items[active] ? `pal-${items[active].key}` : undefined}
			autocomplete="off"
			data-autofocus
		/>
	</label>
	<ul bind:this={list} id="palette-list" role="listbox" aria-label="Results" class="mt-2 max-h-80 overflow-y-auto pb-2">
		{#each items as it, i (it.key)}
			<li
				id="pal-{it.key}"
				data-i={i}
				role="option"
				aria-selected={i === active}
				class="flex cursor-pointer items-center gap-3 rounded-control px-2 py-2 {i === active ? 'bg-paper' : ''}"
				onmousemove={() => (active = i)}
				onclick={() => choose(i)}
				onkeydown={() => {}}
			>
				<Icon name={it.icon} class="text-muted" />
				<span class="min-w-0 flex-1"><span class="block truncate font-medium">{it.label}</span><span class="block truncate text-small text-muted">{it.hint}</span></span>
				{#if i === active}<kbd class="text-small text-muted">Enter</kbd>{/if}
			</li>
		{:else}
			<li class="px-2 py-3 text-muted">Nothing matches “{q}”.</li>
		{/each}
	</ul>
</Dialog>
