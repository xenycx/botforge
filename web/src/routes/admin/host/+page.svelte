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
<p class="mt-1 max-w-3xl text-muted">Resource use of the machine that runs the bots, sampled every minute and kept for 7 days.</p>
<div class="mt-5"><NodeStrip /></div>

{#if cap?.node}
	{@const node = cap.node}
	<section class="mt-10" aria-labelledby="cap-h">
		<h2 id="cap-h" class="text-section">Capacity</h2>
		<p class="mt-1 max-w-3xl text-muted">Memory limits of bots that are wanted running, counted against the admission budget. A start that would exceed it is refused with an explanation. This is bookkeeping for admission, not a kernel limit.</p>
		<div class="mt-5 grid gap-3 md:grid-cols-3">
			<div class="stat md:col-span-2">
				<CapacityMeter label="Reserved memory" value={node.reserved_bytes} max={node.budget_bytes} format={fmtBytes} />
				<p class="mt-2 text-small text-muted">
					{#if node.budget_bytes > 0}{fmtBytes(Math.max(0, node.budget_bytes - node.reserved_bytes))} left for new starts.{:else}No node budget is set, so starts are never refused for memory. Set <code>BOTPANEL_NODE_MEMORY_BYTES</code> to protect a small host from overcommitting.{/if}
				</p>
			</div>
			<div class="stat">
				<p class="eyebrow">Bots wanted running</p>
				<p class="mt-1 font-mono text-section font-semibold">{node.running}</p>
				<p class="text-small text-muted">Each build may use up to {fmtBytes(cap.build_memory_bytes)} on top.</p>
			</div>
		</div>

		<h3 class="mt-8 text-title font-semibold">Budgets</h3>
		<p class="mt-1 text-small text-muted">Set in the environment file and applied at the next restart. 0 means unlimited. Administrators are exempt from per-account limits.</p>
		<div class="card mt-3 overflow-x-auto">
			<table class="w-full min-w-[36rem] text-left">
				<thead class="border-b border-rule-soft">
					<tr class="[&>th]:px-4 [&>th]:py-2.5 [&>th]:font-normal"><th class="eyebrow">Setting</th><th class="eyebrow">Limits</th><th class="eyebrow">Scope</th></tr>
				</thead>
				<tbody class="divide-y divide-rule-soft text-small">
					{#each [['BOTPANEL_NODE_MEMORY_BYTES', 'Sum of memory limits of bots wanted running', 'Host'], ['BOTPANEL_USER_MEMORY_BYTES', 'Sum of memory limits of one account’s bots', 'Account'], ['BOTPANEL_MAX_BOTS_PER_USER', 'Number of bots one account can create', 'Account'], ['BOTPANEL_MAX_BUILDS', `Builds running at once, each up to ${fmtBytes(cap.build_memory_bytes)} or its runtime’s build memory`, 'Host'], ['BOTPANEL_MIN_FREE_DISK_BYTES', 'Free disk space backups and deployments need before they start', 'Host']] as [k, what, scope] (k)}
						<tr class="[&>td]:px-4 [&>td]:py-2.5"><td><code class="font-mono text-[12px]">{k}</code></td><td>{what}</td><td class="text-muted">{scope}</td></tr>
					{/each}
				</tbody>
			</table>
		</div>
	</section>
{/if}
