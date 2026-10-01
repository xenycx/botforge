import { goto } from '$app/navigation';
import { api, setCsrf, setUnauthorizedHandler } from '$lib/api/client';
import type { User } from '$lib/api/types';

export type Features = { runner: boolean; console: boolean; stats: boolean; files: boolean; deploy: boolean; backups: boolean; analytics: boolean; sftp: boolean; operations: boolean; oauth: boolean; schedules: boolean; mfa: boolean; automation: boolean; health: boolean; sites: boolean; workspaces: boolean; ai: boolean };
const allOn: Features = { runner: true, console: true, stats: true, files: true, deploy: true, backups: true, analytics: true, sftp: true, operations: true, oauth: true, schedules: true, mfa: true, automation: true, health: true, sites: false, workspaces: true, ai: true };
export const session = $state<{ user: User | null; loaded: boolean; hasPassword: boolean; features: Features }>({ user: null, loaded: false, hasPassword: true, features: allOn });

setUnauthorizedHandler(() => {
	if (session.user) {
		session.user = null;
		goto('/login');
	}
});

export async function loadSession() {
	try {
		const r = await api<{ user: User; csrf_token: string; has_password: boolean; features: Features }>('GET', '/auth/me');
		setCsrf(r.csrf_token);
		session.user = r.user;
		session.hasPassword = r.has_password;
		session.features = { ...allOn, ...r.features };
	} catch {
		session.user = null;
	} finally {
		session.loaded = true;
	}
}

/** Signs in with a password. Returns 'mfa' when a second step is needed. */
export async function login(email: string, password: string): Promise<'ok' | 'mfa'> {
	const r = await api<{ user?: User; csrf_token?: string; mfa_required?: boolean }>('POST', '/auth/login', { email, password });
	if (r.mfa_required) return 'mfa';
	await loadSession(); // picks up features and account details
	return 'ok';
}

/** Second step of a sign-in: an authenticator code or a recovery code. */
export async function verifyMFA(code: string) {
	await api('POST', '/auth/mfa', { code });
	await loadSession();
}

export async function logout() {
	try {
		await api('POST', '/auth/logout');
	} finally {
		session.user = null;
		setCsrf('');
		goto('/login');
	}
}

// Where to go after signing in (an invitation link, a deep link). Kept in
// sessionStorage so tokens in URL fragments never reach a server or a query.
const NEXT = 'botpanel.next';
export function rememberNext(path: string) {
	try {
		if (path.startsWith('/') && !path.startsWith('//') && !path.startsWith('/login')) sessionStorage.setItem(NEXT, path);
	} catch {
		/* storage unavailable: land on the home page */
	}
}
export function takeNext(): string {
	try {
		const p = sessionStorage.getItem(NEXT);
		sessionStorage.removeItem(NEXT);
		if (p && p.startsWith('/') && !p.startsWith('//')) return p;
	} catch {
		/* ignore */
	}
	return '/dashboard';
}
