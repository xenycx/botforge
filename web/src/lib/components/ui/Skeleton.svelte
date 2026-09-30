<script lang="ts">
	// Static placeholder rows while data loads (no shimmer animation).
	let { rows = 3, label = 'Loading' }: { rows?: number; label?: string } = $props();
</script>

<div class="space-y-2.5" role="status" aria-label={label}>
	{#each Array(rows) as _, i (i)}
		<div class="h-10 bg-paper-2/70" style="width: {100 - ((i * 17) % 35)}%"></div>
	{/each}
</div>
