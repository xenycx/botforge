<script lang="ts">
	import { onMount } from 'svelte';
	import { api, ApiError } from '$lib/api/client';
	import { confirmDialog } from '$lib/ui/dialogs.svelte';
	import { toast } from '$lib/ui/toast.svelte';
	import Icon from '$lib/components/ui/Icon.svelte';
	import Notice from '$lib/components/ui/Notice.svelte';
	import SafeMarkdown from '$lib/components/SafeMarkdown.svelte';

	let { botId, siteId, targetName }: { botId?: string; siteId?: string; targetName: string } = $props();
	type Provider = { id: string; name: string; default_model: string; default: boolean; pricing_configured: boolean };
	type Conversation = { id: string; title: string; provider_id: string | null; model: string | null; updated_at_ms: number };
	type Message = { id: string; role: 'user' | 'assistant'; content: string; created_at_ms: number };
	type Run = { id: string; status: string; mode: string; input_tokens: number; output_tokens: number; error_message?: string | null };
	type ToolCard = { id: string; name: string; args?: string; output?: string; status: string; approval?: boolean; secure?: boolean; names?: string[]; title?: string; exit?: number; duration?: number };
	type Change = { id: string; run_id?: string; status: string; summary: string; files: { path: string; operation: string; diff: string }[] };
	type ToolCall = { id: string; name: string; arguments_json: string; output: string; approval_state: string; status: string; exit_code: number | null; duration_ms: number | null };
	type RunDetail = Run & { tool_calls: ToolCall[]; change_sets: Change[] };
	type Event = { sequence: number; type: string; data?: any };

	let providers = $state<Provider[]>([]);
	let conversations = $state<Conversation[]>([]);
	let selected = $state<Conversation | null>(null);
	let messages = $state<Message[]>([]);
	let run = $state<Run | null>(null);
	let tools = $state<ToolCard[]>([]);
	let changes = $state<Change[]>([]);
	let citations = $state<any[]>([]);
	let streaming = $state('');
	let prompt = $state('');
	let mode = $state<'approval' | 'auto'>('approval');
	let error = $state('');
	let busy = $state(false);
	let lastSequence = $state(0);
	let ws: WebSocket | null = null;
	let secureValues = $state<Record<string, Record<string, string>>>({});
	const base = $derived(botId ? `/bots/${botId}` : `/sites/${siteId}`);
	const active = $derived(!!run && ['queued', 'running', 'waiting_approval'].includes(run.status));
	const provider = $derived(providers.find((p) => p.id === selected?.provider_id));

	const titles: Record<string, string> = { auto_repair_envelope: 'Approve bounded Auto repair', request_environment_values: 'Secure environment input', run_diagnostic: 'Run isolated diagnostic', propose_file_change: 'Apply reviewed file change', restart_bot: 'Restart bot' };
	const isActive = (r: Run | null) => !!r && ['queued', 'running', 'waiting_approval'].includes(r.status);
	const why = (e: unknown) => (e instanceof ApiError ? e.message : 'The AI workspace could not complete that request.');
	async function loadList() {
		try {
			providers = (await api<{ providers: Provider[] }>('GET', '/ai/providers')).providers;
			conversations = (await api<{ conversations: Conversation[] }>('GET', `${base}/ai/conversations`)).conversations;
			if (!selected && conversations[0]) await open(conversations[0]);
		} catch (e) { error = why(e); }
	}
	async function create() {
		busy = true;
		try { const c = await api<Conversation>('POST', `${base}/ai/conversations`, { title: `${targetName} incident` }); conversations = [c, ...conversations]; await open(c); }
		catch (e) { error = why(e); } finally { busy = false; }
	}
	async function open(c: Conversation) {
		ws?.close(); ws = null; selected = c; run = null; tools = []; changes = []; citations = []; streaming = ''; lastSequence = 0;
		try { const d = await api<{ conversation: Conversation; messages: Message[] }>('GET', `/ai/conversations/${c.id}`); selected = d.conversation; messages = d.messages; }
		catch (e) { error = why(e); return; }
		const latest = await restore(c.id);
		// An active run resumes its stream from the start; replayed events
		// update the restored cards in place.
		if (latest && isActive(latest)) connect(latest.id);
	}
	// restore rebuilds the inspector from the server: the latest run's tool
	// cards (pending approvals and secure input only while it is active) and
	// the change sets of recent runs, so Undo survives reloads.
	async function restore(conversationId: string): Promise<Run | null> {
		try {
			const runs = (await api<{ runs: RunDetail[] }>('GET', `/ai/conversations/${conversationId}/runs`)).runs;
			if (selected?.id !== conversationId) return null;
			const latest = runs[0] ?? null;
			run = latest;
			const live = isActive(latest);
			tools = (latest?.tool_calls ?? []).map((c) => card(c, live));
			changes = runs.flatMap((r) => [...(r.change_sets ?? [])].reverse());
			return latest;
		} catch (e) { error = why(e); return null; }
	}
	function card(c: ToolCall, live: boolean): ToolCard {
		const pending = live && c.approval_state === 'pending';
		const secureCall = c.name === 'request_environment_values';
		let names: string[] | undefined;
		if (secureCall) { try { names = JSON.parse(c.arguments_json).names; } catch { names = []; } }
		return { id: c.id, name: c.name, args: c.arguments_json, output: c.output || undefined, status: pending ? 'waiting' : c.status, approval: pending && !secureCall, secure: pending && secureCall, names, title: pending ? titles[c.name] : undefined, exit: c.exit_code ?? undefined, duration: c.duration_ms ?? undefined };
	}
	async function selectProvider(id: string) {
		if (!selected) return; const p = providers.find((x) => x.id === id); if (!p) return;
		try { selected = await api<Conversation>('PATCH', `/ai/conversations/${selected.id}`, { provider_id: id, model: p.default_model }); conversations = conversations.map((x) => x.id === selected?.id ? selected! : x); }
		catch (e) { error = why(e); }
	}
	async function saveModel() { if (!selected?.model) return; try { selected = await api<Conversation>('PATCH', `/ai/conversations/${selected.id}`, { model: selected.model }); } catch (e) { error = why(e); } }
	async function send() {
		if (!selected || !prompt.trim() || active) return;
		if (mode === 'auto') {
			const ok = await confirmDialog({ title: 'Start bounded Auto repair?', body: 'The operator will first show a visible action envelope. Nothing live changes until you approve it. It can then apply target files, run isolated diagnostics, and restart within the displayed limits. GitHub pushes still need separate approval.', confirmLabel: 'Continue to plan' });
			if (!ok) return;
		}
		const content = prompt.trim(); prompt = ''; messages = [...messages, { id: crypto.randomUUID(), role: 'user', content, created_at_ms: Date.now() }]; busy = true; error = ''; tools = []; citations = []; streaming = '';
		try { run = await api<Run>('POST', `/ai/conversations/${selected.id}/messages`, { content, mode }); lastSequence = 0; connect(run.id); }
		catch (e) { error = why(e); prompt = content; } finally { busy = false; }
	}
	function connect(id: string) {
		ws?.close(); ws = null; const scheme = location.protocol === 'https:' ? 'wss:' : 'ws:'; ws = new WebSocket(`${scheme}//${location.host}/api/v1/ai/runs/${id}/stream?after=${lastSequence}`);
		ws.onmessage = (m) => { const e = JSON.parse(m.data) as Event; lastSequence = Math.max(lastSequence, e.sequence); consume(e); };
		const sock = ws;
		sock.onclose = () => { if (ws === sock && active) setTimeout(() => run?.id === id && ws === sock && connect(id), 1200); };
	}
	function callValue(v: any, low: string, high: string) { return v?.[low] ?? v?.[high]; }
	function upsert(card: ToolCard) { const i = tools.findIndex((t) => t.id === card.id); tools = i < 0 ? [...tools, card] : tools.map((t, n) => n === i ? { ...t, ...card } : t); }
	async function consume(e: Event) {
		const d = e.data ?? {};
		if (e.type === 'delta') streaming += d.text ?? '';
		else if (e.type === 'tool_proposed') upsert({ id: callValue(d, 'id', 'ID'), name: callValue(d, 'name', 'Name'), args: callValue(d, 'arguments_json', 'ArgumentsJSON'), status: 'running' });
		else if (e.type === 'tool_output') upsert({ id: d.id, name: d.name, output: d.output, status: d.status, exit: d.exit_code, duration: d.duration_ms });
		else if (e.type === 'approval_required') { const c = d.tool_call ?? {}; upsert({ id: callValue(c, 'id', 'ID'), name: callValue(c, 'name', 'Name'), args: callValue(c, 'arguments_json', 'ArgumentsJSON'), status: 'waiting', approval: !d.secure_input, secure: !!d.secure_input, names: d.names, title: d.title }); if (run) run = { ...run, status: 'waiting_approval' }; }
		else if (e.type === 'status' && d.tool_call_id) { const t = tools.find((x) => x.id === d.tool_call_id); if (t) upsert({ ...t, approval: false, secure: false, status: d.approval ?? (d.secure_names_configured ? 'configured' : t.status) }); if (run?.status === 'waiting_approval') run = { ...run, status: 'running' }; }
		else if (e.type === 'change_set') { const i = changes.findIndex((x) => x.id === d.id); changes = i < 0 ? [d as Change, ...changes] : changes.map((x, n) => n === i ? { ...x, ...d } : x); }
		else if (e.type === 'citation') { const add = (Array.isArray(d) ? d : [d]).filter((c: any) => !citations.some((x) => (x.URL ?? x.url) === (c.URL ?? c.url))); citations = [...citations, ...add]; }
		else if (e.type === 'usage' && run) run = { ...run, input_tokens: d.input_tokens, output_tokens: d.output_tokens };
		else if (['done', 'error', 'cancelled'].includes(e.type)) await finished();
	}
	// finished keeps the inspector: messages and run cards are reloaded from
	// the server, so applied change sets keep their Undo.
	async function finished() {
		const c = selected; if (!c) return;
		ws?.close(); ws = null;
		try { const d = await api<{ conversation: Conversation; messages: Message[] }>('GET', `/ai/conversations/${c.id}`); if (selected?.id === c.id) messages = d.messages; } catch (e) { error = why(e); }
		streaming = '';
		await restore(c.id);
	}
	async function decide(t: ToolCard, approve: boolean) { try { await api('POST', `/ai/tool-calls/${t.id}/decision`, { approve }); upsert({ ...t, approval: false, status: approve ? 'approved' : 'rejected' }); } catch (e) { error = why(e); } }
	async function secure(t: ToolCard) { try { await api('POST', `/ai/tool-calls/${t.id}/secure-input`, { values: secureValues[t.id] ?? {} }); secureValues[t.id] = {}; upsert({ ...t, secure: false, status: 'configured' }); } catch (e) { error = why(e); } }
	async function cancel() { if (!run) return; try { await api('POST', `/ai/runs/${run.id}/cancel`); run = { ...run, status: 'cancelled' }; await finished(); } catch (e) { error = why(e); } }
	async function undo(id: string) { try { await api('POST', `/ai/change-sets/${id}/revert`); changes = changes.map((x) => x.id === id ? { ...x, status: 'reverted' } : x); toast('AI change set reverted', 'success'); } catch (e) { error = why(e); } }
	onMount(() => { loadList(); return () => ws?.close(); });
</script>

<div class="overflow-hidden rounded-tile border border-rule bg-paper shadow-card">
	<div class="flex flex-wrap items-center gap-3 border-b border-rule-soft bg-paper-2/70 px-4 py-3">
		<span class="grid size-8 place-items-center rounded-control bg-action/10 text-action"><Icon name="bolt" /></span>
		<div class="min-w-0 flex-1"><h3 class="font-semibold">AI operator</h3><p class="text-small text-muted">Investigate, stage, verify, approve.</p></div>
		<label class="flex items-center gap-2 text-small"><span class="text-muted">Mode</span><select class="field w-auto py-1.5" bind:value={mode} disabled={active}><option value="approval">Approval</option><option value="auto">Auto repair</option></select></label>
		{#if active}<button class="btn btn-sm" onclick={cancel}>Cancel run</button>{/if}
	</div>
	{#if error}<Notice tone="fail" class="m-3" live>{error}</Notice>{/if}
	<div class="grid min-h-[36rem] xl:grid-cols-[15rem_minmax(20rem,1fr)_21rem]">
		<aside class="border-b border-rule-soft bg-paper-2/35 p-3 xl:border-r xl:border-b-0" aria-label="Incident conversations">
			<button class="btn btn-primary w-full" onclick={create} disabled={busy || providers.length === 0}><Icon name="plus" />New incident</button>
			{#if providers.length === 0}<p class="mt-3 rounded-control border border-rule-soft bg-paper p-3 text-small text-muted">An administrator must add an enabled AI provider before new messages can run.</p>{/if}
			<div class="mt-3 flex gap-2 overflow-x-auto xl:block xl:space-y-1">
				{#each conversations as c (c.id)}<button class="min-w-48 rounded-control px-3 py-2 text-left text-small xl:w-full {selected?.id === c.id ? 'bg-paper font-medium shadow-sm' : 'hover:bg-paper/70'}" onclick={() => open(c)}><span class="block truncate">{c.title}</span><span class="text-muted">{new Date(c.updated_at_ms).toLocaleDateString()}</span></button>{/each}
			</div>
		</aside>

		<main class="flex min-h-0 flex-col">
			{#if selected}
				<div class="flex flex-wrap gap-2 border-b border-rule-soft px-4 py-2.5">
					<select class="field min-w-40 flex-1 py-1.5" value={selected.provider_id ?? ''} onchange={(e) => selectProvider(e.currentTarget.value)} disabled={active}>{#each providers as p}<option value={p.id}>{p.name}</option>{/each}</select>
					<input class="field min-w-36 flex-1 py-1.5 font-mono text-small" bind:value={selected.model} onblur={saveModel} disabled={active} aria-label="Model ID" />
				</div>
				<div class="flex-1 space-y-4 overflow-y-auto p-4" aria-live="polite">
					{#each messages as m (m.id)}<article class="max-w-[52rem] {m.role === 'user' ? 'ml-auto rounded-tile bg-action px-4 py-3 text-white' : ''}">{#if m.role === 'assistant'}<div class="mb-1 eyebrow text-action">Operator</div>{/if}<SafeMarkdown source={m.content} /></article>{/each}
					{#if streaming}<article class="max-w-[52rem]"><div class="mb-1 eyebrow text-action">Operator · working</div><SafeMarkdown source={streaming} /><span class="ml-1 inline-block size-1.5 animate-pulse rounded-full bg-action"></span></article>{/if}
					{#if messages.length === 0}<div class="mx-auto max-w-lg py-16 text-center"><span class="mx-auto grid size-12 place-items-center rounded-pill bg-action/10 text-action"><Icon name="activity" size={20} /></span><h4 class="mt-3 text-title font-semibold">Start with the symptom</h4><p class="mt-1 text-muted">Try “Why did the last deployment fail?” or “Check the startup command and repair the build.” Read-only investigation starts automatically.</p></div>{/if}
				</div>
				<form class="border-t border-rule-soft p-3" onsubmit={(e) => { e.preventDefault(); send(); }}><label class="sr-only" for="ai-prompt">Message AI operator</label><textarea id="ai-prompt" class="field min-h-24 resize-y" bind:value={prompt} placeholder="Describe the incident or feature…" disabled={active || providers.length === 0}></textarea><div class="mt-2 flex items-center justify-between gap-3"><p class="text-small text-muted">{mode === 'approval' ? 'Live actions stop for review.' : 'One envelope approval, then bounded repairs.'}</p><button class="btn btn-primary" disabled={busy || active || !prompt.trim()}><Icon name="play" size={12} />Run</button></div></form>
			{:else}<div class="grid flex-1 place-items-center p-8 text-center text-muted">Create or open an incident conversation.</div>{/if}
		</main>

		<aside class="border-t border-rule-soft bg-paper-2/25 p-3 xl:border-t-0 xl:border-l" aria-label="Run inspector">
			<div class="flex items-center justify-between"><h4 class="font-semibold">Run inspector</h4>{#if run}<span class="pill" data-tone={run.status === 'completed' ? 'run' : run.status === 'failed' ? 'fail' : undefined}>{run.status.replace('_', ' ')}</span>{/if}</div>
			{#if run}<p class="mt-1 font-mono text-[.72rem] text-muted">{run.input_tokens + run.output_tokens} tokens · {run.mode}</p>{/if}
			<div class="mt-3 space-y-3">
				{#each tools as t (t.id)}<section class="rounded-control border border-rule-soft bg-paper p-3"><div class="flex items-center gap-2"><Icon name={t.name === 'run_diagnostic' ? 'terminal' : t.name.includes('file') ? 'file' : 'activity'} size={14} class="text-action"/><strong class="min-w-0 flex-1 truncate text-small">{t.title ?? t.name.replaceAll('_', ' ')}</strong><span class="text-[.7rem] text-muted">{t.status}</span></div>{#if t.args}<details class="mt-2"><summary class="cursor-pointer text-small text-muted">Arguments</summary><pre class="mt-1 overflow-auto rounded bg-paper-2 p-2 font-mono text-[.7rem]">{t.args}</pre></details>{/if}{#if t.output}<details class="mt-2" open={t.status === 'failed'}><summary class="cursor-pointer text-small text-muted">Output{t.exit != null ? ` · exit ${t.exit}` : ''}</summary><pre class="mt-1 max-h-48 overflow-auto whitespace-pre-wrap rounded bg-paper-2 p-2 font-mono text-[.7rem]">{t.output}</pre></details>{/if}{#if t.approval}<div class="mt-3 flex gap-2"><button class="btn btn-sm btn-primary" onclick={() => decide(t, true)}>Approve</button><button class="btn btn-sm" onclick={() => decide(t, false)}>Reject</button></div>{/if}{#if t.secure}<div class="mt-3 space-y-2">{#each t.names ?? [] as n}<label class="block"><span class="label font-mono">{n}</span><input class="field" type="password" autocomplete="new-password" value={secureValues[t.id]?.[n] ?? ''} oninput={(e) => { secureValues[t.id] ??= {}; secureValues[t.id][n] = e.currentTarget.value; }} /></label>{/each}<button class="btn btn-sm btn-primary" onclick={() => secure(t)}>Configure securely</button></div>{/if}</section>{/each}
				{#each changes as ch (ch.id)}<section class="rounded-control border border-action/30 bg-action/5 p-3"><div class="flex items-center gap-2"><Icon name="commit" class="text-action"/><strong class="flex-1 text-small">{ch.summary}</strong><span class="pill">{ch.status}</span></div>{#each ch.files ?? [] as f}<details class="mt-2"><summary class="cursor-pointer font-mono text-small"><span class="mr-1 text-action">{f.operation}</span>{f.path}</summary><pre class="mt-1 max-h-60 overflow-auto whitespace-pre rounded bg-paper p-2 font-mono text-[.68rem]">{f.diff}</pre></details>{/each}{#if ch.status === 'applied'}<button class="btn btn-sm mt-2" onclick={() => undo(ch.id)}><Icon name="history"/>Undo</button>{/if}</section>{/each}
				{#if citations.length}<section class="rounded-control border border-rule-soft bg-paper p-3"><strong class="text-small">Research citations</strong><ol class="mt-2 space-y-2">{#each citations as c}<li><a class="link block truncate text-small" href={c.URL ?? c.url} target="_blank" rel="noopener noreferrer" referrerpolicy="no-referrer">{c.Title ?? c.title ?? c.URL ?? c.url}</a></li>{/each}</ol></section>{/if}
				{#if !run && tools.length === 0}<p class="rounded-control border border-dashed border-rule p-4 text-small text-muted">Plans, tools, approvals, diffs, citations, verification, and rollback appear here.</p>{/if}
			</div>
		</aside>
	</div>
</div>
