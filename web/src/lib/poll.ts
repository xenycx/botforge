/** Runs fn now and then every `ms` while the page is visible; a hidden tab does
 * not poll and catches up as soon as it becomes visible again. Returns a stop
 * function for onMount's cleanup. */
export function poll(fn: () => void | Promise<void>, ms: number): () => void {
	let timer: ReturnType<typeof setInterval> | undefined;
	let running = false;
	const tick = async () => {
		if (running) return; // a slow request must not pile up
		running = true;
		try {
			await fn();
		} finally {
			running = false;
		}
	};
	const start = () => {
		if (timer) return;
		void tick();
		timer = setInterval(() => void tick(), ms);
	};
	const stop = () => {
		if (timer) clearInterval(timer);
		timer = undefined;
	};
	const onVis = () => (document.visibilityState === 'visible' ? start() : stop());
	if (document.visibilityState === 'visible') start();
	document.addEventListener('visibilitychange', onVis);
	return () => {
		stop();
		document.removeEventListener('visibilitychange', onVis);
	};
}
