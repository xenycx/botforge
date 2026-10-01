<script lang="ts">
	import type { AuditEvent } from '$lib/audit';
	import { actionText } from '$lib/audit';
	import { fmtAgo, fmtWhen } from '$lib/args';

	// Recorded changes as a compact list. Values are never recorded, only names.
	let { events, showBot = false, now = Date.now() }: { events: AuditEvent[]; showBot?: boolean; now?: number } = $props();
</script>

<ul class="list-card">
	{#each events as e (e.id)}
		<li class="flex flex-wrap items-baseline gap-x-3 gap-y-0.5 px-3 py-2.5">
			<p class="min-w-0 flex-1 basis-72">
				<span class="font-medium">{e.actor ?? 'Someone'}</span>
				{actionText[e.action] ?? e.action}{#if e.target}<code class="ml-1 text-small break-all">{e.target}</code>{/if}{#if showBot && e.bot_name}<span class="text-muted"> on </span>{#if e.bot_id}<a class="link" href="/bots/{e.bot_id}">{e.bot_name}</a>{:else}{e.bot_name}{/if}{/if}
				{#if e.outcome === 'denied'}<span class="ml-1 text-small font-medium text-fail">Refused</span>{:else if e.outcome === 'failed'}<span class="ml-1 text-small font-medium text-warn">Did not complete</span>{/if}
			</p>
			<span class="shrink-0 text-small text-muted" title={fmtWhen(e.at_ms)}>{fmtAgo(e.at_ms, now)}</span>
		</li>
	{/each}
</ul>
