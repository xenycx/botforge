<script lang="ts">
	import type { Snippet } from 'svelte';
	import Icon from './Icon.svelte';

	// A modal built on <dialog>: the browser provides the focus trap, Escape and
	// an inert background. Focus returns to whatever opened it.
	let {
		open = $bindable(false),
		title,
		description,
		size = 'md',
		dismissable = true,
		onclose,
		children,
		footer,
		stackFooter = false
	}: {
		open?: boolean;
		title: string;
		description?: string;
		size?: 'sm' | 'md' | 'lg';
		dismissable?: boolean;
		onclose?: () => void;
		children?: Snippet;
		footer?: Snippet;
		/** Stack footer buttons full width (for three or more choices). */
		stackFooter?: boolean;
	} = $props();

	let el: HTMLDialogElement | undefined = $state();
	let opener: Element | null = null;
	const uid = Math.random().toString(36).slice(2, 8);

	$effect(() => {
		if (!el) return;
		if (open && !el.open) {
			opener = document.activeElement;
			el.showModal();
			// Prefer the first field; otherwise the heading, so screen readers start at the title.
			const first = el.querySelector<HTMLElement>('[data-autofocus], input:not([type=hidden]):not(:disabled), textarea, select');
			(first ?? el.querySelector<HTMLElement>('h2'))?.focus();
		} else if (!open && el.open) {
			el.close();
		}
	});

	function closed() {
		if (open) open = false;
		onclose?.();
		if (opener instanceof HTMLElement && opener.isConnected) opener.focus();
	}
	function cancel(e: Event) {
		if (!dismissable) e.preventDefault();
	}
	function backdrop(e: MouseEvent) {
		if (dismissable && e.target === el) open = false;
	}
	const width = $derived({ sm: 'sm:max-w-md', md: 'sm:max-w-lg', lg: 'sm:max-w-2xl' }[size]);
</script>

<dialog
	bind:this={el}
	class="m-0 mt-auto w-full max-w-none bg-transparent p-0 text-ink backdrop:backdrop-blur-[1px] open:animate-sheet sm:m-auto sm:open:animate-enter {width}"
	aria-labelledby="dlg-{uid}-t"
	aria-describedby={description ? `dlg-${uid}-d` : undefined}
	onclose={closed}
	oncancel={cancel}
	onclick={backdrop}
>
	<div class="pb-safe flex max-h-[90dvh] flex-col rounded-t-overlay bg-raised shadow-overlay sm:rounded-overlay sm:pb-0">
		<header class="flex items-start gap-3 px-5 pt-4 pb-2">
			<div class="min-w-0 flex-1">
				<h2 id="dlg-{uid}-t" tabindex="-1" class="text-section outline-none">{title}</h2>
				{#if description}<p id="dlg-{uid}-d" class="mt-1 text-muted">{description}</p>{/if}
			</div>
			{#if dismissable}
				<button class="btn btn-quiet btn-icon btn-sm -mr-2" aria-label="Close" onclick={() => (open = false)}><Icon name="x" /></button>
			{/if}
		</header>
		{#if children}<div class="min-h-0 overflow-y-auto px-5 py-2">{@render children()}</div>{/if}
		{#if footer}
			<footer class="flex flex-col-reverse gap-2 px-5 pt-3 pb-4 {stackFooter ? '' : 'sm:flex-row sm:flex-wrap sm:justify-end'}">{@render footer()}</footer>
		{/if}
	</div>
</dialog>
