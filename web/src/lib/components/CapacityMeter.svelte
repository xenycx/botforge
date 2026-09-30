<script lang="ts">
	// One labelled usage bar: value of max (max 0 = unlimited, shown as text).
	let { label, value, max, format = (n: number) => String(n) }: { label: string; value: number; max: number; format?: (n: number) => string } = $props();
	const pct = $derived(max > 0 ? Math.min(100, Math.round((value / max) * 100)) : 0);
	const tone = $derived(pct >= 90 ? 'bg-fail' : pct >= 75 ? 'bg-warn' : 'bg-run');
</script>

<div class="min-w-0">
	<div class="flex items-baseline justify-between gap-2">
		<span class="eyebrow">{label}</span>
		<span class="font-mono text-small"><span class="font-medium text-ink">{format(value)}</span><span class="text-muted"> / {max > 0 ? format(max) : 'no limit'}</span></span>
	</div>
	{#if max > 0}
		<div class="mt-1.5 h-1.5 overflow-hidden rounded-full bg-rule-soft" role="meter" aria-label={label} aria-valuemin="0" aria-valuemax={max} aria-valuenow={value}>
			<div class="h-full rounded-full {tone} transition-[width]" style="width: {pct}%"></div>
		</div>
	{/if}
</div>
