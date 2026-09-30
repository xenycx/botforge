// Transient confirmations for completed actions that need no decision.
// Bounded: at most four at a time, oldest dropped first.

export type ToastTone = 'success' | 'info' | 'warn' | 'fail';
export type Toast = { id: number; tone: ToastTone; text: string; action?: { label: string; href?: string; run?: () => void } };

export const toasts = $state<Toast[]>([]);
let seq = 0;
const MAX = 4;

export function toast(text: string, tone: ToastTone = 'success', action?: Toast['action'], ms = tone === 'fail' ? 9000 : 5000) {
	const id = ++seq;
	toasts.push({ id, tone, text, action });
	while (toasts.length > MAX) toasts.shift();
	if (ms > 0) setTimeout(() => dismiss(id), ms);
	return id;
}

export function dismiss(id: number) {
	const i = toasts.findIndex((t) => t.id === id);
	if (i >= 0) toasts.splice(i, 1);
}
