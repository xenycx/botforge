<script lang="ts" module>
	export type MenuItem = { label: string; onselect: () => void; danger?: boolean; disabled?: boolean; hint?: string } | 'separator';
</script>

<script lang="ts">
	import type { Snippet } from 'svelte';
	import Icon, { type IconName } from './Icon.svelte';

	// Compact action menu for rows and narrow layouts. Keyboard: Enter/Space or
	// ArrowDown opens, arrows move, Escape closes and returns focus.
	let {
		items,
		label,
		icon = 'more',
		text = '',
		align = 'end',
		class: cls = '',
		trigger
	}: { items: MenuItem[]; label: string; icon?: IconName; text?: string; align?: 'start' | 'end'; class?: string; trigger?: Snippet } = $props();

	let open = $state(false);
	let btn: HTMLButtonElement | undefined = $state();
	let list: HTMLDivElement | undefined = $state();
	const uid = Math.random().toString(36).slice(2, 8);

	function focusItem(delta: number | 'first' | 'last') {
		const els = [...(list?.querySelectorAll<HTMLButtonElement>('[role=menuitem]:not(:disabled)') ?? [])];
		if (!els.length) return;
		const i = els.indexOf(document.activeElement as HTMLButtonElement);
		const n = delta === 'first' ? 0 : delta === 'last' ? els.length - 1 : (i + delta + els.length) % els.length;
		els[n].focus();
	}
	async function show() {
		open = true;
		await Promise.resolve();
		focusItem('first');
	}
	function hide(refocus = true) {
		open = false;
		if (refocus) btn?.focus();
	}
	function onkey(e: KeyboardEvent) {
		if (e.key === 'Escape') {
			e.preventDefault();
			hide();
		} else if (e.key === 'ArrowDown') {
			e.preventDefault();
			focusItem(1);
		} else if (e.key === 'ArrowUp') {
			e.preventDefault();
			focusItem(-1);
		} else if (e.key === 'Home') {
			e.preventDefault();
			focusItem('first');
		} else if (e.key === 'End') {
			e.preventDefault();
			focusItem('last');
		} else if (e.key === 'Tab') hide(false);
	}
	function outside(e: PointerEvent) {
		if (open && !btn?.contains(e.target as Node) && !list?.contains(e.target as Node)) hide(false);
	}
</script>

<svelte:window onpointerdown={outside} />

<div class="relative inline-block {cls}">
	<button
		bind:this={btn}
		class={trigger ? 'flex items-center gap-1.5 rounded-full p-0.5 pr-1.5 hover:bg-paper-2/60' : `btn ${text ? '' : 'btn-icon'} btn-quiet`}
		aria-haspopup="menu"
		aria-expanded={open}
		aria-controls="menu-{uid}"
		aria-label={text ? undefined : label}
		onclick={() => (open ? hide() : show())}
		onkeydown={(e) => {
			if (e.key === 'ArrowDown' && !open) {
				e.preventDefault();
				show();
			}
		}}
	>
		{#if trigger}{@render trigger()}{:else}<Icon name={icon} />{#if text}<span>{text}</span>{/if}{/if}
	</button>
	{#if open}
		<div
			bind:this={list}
			id="menu-{uid}"
			role="menu"
			tabindex="-1"
			aria-label={label}
			class="absolute top-full z-40 mt-1 min-w-48 animate-enter rounded-overlay border border-rule-soft bg-raised py-1 shadow-overlay {align === 'end' ? 'right-0' : 'left-0'}"
			onkeydown={onkey}
		>
			{#each items as it, i (i)}
				{#if it === 'separator'}
					<div class="my-1 border-t border-rule-soft" role="separator"></div>
				{:else}
					<button
						role="menuitem"
						class="flex w-full flex-col items-start px-3 py-2 text-left hover:bg-paper focus:bg-paper focus:outline-none disabled:opacity-45 {it.danger ? 'text-fail' : ''}"
						disabled={it.disabled}
						onclick={() => {
							hide();
							it.onselect();
						}}
					>
						<span>{it.label}</span>
						{#if it.hint}<span class="text-small text-muted">{it.hint}</span>{/if}
					</button>
				{/if}
			{/each}
		</div>
	{/if}
</div>
