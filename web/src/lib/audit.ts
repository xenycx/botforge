export type AuditEvent = {
	id: number;
	at_ms: number;
	actor: string | null;
	bot_id: string | null;
	bot_name: string | null;
	action: string;
	target: string | null;
	outcome: 'ok' | 'denied' | 'failed';
};
export type AuditPage = { events: AuditEvent[]; next_before?: number };

/** Plain past-tense wording for recorded actions. */
export const actionText: Record<string, string> = {
	'bot.create': 'created the bot',
	'bot.update': 'changed its settings',
	'bot.delete': 'deleted the bot',
	'bot.start': 'started it',
	'bot.stop': 'stopped it',
	'bot.restart': 'restarted it',
	'bot.kill': 'killed it',
	'bot.ports': 'changed published ports',
	'bot.transfer': 'transferred ownership',
	'env.set': 'set variables',
	'env.reveal': 'revealed a variable',
	'env.delete': 'removed a variable',
	'files.write': 'saved a file',
	'files.delete': 'deleted a file or folder',
	'files.mkdir': 'created a folder',
	'files.move': 'moved or renamed a file',
	'files.extract': 'extracted an archive',
	'packages.edit': 'changed packages',
	'deploy.link': 'linked a repository',
	'deploy.unlink': 'unlinked the repository',
	'deploy.start': 'started a deployment',
	'backup.create': 'created a backup',
	'backup.restore': 'restored a backup',
	'backup.delete': 'deleted a backup',
	'backup.label': 'relabeled a backup',
	'access.grant': 'shared the bot',
	'access.revoke': 'removed someone’s access',
	'telemetry.key': 'created a telemetry key',
	'telemetry.revoke': 'revoked the telemetry key',
	'schedule.create': 'added a schedule',
	'schedule.update': 'changed a schedule',
	'schedule.delete': 'removed a schedule',
	'account.sign_in': 'signed in',
	'account.logout': 'signed out',
	'account.password': 'changed their password',
	'account.session_revoke': 'signed out a session',
	'account.sessions_revoke': 'signed out other sessions',
	'account.key_create': 'created an SFTP key',
	'account.key_delete': 'deleted an SFTP key',
	'account.token_create': 'created an API token',
	'account.token_delete': 'deleted an API token',
	'account.disconnect': 'disconnected an account',
	'account.mfa_enable': 'turned on two-step sign-in',
	'account.mfa_disable': 'turned off two-step sign-in',
	'admin.user_create': 'added a user',
	'admin.user_update': 'changed a user'
};
