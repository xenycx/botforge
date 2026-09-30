<script lang="ts">
	import { onMount } from 'svelte';
	import { api, fmtBytes } from '$lib/api/client';
	import type { NodeInfo, Sample } from '$lib/api/types';
	import Sparkline from './Sparkline.svelte';

	let nodes = $state<NodeInfo[]>([]);
	let series = $state<Record<string, Sample[]>>({});
	let error = $state('');

	async function load() {
		try {
			nodes = (await api<{ nodes: NodeInfo[] }>('GET', '/nodes')).nodes;
			for (const n of nodes) {
				series[n.id] = (await api<{ samples: Sample[] }>('GET', `/nodes/${n.id}/telemetry?limit=60`)).samples;
			}
			error = '';
		} catch {
			error = 'Node metrics are unavailable right now.';
		}
	}
	onMount(() => {
		load();
		const t = setInterval(load, 30_000);
		return () => clearInterval(t);
	});
	const pct = (a: number, b: number) => (b ? Math.round((a / b) * 100) : 0);
</script>

{#if error}
	<p class="text-muted">{error}</p>
{:else}
	{#each nodes as n (n.id)}
		{@const s = series[n.id] ?? []}
		{@const last = s.at(-1)}
		<section class="border-y border-rule-soft bg-panel p-4" aria-label="Node {n.name}">
			<h3 class="text-title font-semibold">Node {n.name}</h3>
			{#if last}
				<dl class="mt-2 grid grid-cols-2 gap-x-8 gap-y-3 sm:grid-cols-4">
					<div>
						<dt class="text-muted">CPU, all {last.logical_cpus} cores</dt>
						<dd class="text-section font-semibold">{last.cpu_percent.toFixed(0)}%</dd>
						<Sparkline values={s.map((x) => x.cpu_percent)} label="CPU history" />
					</div>
					<div>
						<dt class="text-muted">Memory</dt>
						<dd class="text-section font-semibold">{pct(last.memory_used_bytes, last.memory_total_bytes)}%</dd>
						<Sparkline values={s.map((x) => pct(x.memory_used_bytes, x.memory_total_bytes))} label="Memory history" />
					</div>
					<div>
						<dt class="text-muted">Disk</dt>
						<dd class="text-section font-semibold">{fmtBytes(last.disk_used_bytes)}</dd>
						<span class="text-muted">of {fmtBytes(last.disk_total_bytes)}</span>
					</div>
					<div>
						<dt class="text-muted">Running bots</dt>
						<dd class="text-section font-semibold">{last.running_bots}</dd>
					</div>
				</dl>
			{:else}
				<p class="text-muted">Collecting the first samples. Metrics appear after about a minute.</p>
			{/if}
		</section>
	{/each}
{/if}
