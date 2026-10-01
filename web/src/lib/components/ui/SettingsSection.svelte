<script lang="ts">
	import type { Snippet } from 'svelte';

	// One block of a settings page: the explanation sits beside the controls
	// when there is room (container query), and above them when there is not,
	// so wide screens do not waste the right half and narrow ones do not squeeze.
	let {
		title,
		description,
		badge,
		aside,
		children,
		id = `s-${Math.random().toString(36).slice(2, 8)}`,
		class: cls = ''
	}: { title: string; description?: string; badge?: Snippet; aside?: Snippet; children: Snippet; id?: string; class?: string } = $props();
</script>

<section class="@container border-t border-rule-soft py-7 first-of-type:border-t-0 first-of-type:pt-1 {cls}" aria-labelledby={id}>
	<div class="grid gap-x-10 gap-y-4 @3xl:grid-cols-[minmax(0,16rem)_minmax(0,1fr)]">
		<header class="min-w-0">
			<h2 {id} class="flex flex-wrap items-center gap-2 text-title font-semibold">{title}{@render badge?.()}</h2>
			{#if description}<p class="mt-1 text-small text-muted">{description}</p>{/if}
			{@render aside?.()}
		</header>
		<div class="min-w-0">{@render children()}</div>
	</div>
</section>
