import type { Operation } from '$lib/api/types';
import { fmtDuration } from '$lib/args';

export const kindNoun: Record<Operation['kind'], string> = {
	build: 'Build',
	deploy: 'Deployment',
	rollback: 'Rollback',
	backup: 'Backup',
	restore: 'Restore'
};
/** "Deploying music-bot", used while an operation is active. */
export const kindVerb: Record<Operation['kind'], string> = {
	build: 'Building',
	deploy: 'Deploying',
	rollback: 'Rolling back',
	backup: 'Backing up',
	restore: 'Restoring'
};
export const statusText: Record<Operation['status'], string> = {
	queued: 'Waiting',
	running: 'In progress',
	succeeded: 'Succeeded',
	failed: 'Failed',
	cancelled: 'Cancelled',
	interrupted: 'Interrupted'
};
export const triggerText: Record<Operation['trigger'], string> = {
	manual: 'started by hand',
	push: 'after a push to GitHub',
	schedule: 'on schedule',
	initial: 'when the bot was created',
	start: 'when the bot started',
	api: 'through the API',
	system: 'by the panel'
};

export function opTone(o: Operation): 'run' | 'warn' | 'fail' | 'idle' {
	if (o.status === 'succeeded') return 'run';
	if (o.status === 'failed' || o.status === 'interrupted') return 'fail';
	if (o.status === 'cancelled') return 'idle';
	return 'warn';
}

export function elapsed(o: Operation, now = Date.now()): string {
	const from = o.started_at_ms ?? o.created_at_ms;
	const to = o.finished_at_ms ?? now;
	return fmtDuration(Math.max(0, to - from));
}

/** Where to look at an operation in the bot workspace. */
export function opHref(o: Operation): string {
	const tab = o.kind === 'deploy' || o.kind === 'rollback' ? 'deploy' : o.kind === 'backup' || o.kind === 'restore' ? 'backups' : 'overview';
	return `/bots/${o.bot_id}?tab=${tab}&op=${o.id}`;
}

export const active = (o: Operation) => o.status === 'queued' || o.status === 'running';
