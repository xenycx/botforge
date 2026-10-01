<script lang="ts" module>
	export type Series = {
		label: string;
		values: (number | null)[];
		color?: 'action' | 'run' | 'warn' | 'fail' | 'muted';
		/** Fill under the line. */
		area?: boolean;
		/** Thin dashed line, for peaks beside an average. */
		dashed?: boolean;
	};
</script>

<script lang="ts">
	// A small, honest time-series chart: labelled axes, a hover read-out with
	// every series at that moment, and a text summary for screen readers. Values
	// are drawn as given; nothing is smoothed.
	let {
		times,
		series,
		format = (n: number) => String(Math.round(n * 10) / 10),
		max,
		height = 150,
		label
	}: { times: number[]; series: Series[]; format?: (n: number) => string; max?: number; height?: number; label: string } = $props();

	let width = $state(0);
	let hover = $state<number | null>(null);
	const pad = { l: 46, r: 8, t: 8, b: 20 };
	const iw = $derived(Math.max(0, width - pad.l - pad.r));
	const ih = $derived(height - pad.t - pad.b);

	function niceMax(v: number): number {
		if (v <= 0) return 1;
		const e = 10 ** Math.floor(Math.log10(v));
		for (const m of [1, 2, 2.5, 5, 10]) if (v <= m * e) return m * e;
		return 10 * e;
	}
	const top = $derived.by(() => {
		if (max !== undefined) return max;
		let m = 0;
		for (const s of series) for (const v of s.values) if (v !== null && v > m) m = v;
		return niceMax(m * 1.08);
	});
	const x = (i: number) => pad.l + (times.length > 1 ? (i / (times.length - 1)) * iw : iw / 2);
	const y = (v: number) => pad.t + ih - (Math.min(Math.max(v, 0), top) / top) * ih;

	// Break the line at gaps (null) so a missing sample is not drawn as a value.
	function segments(values: (number | null)[]): string[] {
		const out: string[] = [];
		let cur: string[] = [];
		values.forEach((v, i) => {
			if (v === null) {
				if (cur.length) out.push(cur.join(' '));
				cur = [];
			} else cur.push(`${cur.length ? 'L' : 'M'}${x(i).toFixed(1)},${y(v).toFixed(1)}`);
		});
		if (cur.length) out.push(cur.join(' '));
		return out;
	}
	const areaPath = (values: (number | null)[]) => {
		const pts = values.map((v, i) => (v === null ? null : [x(i), y(v)])).filter(Boolean) as number[][];
		if (pts.length < 2) return '';
		return `M${pts[0][0].toFixed(1)},${(pad.t + ih).toFixed(1)} ` + pts.map((p) => `L${p[0].toFixed(1)},${p[1].toFixed(1)}`).join(' ') + ` L${pts.at(-1)![0].toFixed(1)},${(pad.t + ih).toFixed(1)}Z`;
	};

	const yTicks = $derived([0, 0.5, 1].map((f) => ({ v: top * f, y: y(top * f) })));
	const span = $derived(times.length > 1 ? times.at(-1)! - times[0] : 0);
	const fmtT = (t: number) => {
		const d = new Date(t);
		return span > 36 * 3600_000 ? d.toLocaleDateString(undefined, { month: 'short', day: 'numeric' }) : d.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' });
	};
	// Four evenly spaced labels; with only a few samples several would land on the
	// same one, so each index is used once (duplicate keys would also break the list).
	const xTicks = $derived(
		times.length < 2
			? []
			: [...new Set([0, 1, 2, 3].map((k) => Math.round((k / 3) * (times.length - 1))))].map((i) => ({ i, label: fmtT(times[i]), anchor: i === 0 ? 'start' : i === times.length - 1 ? 'end' : 'middle' }))
	);

	function move(e: PointerEvent) {
		if (times.length < 1 || iw <= 0) return;
		const r = (e.currentTarget as SVGElement).getBoundingClientRect();
		const f = (e.clientX - r.left - pad.l) / iw;
		hover = Math.min(times.length - 1, Math.max(0, Math.round(f * (times.length - 1))));
	}
	const colorClass = { action: 'text-action', run: 'text-run', warn: 'text-warn', fail: 'text-fail', muted: 'text-muted' } as const;
	const tip = $derived(hover === null || hover >= times.length ? null : { i: hover, left: Math.min(Math.max(x(hover), 70), width - 70) });

	const summary = $derived.by(() => {
		const s = series[0];
		const vs = s?.values.filter((v): v is number => v !== null) ?? [];
		if (!vs.length) return `${label}: no samples.`;
		return `${label}: now ${format(vs.at(-1)!)}, lowest ${format(Math.min(...vs))}, highest ${format(Math.max(...vs))}, average ${format(vs.reduce((a, b) => a + b, 0) / vs.length)}.`;
	});
</script>

<div class="min-w-0" bind:clientWidth={width}>
	{#if series.filter((s) => !s.dashed).length > 1}
		<ul class="mb-1 flex flex-wrap gap-x-4 gap-y-0.5 text-small text-muted" aria-hidden="true">
			{#each series.filter((s) => !s.dashed) as s (s.label)}
				<li class="flex items-center gap-1.5"><span class="inline-block h-0.5 w-3.5 rounded-pill bg-current {colorClass[s.color ?? 'action']}"></span>{s.label}</li>
			{/each}
		</ul>
	{/if}
	{#if times.length < 2}
		<p class="grid place-items-center text-small text-muted" style="height: {height}px">Not enough samples in this range yet.</p>
	{:else if width > 0}
		<div class="relative">
			<svg {width} {height} role="img" aria-label={summary} class="block touch-pan-y" onpointermove={move} onpointerleave={() => (hover = null)}>
				{#each yTicks as t, k (k)}
					<line x1={pad.l} x2={width - pad.r} y1={t.y} y2={t.y} stroke="var(--color-rule-soft)" stroke-dasharray={t.v === 0 ? undefined : '2 3'} />
					<text x={pad.l - 6} y={t.y + 3.5} text-anchor="end" class="fill-muted font-mono text-[10px]">{format(t.v)}</text>
				{/each}
				{#each xTicks as t (t.i)}
					<text x={x(t.i)} y={height - 5} text-anchor={t.anchor} class="fill-muted font-mono text-[10px]">{t.label}</text>
				{/each}
				{#each series as s (s.label)}
					<g class={colorClass[s.color ?? 'action']}>
						{#if s.area}<path d={areaPath(s.values)} fill="currentColor" fill-opacity="0.12" />{/if}
						{#each segments(s.values) as d, k (k)}
							<path {d} fill="none" stroke="currentColor" stroke-width={s.dashed ? 1 : 1.6} stroke-dasharray={s.dashed ? '3 3' : undefined} stroke-opacity={s.dashed ? 0.6 : 1} stroke-linejoin="round" stroke-linecap="round" />
						{/each}
					</g>
				{/each}
				{#if tip}
					<line x1={x(tip.i)} x2={x(tip.i)} y1={pad.t} y2={pad.t + ih} stroke="var(--color-muted)" stroke-opacity="0.5" />
					{#each series.filter((s) => !s.dashed) as s (s.label)}
						{#if s.values[tip.i] !== null && s.values[tip.i] !== undefined}
							<circle cx={x(tip.i)} cy={y(s.values[tip.i]!)} r="3.5" class="{colorClass[s.color ?? 'action']}" fill="var(--color-panel)" stroke="currentColor" stroke-width="1.6" />
						{/if}
					{/each}
				{/if}
			</svg>
			{#if tip}
				<div class="pointer-events-none absolute top-0 z-10 -translate-x-1/2 rounded-control border border-rule bg-raised px-2.5 py-1.5 text-small shadow-overlay" style="left: {tip.left}px">
					<p class="font-mono text-[11px] text-muted">{new Date(times[tip.i]).toLocaleString(undefined, { dateStyle: 'medium', timeStyle: 'short' })}</p>
					{#each series as s (s.label)}
						{#if s.values[tip.i] !== null && s.values[tip.i] !== undefined}
							<p class="flex items-center justify-between gap-4 whitespace-nowrap"><span class="flex items-center gap-1.5 text-muted"><span class="inline-block size-2 rounded-pill bg-current {colorClass[s.color ?? 'action']}"></span>{s.label}</span><span class="font-mono font-medium">{format(s.values[tip.i]!)}</span></p>
						{/if}
					{/each}
				</div>
			{/if}
		</div>
	{/if}
</div>
