<script lang="ts">
	import type { DashboardWidget } from '$lib/api/types';
	let { widgets }: { widgets: DashboardWidget[] } = $props();
	const number = (v: unknown, fallback = 0) => typeof v === 'number' && Number.isFinite(v) ? v : fallback;
	const text = (v: unknown) => typeof v === 'string' ? v : '';
	function safeURL(v: unknown) { try { const u=new URL(text(v)); return u.protocol==='https:'||u.protocol==='http:'?u.href:''; } catch { return ''; } }
	function tone(v: unknown) { return ['good','warn','bad'].includes(text(v)) ? text(v) : 'neutral'; }
	const rows = (v: unknown) => Array.isArray(v) ? v.slice(0,20) : [];
</script>

{#if widgets.length}
	<section class="mt-5" aria-labelledby="custom-dashboard-title">
		<div class="flex items-baseline justify-between gap-3"><h2 id="custom-dashboard-title" class="text-title font-semibold">Bot dashboard</h2><span class="text-small text-muted">Published by the bot</span></div>
		<div class="mt-2 grid auto-rows-min gap-3 sm:grid-cols-2 xl:grid-cols-3">
			{#each widgets as w (w.key)}
				<article class="min-w-0 border border-rule-soft bg-panel p-4 {w.kind === 'table' || w.kind === 'chart' ? 'sm:col-span-2' : ''}">
					<h3 class="text-small font-medium text-muted">{w.title}</h3>
					{#if w.kind === 'metric'}
						<p class="mt-1 text-3xl font-semibold tabular-nums">{number(w.data.value).toLocaleString()} <span class="text-small font-normal text-muted">{text(w.data.unit)}</span></p>
						{#if text(w.data.detail)}<p class="mt-1 text-small text-muted">{text(w.data.detail)}</p>{/if}
					{:else if w.kind === 'status'}
						<p class="mt-2 flex items-center gap-2"><span class="size-2.5 rounded-full" class:bg-ok={tone(w.data.state)==='good'} class:bg-warn={tone(w.data.state)==='warn'} class:bg-fail={tone(w.data.state)==='bad'} class:bg-muted={tone(w.data.state)==='neutral'}></span><span class="text-title font-medium">{text(w.data.text) || 'Unknown'}</span></p>
					{:else if w.kind === 'progress'}
						{@const value=number(w.data.value)}{@const max=Math.max(1,number(w.data.max,100))}
						<div class="mt-3 h-2.5 overflow-hidden rounded-full bg-paper-2"><div class="h-full bg-action" style={`width:${Math.max(0,Math.min(100,value/max*100))}%`}></div></div><p class="mt-2 text-small tabular-nums text-muted">{value.toLocaleString()} / {max.toLocaleString()}</p>
					{:else if w.kind === 'text'}
						<p class="mt-2 whitespace-pre-wrap break-words">{text(w.data.text)}</p>
					{:else if w.kind === 'link'}
						{#if safeURL(w.data.url)}<a class="btn mt-3" href={safeURL(w.data.url)} target="_blank" rel="noopener">{text(w.data.label)||'Open link'}</a>{:else}<p class="mt-2 text-small text-fail">The bot supplied an invalid link.</p>{/if}
					{:else if w.kind === 'chart'}
						{@const points=rows(w.data.points)}{@const peak=Math.max(1,...points.map((p:any)=>number(p?.value)))}
						<div class="mt-3 flex h-28 items-end gap-1" aria-label={w.title}>{#each points as p:any}<div class="min-w-1 flex-1 bg-action/70" style={`height:${Math.max(2,number(p?.value)/peak*100)}%`} title={`${text(p?.label)}: ${number(p?.value)}`}></div>{/each}</div>
					{:else if w.kind === 'table'}
						<div class="mt-2 overflow-x-auto"><table class="w-full text-left text-small"><thead><tr>{#each rows(w.data.columns).slice(0,6) as c}<th class="border-b border-rule px-2 py-1 font-medium">{text(c)}</th>{/each}</tr></thead><tbody>{#each rows(w.data.rows) as row:any}<tr>{#each rows(row).slice(0,6) as cell}<td class="border-b border-rule-soft px-2 py-1.5">{text(cell)}</td>{/each}</tr>{/each}</tbody></table></div>
					{/if}
				</article>
			{/each}
		</div>
	</section>
{/if}
