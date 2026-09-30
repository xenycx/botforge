<script lang="ts">
	import { onMount } from 'svelte';
	import { fmtBytes } from '$lib/api/client';
	import type { Gauge } from '$lib/api/types';

	// Compact instrument strip: resource readings are secondary to state, so
	// they sit in one line instead of three large cards. One SSE stream per
	// open bot page; the fleet never opens these.
	let { botId, cpuLimit }: { botId: string; cpuLimit: number } = $props();
	let g = $state<Gauge | null>(null);
	let link = $state<'connecting' | 'live' | 'lost'>('connecting');

	onMount(() => {
		const es = new EventSource(`/api/v1/bots/${botId}/stats/stream`);
		es.addEventListener('stats', (e) => {
			g = JSON.parse((e as MessageEvent).data);
			link = 'live';
		});
		es.onerror = () => (link = es.readyState === EventSource.CLOSED ? 'lost' : 'connecting');
		return () => es.close();
	});

	const pct = (a: number, b: number) => (b > 0 ? Math.min(100, (a / b) * 100) : 0);
	const tone = (p: number) => (p >= 90 ? 'bg-fail' : p >= 75 ? 'bg-warn' : 'bg-ink/55');
	const meters = $derived(
		g
			? [
					{ k: 'CPU', p: g.running ? g.cpu_percent : 0, v: g.running ? `${Math.round(g.cpu_percent)}%` : '–', of: `of ${cpuLimit} core${cpuLimit === 1 ? '' : 's'}` },
					{ k: 'Memory', p: g.running ? pct(g.mem_used_bytes, g.mem_limit_bytes) : 0, v: g.running ? fmtBytes(g.mem_used_bytes) : '–', of: `of ${fmtBytes(g.mem_limit_bytes)}` },
					{ k: 'Disk', p: pct(g.disk_used_bytes, g.disk_used_bytes + g.disk_free_bytes), v: fmtBytes(g.disk_used_bytes), of: `${fmtBytes(g.disk_free_bytes)} free on host` }
				]
			: []
	);
</script>

<div class="flex flex-wrap items-center gap-x-6 gap-y-2 text-small" aria-label="Resource usage">
	{#if !g}
		<span class="text-muted">{link === 'lost' ? 'Live usage is unavailable.' : 'Connecting to live usage…'}</span>
	{:else}
		{#each meters as m (m.k)}
			<div class="flex min-w-36 items-center gap-2">
				<span class="w-12 text-muted">{m.k}</span>
				<span class="h-1.5 w-14 bg-paper-2" role="meter" aria-label="{m.k} usage" aria-valuemin="0" aria-valuemax="100" aria-valuenow={Math.round(m.p)} aria-valuetext="{m.v} {m.of}">
					<span class="block h-full {tone(m.p)}" style="width: {m.p}%"></span>
				</span>
				<span class="tabular-nums"><span class="font-medium">{m.v}</span> <span class="text-muted">{m.of}</span></span>
			</div>
		{/each}
		{#if link !== 'live'}<span class="text-warn">Reconnecting…</span>{/if}
	{/if}
</div>
