<script lang="ts">
	import { onMount } from 'svelte';
	import { api, ApiError } from '$lib/api/client';
	import { fmtDuration, fmtWhen } from '$lib/args';
	import Icon from '$lib/components/ui/Icon.svelte';
	import Notice from '$lib/components/ui/Notice.svelte';
	import Skeleton from '$lib/components/ui/Skeleton.svelte';

	type Check = { id: string; group: string; title: string; status: 'ok' | 'warn' | 'fail' | 'info'; detail: string; fix?: string };
	type Report = { generated_at_ms: number; version: string; go_version: string; uptime_ms: number; checks: Check[] };
	let report = $state<Report | null>(null);
	let error = $state('');
	let running = $state(false);

	async function run() {
		running = true;
		try {
			report = await api<Report>('GET', '/admin/diagnostics');
			error = '';
		} catch (e) {
			error = e instanceof ApiError ? e.message : 'The checks could not run.';
		} finally {
			running = false;
		}
	}
	onMount(run);

	const groups = $derived(report ? [...new Set(report.checks.map((c) => c.group))] : []);
	const counts = $derived({
		fail: report?.checks.filter((c) => c.status === 'fail').length ?? 0,
		warn: report?.checks.filter((c) => c.status === 'warn').length ?? 0
	});
	const tone = { ok: 'run', warn: 'warn', fail: 'fail', info: 'idle' } as const;
	const word = { ok: 'OK', warn: 'Check', fail: 'Problem', info: 'Note' } as const;
</script>

<svelte:head><title>Diagnostics · BotForge</title></svelte:head>
<div class="flex flex-wrap items-end justify-between gap-3">
	<div>
		<h2 class="text-section">Diagnostics</h2>
		<p class="mt-1 max-w-prose text-muted">Read-only checks of this installation. The same report is available on the host with <code>botpanel doctor</code>.</p>
	</div>
	<div class="flex gap-2">
		<a class="btn" href="/api/v1/admin/diagnostics?download=1" download><Icon name="download" />Download report</a>
		<button class="btn btn-primary" onclick={run} disabled={running}><Icon name="restart" />{running ? 'Checking…' : 'Run again'}</button>
	</div>
</div>

{#if error}<Notice tone="fail" class="mt-4">{error}</Notice>{/if}
{#if !report && !error}
	<div class="mt-4"><Skeleton rows={5} /></div>
{:else if report}
	<Notice tone={counts.fail ? 'fail' : counts.warn ? 'warn' : 'success'} class="mt-4" title={counts.fail ? `${counts.fail} problem${counts.fail === 1 ? '' : 's'} need attention` : counts.warn ? `${counts.warn} thing${counts.warn === 1 ? '' : 's'} to check` : 'Everything checked out'}>
		Version {report.version}, running for {fmtDuration(report.uptime_ms)}. Checked {fmtWhen(report.generated_at_ms)}.
	</Notice>
	{#each groups as g (g)}
		<section class="mt-6" aria-labelledby="g-{g}">
			<h3 id="g-{g}" class="text-title font-semibold">{g}</h3>
			<ul class="mt-2 border-y border-rule-soft bg-panel [&>li+li]:border-t [&>li+li]:border-rule-soft">
				{#each report.checks.filter((c) => c.group === g) as c (c.id)}
					<li class="spine grid gap-x-4 gap-y-0.5 py-3 pr-3 pl-5 sm:grid-cols-[12rem_minmax(0,1fr)]" data-tone={tone[c.status]}>
						<p class="font-medium">{c.title}<span class="ml-2 text-small font-normal {c.status === 'fail' ? 'text-fail' : c.status === 'warn' ? 'text-warn' : c.status === 'ok' ? 'text-run' : 'text-muted'}">{word[c.status]}</span></p>
						<div>
							<p class="break-words">{c.detail}</p>
							{#if c.fix && c.status !== 'ok'}<p class="mt-0.5 text-small text-muted">{c.fix}</p>{/if}
						</div>
					</li>
				{/each}
			</ul>
		</section>
	{/each}
{/if}
