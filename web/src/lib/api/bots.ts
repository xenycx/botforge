import { api, ApiError } from './client';
import type { Bot } from './types';
import { toast } from '$lib/ui/toast.svelte';
import { confirmDialog } from '$lib/ui/dialogs.svelte';

export type PowerAction = 'start' | 'stop' | 'restart' | 'kill';

const done: Record<PowerAction, string> = {
	start: 'Start requested',
	stop: 'Stop requested',
	restart: 'Restart requested',
	kill: 'Killed'
};

/** Sends a lifecycle request with the shared confirmation and feedback rules. */
export async function power(b: Bot, action: PowerAction): Promise<boolean> {
	if (action === 'kill') {
		const ok = await confirmDialog({
			title: `Kill ${b.name}?`,
			body: 'Kill ends the process immediately with SIGKILL. It gets no chance to save data or close connections cleanly. Use Stop for a normal shutdown.',
			confirmLabel: 'Kill now',
			tone: 'danger'
		});
		if (!ok) return false;
	}
	try {
		await api('POST', `/bots/${b.id}/${action}`);
		toast(`${done[action]}: ${b.name}`, action === 'kill' ? 'warn' : 'success');
		return true;
	} catch (e) {
		toast(`${b.name}: ${e instanceof ApiError ? e.message : 'the request failed'}`, 'fail');
		return false;
	}
}
