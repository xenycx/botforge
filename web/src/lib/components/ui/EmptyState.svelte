<script lang="ts">
	import type { Snippet } from 'svelte';
	// Every empty state names the next useful action.
	let { title, children, actions, compact = false }: { title: string; children?: Snippet; actions?: Snippet; compact?: boolean } = $props();
</script>

<div class="border border-dashed border-rule {compact ? 'px-4 py-5' : 'px-6 py-8'}">
	<h2 class="text-title">{title}</h2>
	{#if children}<div class="mt-1 max-w-prose text-muted">{@render children()}</div>{/if}
	{#if actions}<div class="mt-4 flex flex-wrap gap-2">{@render actions()}</div>{/if}
</div>
