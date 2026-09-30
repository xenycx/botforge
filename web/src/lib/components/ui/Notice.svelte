<script lang="ts">
	import type { Snippet } from 'svelte';
	import Icon from './Icon.svelte';

	// Section-level message for recoverable problems, permissions or context.
	let {
		tone = 'info',
		title,
		children,
		action,
		class: cls = '',
		live = false
	}: { tone?: 'info' | 'warn' | 'fail' | 'success'; title?: string; children?: Snippet; action?: Snippet; class?: string; live?: boolean } = $props();

	const s = {
		info: { bar: 'border-action', icon: 'info', color: 'text-action' },
		warn: { bar: 'border-warn', icon: 'alert', color: 'text-warn' },
		fail: { bar: 'border-fail', icon: 'alert', color: 'text-fail' },
		success: { bar: 'border-run', icon: 'check', color: 'text-run' }
	} as const;
</script>

<div
	class="flex flex-wrap items-start gap-x-3 gap-y-2 border-l-[3px] bg-panel px-3.5 py-2.5 {s[tone].bar} {cls}"
	role={live ? (tone === 'fail' ? 'alert' : 'status') : undefined}
>
	<Icon name={s[tone].icon} class="mt-[3px] {s[tone].color}" />
	<div class="min-w-0 flex-1 basis-60">
		{#if title}<p class="font-semibold">{title}</p>{/if}
		{#if children}<div class="{title ? 'mt-0.5 text-ink/85' : ''} [&_a]:link">{@render children()}</div>{/if}
	</div>
	{#if action}<div class="flex shrink-0 gap-2">{@render action()}</div>{/if}
</div>
