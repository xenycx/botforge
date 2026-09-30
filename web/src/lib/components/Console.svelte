<script lang="ts">
	import { theme as uiTheme } from '$lib/ui/theme.svelte';
	// The terminal follows the page's --color-term tokens (dark in both themes).
	function termTheme() {
		const v = (n: string) => getComputedStyle(document.documentElement).getPropertyValue(n).trim();
		return { background: v('--color-term'), foreground: v('--color-term-ink'), selectionBackground: v('--color-action') + '55', cursor: v('--color-action') };
	}
	import { onMount } from 'svelte';
	import { can, Perm, type Bot } from '$lib/api/types';
	import { session } from '$lib/session.svelte';
	import EmptyState from '$lib/components/ui/EmptyState.svelte';
	import Icon from '$lib/components/ui/Icon.svelte';

	let { bot }: { bot: Bot } = $props();
	// Input needs the power permission (the server re-checks it on every line).
	const canInput = $derived(can(bot, Perm.power));

	let host: HTMLDivElement | undefined = $state();
	let link = $state<'connecting' | 'live' | 'reconnecting' | 'ended'>('connecting');
	let notice = $state('');
	let line = $state('');
	let paused = $state(false);
	let dropped = $state(0);
	let copied = $state(false);
	let ws: WebSocket | null = null;

	// Not reactive: internal handles set up once xterm has loaded.
	let sendInput: (text: string) => boolean = () => false;
	let reconnectNow: () => void = () => {};
	let clearView: () => void = () => {};
	let copyAll: () => Promise<void> = async () => {};
	let downloadAll: () => void = () => {};
	let setPaused: (p: boolean) => void = () => {};
	let retheme: () => void = () => {};
	$effect(() => {
		void uiTheme.dark;
		retheme();
	});

	onMount(() => {
		if (!session.features.console) return;
		let term: import('@xterm/xterm').Terminal | undefined;
		let observer: ResizeObserver | undefined;
		let timer: ReturnType<typeof setTimeout> | undefined;
		let stopped = false;
		let attempt = 0;
		let lastTs = '';
		// While paused, output is held (bounded) and written on resume, so the
		// reader's scroll position is not disturbed.
		let held: string[] = [];
		let heldBytes = 0;
		let isPaused = false;
		// A plain-text copy of what was received, bounded, for copy/download.
		let transcript: string[] = [];
		let transcriptBytes = 0;

		const keep = (s: string) => {
			transcript.push(s);
			transcriptBytes += s.length;
			while (transcriptBytes > 1_000_000 && transcript.length) transcriptBytes -= transcript.shift()!.length;
		};
		const write = (s: string) => {
			if (!isPaused) return term?.write(s);
			held.push(s);
			heldBytes += s.length;
			while (heldBytes > 512_000 && held.length) heldBytes -= held.shift()!.length;
		};

		function connect() {
			const proto = location.protocol === 'https:' ? 'wss' : 'ws';
			const q = (lastTs ? `since=${encodeURIComponent(lastTs)}` : 'tail=300') + (canInput ? '' : '&stdin=0');
			const sock = new WebSocket(`${proto}://${location.host}/api/v1/bots/${bot.id}/console?${q}`);
			ws = sock;
			sock.onopen = () => {
				attempt = 0;
				link = 'live';
				notice = '';
			};
			sock.onmessage = (ev) => {
				let m: any;
				try {
					m = JSON.parse(ev.data);
				} catch {
					return;
				}
				switch (m.type) {
					case 'log':
						if (m.ts && m.ts > lastTs) lastTs = m.ts;
						keep(m.data);
						write(m.stream === 'stderr' ? `\x1b[38;5;217m${m.data}\x1b[0m` : m.data);
						break;
					case 'dropped':
						dropped += m.count;
						write(`\r\n\x1b[2m[${m.count} lines skipped: output was faster than this connection]\x1b[0m\r\n`);
						break;
					case 'error':
						notice = m.message ?? 'The console reported an error.';
						break;
				}
			};
			sock.onclose = (ev) => {
				ws = null;
				if (stopped) return;
				if (ev.code === 1008) {
					link = 'ended';
					notice = 'This console session ended because your access changed or you signed out. Reload the page to reconnect.';
					return;
				}
				link = 'reconnecting';
				const delay = Math.min(1000 * 2 ** attempt++, 15000);
				timer = setTimeout(connect, delay);
			};
		}

		sendInput = (text) => {
			if (ws?.readyState !== WebSocket.OPEN) return false;
			ws.send(JSON.stringify({ type: 'stdin', data: text }));
			return true;
		};
		reconnectNow = () => {
			clearTimeout(timer);
			attempt = 0;
			if (ws) ws.close();
			else connect();
			link = 'connecting';
		};
		clearView = () => term?.clear();
		copyAll = async () => {
			await navigator.clipboard.writeText(transcript.join('').replace(/\x1b\[[0-9;?]*[ -/]*[@-~]/g, ''));
		};
		downloadAll = () => {
			const blob = new Blob([transcript.join('').replace(/\x1b\[[0-9;?]*[ -/]*[@-~]/g, '')], { type: 'text/plain' });
			const a = document.createElement('a');
			a.href = URL.createObjectURL(blob);
			a.download = `${bot.name.replace(/[^\w.-]+/g, '_')}-console.log`;
			a.click();
			setTimeout(() => URL.revokeObjectURL(a.href), 1000);
		};
		setPaused = (p) => {
			isPaused = p;
			if (!p && held.length) {
				term?.write(held.join(''));
				held = [];
				heldBytes = 0;
			}
		};

		(async () => {
			const [{ Terminal }, { FitAddon }] = await Promise.all([import('@xterm/xterm'), import('@xterm/addon-fit')]);
			await import('@xterm/xterm/css/xterm.css');
			if (stopped) return;
			term = new Terminal({
				convertEol: true,
				disableStdin: true, // input goes to the bot's stdin through the field below, not a shell
				scrollback: 5000,
				fontFamily: getComputedStyle(document.documentElement).getPropertyValue('--font-mono'),
				fontSize: 13,
				lineHeight: 1.25,
				theme: termTheme()
			});
			const fit = new FitAddon();
			term.loadAddon(fit);
			if (!host) return;
			term.open(host);
			retheme = () => term && (term.options.theme = termTheme());
			fit.fit();
			observer = new ResizeObserver(() => fit.fit());
			observer.observe(host);
			connect();
		})();

		return () => {
			stopped = true;
			clearTimeout(timer);
			observer?.disconnect();
			ws?.close();
			term?.dispose();
		};
	});

	function send(e: SubmitEvent) {
		e.preventDefault();
		if (line === '') return;
		if (sendInput(line + '\n')) line = '';
	}
	function togglePause() {
		paused = !paused;
		setPaused(paused);
	}
	async function copy() {
		try {
			await copyAll();
			copied = true;
			setTimeout(() => (copied = false), 1500);
		} catch {
			notice = 'Copying needs clipboard access; use Download instead.';
		}
	}

	const linkText = $derived({ connecting: 'Connecting', live: 'Live', reconnecting: 'Reconnecting', ended: 'Disconnected' }[link]);
	const linkColor = $derived({ connecting: 'text-warn', live: 'text-run', reconnecting: 'text-warn', ended: 'text-fail' }[link]);
</script>

{#if !session.features.console}
	<EmptyState title="Live output needs the Docker runner">
		<p>This panel runs without Docker (BOTPANEL_RUNNER_MODE=none), so bots cannot run here and there is no output to show.</p>
	</EmptyState>
{:else}

<div class="flex flex-wrap items-center gap-2 border border-b-0 border-rule-soft bg-panel px-2 py-1.5">
	<span class="inline-flex items-center gap-1.5 text-small font-medium {linkColor}" aria-live="polite">
		<span class="size-2 rounded-pill bg-current" aria-hidden="true"></span>{linkText}{#if paused}<span class="text-muted">, paused</span>{/if}
	</span>
	{#if dropped}<span class="text-small text-muted">{dropped} lines skipped</span>{/if}
	<span class="flex-1"></span>
	<button class="btn btn-sm btn-quiet" aria-pressed={paused} onclick={togglePause} title="Hold new output so you can read">
		<Icon name={paused ? 'play' : 'pause'} size={13} />{paused ? 'Resume' : 'Pause'}
	</button>
	<button class="btn btn-sm btn-quiet" onclick={() => clearView()} title="Clear this view (the bot is not affected)">Clear</button>
	<button class="btn btn-sm btn-quiet" onclick={copy}><Icon name={copied ? 'check' : 'copy'} size={13} />{copied ? 'Copied' : 'Copy'}</button>
	<button class="btn btn-sm btn-quiet" onclick={() => downloadAll()}><Icon name="download" size={13} />Download</button>
	{#if link !== 'live'}<button class="btn btn-sm" onclick={() => reconnectNow()}>Reconnect</button>{/if}
</div>
<div bind:this={host} class="h-[max(20rem,calc(100dvh-26rem))] overflow-hidden bg-term p-2" aria-label="Bot output" role="log"></div>
{#if notice}<p class="mt-2 text-small text-warn" role="status">{notice}</p>{/if}
{#if canInput}
	<form class="mt-2 flex gap-2" onsubmit={send}>
		<label class="sr-only" for="stdin">Send a line to the bot</label>
		<input id="stdin" class="field font-mono" placeholder="Send a line to the bot's standard input" autocomplete="off" spellcheck="false" bind:value={line} disabled={link !== 'live'} />
		<button class="btn" disabled={link !== 'live' || line === ''}>Send</button>
	</form>
	<p class="mt-1 text-small text-muted">Input goes to the bot process, not to a shell. One console at a time can send input. Closing this page does not stop the bot.</p>
{:else}
	<p class="mt-2 text-small text-muted">You can watch the output. Sending input needs the start and stop permission. Closing this page does not stop the bot.</p>
{/if}
{/if}
