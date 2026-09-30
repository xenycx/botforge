<script lang="ts">
	import { onMount } from 'svelte';
	import { api, fmtBytes } from '$lib/api/client';
	import type { Capacity } from '$lib/api/types';
	import NodeStrip from '$lib/components/NodeStrip.svelte';
	import CapacityMeter from '$lib/components/CapacityMeter.svelte';

	let cap = $state<Capacity | null>(null);
	onMount(async () => {
		try {
			cap = await api<Capacity>('GET', '/me/capacity');
		} catch {
			/* the host strip still works */
		}
	});
</script>

<svelte:head><title>Host · BotForge</title></svelte:head>
<h2 class="text-section">Host</h2>
<p class="mt-1 max-w-prose text-muted">Resource use of the machine that runs the bots, sampled every minute and kept for 7 days.</p>
<div class="mt-4"><NodeStrip /></div>

{#if cap?.node}
	<section class="mt-10 max-w-3xl" aria-labelledby="cap-h">
		<h2 id="cap-h" class="text-section">Capacity</h2>
		<p class="mt-1 text-muted">Memory limits of bots that are wanted running, against the admission budget. Starts that would exceed it are refused with an explanation. This is bookkeeping for admission, not a kernel limit.</p>
		<div class="mt-4 grid gap-5 border-y border-rule-soft bg-panel px-4 py-4 sm:grid-cols-2">
			<CapacityMeter label="Reserved memory" value={cap.node.reserved_bytes} max={cap.node.budget_bytes} format={fmtBytes} />
			<div>
				<span class="eyebrow">Running bots</span>
				<p class="mt-0.5 font-mono text-section font-medium">{cap.node.running}</p>
			</div>
		</div>
		<p class="mt-3 text-small text-muted">
			Budgets are set in the environment file: <code>BOTPANEL_NODE_MEMORY_BYTES</code> (node), <code>BOTPANEL_USER_MEMORY_BYTES</code> and <code>BOTPANEL_MAX_BOTS_PER_USER</code> (per account; administrators are exempt), <code>BOTPANEL_MAX_BUILDS</code> (concurrent builds, each up to {fmtBytes(cap.build_memory_bytes)} or its runtime's build memory) and <code>BOTPANEL_MIN_FREE_DISK_BYTES</code> (free space backups and deployments need). 0 means unlimited.
		</p>
	</section>
{/if}
