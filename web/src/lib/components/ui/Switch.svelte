<script lang="ts">
	import type { Snippet } from 'svelte';

	// An on/off setting with its explanation beside it. The native checkbox stays
	// in the page (transparent, over the track), so keyboard and screen readers work as usual; it also covers the track so a click lands on it.
	let { checked = $bindable(false), disabled = false, label, children }: { checked?: boolean; disabled?: boolean; label: string; children?: Snippet } = $props();
</script>

<label class="flex items-start gap-3 {disabled ? 'opacity-55' : 'cursor-pointer'}">
	<span class="relative mt-0.5 inline-flex h-5 w-9 shrink-0">
		<input type="checkbox" class="peer absolute inset-0 z-10 m-0 size-full cursor-pointer opacity-0 disabled:cursor-default" bind:checked {disabled} />
		<span class="absolute inset-0 rounded-pill bg-rule transition-colors peer-checked:bg-action peer-focus-visible:outline-2 peer-focus-visible:outline-offset-2 peer-focus-visible:outline-action"></span>
		<span class="absolute top-0.5 left-0.5 size-4 rounded-pill bg-white shadow-sm transition-transform peer-checked:translate-x-4"></span>
	</span>
	<span class="min-w-0"><span class="font-medium">{label}</span>{#if children}<span class="block text-small text-muted">{@render children()}</span>{/if}</span>
</label>
