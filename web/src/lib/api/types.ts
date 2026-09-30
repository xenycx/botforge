export type User = { id: string; email: string; display_name: string; avatar_url: string; role: 'admin' | 'user'; disabled: boolean; created_at_ms: number };

export type Bot = {
	id: string;
	owner_id: string;
	node_id: string;
	name: string;
	runtime: string;
	image_ref: string;
	argv: string[];
	memory_bytes: number;
	nano_cpus: number;
	pids_limit: number;
	desired_state: 'stopped' | 'running' | 'deleted';
	observed_state: 'unknown' | 'stopped' | 'building' | 'starting' | 'running' | 'stopping' | 'failed';
	generation: number;
	observed_generation: number;
	last_exit_code: number | null;
	last_error: string | null;
	created_at_ms: number;
	updated_at_ms: number;
	permissions: number;
	shared: boolean;
	entrypoint: string[];
	source_type: 'manual' | 'template' | 'github';
	template_id: string | null;
	network_enabled: boolean;
	bandwidth_kbps: number | null;
	ports: Port[];
	auto_backup: boolean;
	restart_policy: 'never' | 'on_failure';
	restart_max_attempts: number;
	restart_backoff_initial_ms: number;
	restart_backoff_max_ms: number;
	phase: Phase;
	state_reason: string | null;
	restart_count: number;
	next_retry_at_ms: number | null;
	last_started_at_ms: number | null;
	tags: string[];
	favorite: boolean;
};

/** Server-derived lifecycle word (internal/api phaseOf). */
export type Phase =
	| 'deleting'
	| 'checking'
	| 'no_runner'
	| 'runner_offline'
	| 'queued'
	| 'building'
	| 'starting'
	| 'running'
	| 'restarting'
	| 'retrying'
	| 'failed'
	| 'exited'
	| 'stopping'
	| 'stopped';

export type Port = { container_port: number; host_port: number; protocol: 'tcp' | 'udp'; host_ip: string };

/** Sub-user permission bits (mirror internal/domain). */
export const Perm = { console: 1, power: 2, files: 4, env: 8, admin: 16 } as const;
export const can = (b: { permissions: number }, p: number) => (b.permissions & Perm.admin) !== 0 || (b.permissions & p) === p;

export type RuntimeInfo = {
	id: string;
	display_name: string;
	image_ref: string;
	default_argv: string[];
	default_memory_bytes: number;
	default_nano_cpus: number;
	default_pids_limit: number;
	min_memory_bytes: number;
	build_memory_bytes: number;
	has_build: boolean;
};

export type Limits = {
	min_memory_bytes: number;
	max_memory_bytes: number;
	min_nano_cpus: number;
	max_nano_cpus: number;
	port_min: number;
	port_max: number;
	port_public_bind: boolean;
};

export type EnvVar = { name: string; value: string; updated_at_ms: number };
export type FileEntry = { name: string; type: 'file' | 'dir' | 'symlink'; size: number };

export type Sample = {
	sampled_at_ms: number;
	cpu_percent: number;
	logical_cpus: number;
	memory_used_bytes: number;
	memory_total_bytes: number;
	disk_used_bytes: number;
	disk_total_bytes: number;
	running_bots: number;
};
export type NodeInfo = { id: string; name: string; transport: string; enabled: boolean; latest?: Sample };

export type Provider = 'github' | 'discord';
export type Connection = {
	provider: Provider;
	configured: boolean;
	linked: boolean;
	username: string;
	avatar_url: string;
	notifications: boolean;
	repo_access: boolean;
	can_disconnect: boolean;
};

/** Human text for the `?error=` codes the OAuth callback redirects with. */
export const oauthErrors: Record<string, string> = {
	denied: 'Sign-in was cancelled at the provider.',
	state_invalid: 'That sign-in link expired or was already used. Please try again.',
	exchange_failed: 'The provider rejected the sign-in. Please try again.',
	signup_disabled: 'No account is linked to that identity. Ask an administrator for an account, then connect it in Settings.',
	email_in_use: 'An account with that email already exists. Sign in with your password, then connect the provider in Settings.',
	email_unverified: 'The provider did not share a verified email address.',
	already_linked: 'That account is already connected to a different BotForge user.',
	account_disabled: 'This account is disabled.',
	server_error: 'Something went wrong on the server. Please try again.'
};

export type TemplateEnv = { name: string; label: string; description: string; secret: boolean; required: boolean; default?: string };
export type Template = {
	id: string;
	name: string;
	description: string;
	runtime: string;
	language: string;
	version: number;
	tested_with: string;
	first_start: string;
	privileged_intents: string[];
	env: TemplateEnv[];
	setup: string[];
	default_memory_bytes: number;
	build_memory_bytes: number;
	has_build: boolean;
};
export type GitHubRepo = { full_name: string; private: boolean; default_branch: string };
export type RepoLink = {
	full_name: string;
	branch: string;
	root_dir: string;
	private: boolean;
	auto_deploy: boolean;
	hook_created: boolean;
	webhook_url: string;
	secret?: string;
	last_sha: string;
	last_deployed_at_ms: number;
	last_error: string;
	deploying: boolean;
};
export type Backup = {
	id: string;
	kind: 'manual' | 'auto' | 'pre_restore';
	status: 'creating' | 'ready' | 'failed';
	size_bytes: number;
	sha256: string | null;
	includes_env: boolean;
	error: string | null;
	created_at_ms: number;
	label: string | null;
	verified_at_ms: number | null;
	verify_error: string | null;
	consistent: boolean;
};
export type BackupHealth = {
	interval_ms: number;
	keep: number;
	enabled: boolean;
	last_success_ms: number;
	last_scheduled_ms: number;
	next_due_ms: number;
	last_failure: Backup | null;
	total_bytes: number;
	count: number;
	limit: number;
	manual_limit: number;
};
export type SubUser = { user_id: string; email: string; permissions: number; created_at_ms: number };
export type Dep = { name: string; spec: string; group: string; editable: boolean };
export type Packages = { supported: boolean; ecosystem?: string; file?: string; exists: boolean; groups?: string[]; deps: Dep[] };
export type PkgResult = { name: string; version: string; description: string };
export type Series = { name: string; latest: number; points: { t: number; v: number }[] };
export type DashboardWidget = { key: string; kind: 'metric'|'status'|'progress'|'text'|'chart'|'table'|'link'; title: string; position: number; data: Record<string, unknown>; updated_at_ms: number };
export type Analytics = {
	key_set: boolean;
	window_ms: number;
	last_at_ms: number;
	stats: Series[];
	commands: { name: string; count: number }[];
	events: { t: number; name: string; data?: unknown }[];
	widgets: DashboardWidget[];
};
export type Gauge = {
	running: boolean;
	cpu_cores: number;
	cpu_limit_cores: number;
	cpu_percent: number;
	mem_used_bytes: number;
	mem_limit_bytes: number;
	pids: number;
	net_rx_bytes: number;
	net_tx_bytes: number;
	disk_used_bytes: number;
	disk_total_bytes: number;
	disk_free_bytes: number;
};
export type ApiKey = { id: string; name: string; prefix: string; created_at_ms: number; last_used_at_ms: number | null; expires_at_ms: number | null };

export type OpKind = 'build' | 'deploy' | 'rollback' | 'backup' | 'restore';
export type OpStatus = 'queued' | 'running' | 'succeeded' | 'failed' | 'cancelled' | 'interrupted';
export type Operation = {
	id: string;
	bot_id: string;
	bot_name: string;
	kind: OpKind;
	trigger: 'manual' | 'push' | 'schedule' | 'initial' | 'start' | 'api' | 'system';
	actor: string | null;
	status: OpStatus;
	stage: string;
	source_ref: string | null;
	source_label: string | null;
	generation: number | null;
	result_code: string | null;
	message: string | null;
	detail?: Record<string, unknown>;
	log_bytes: number;
	created_at_ms: number;
	started_at_ms: number | null;
	finished_at_ms: number | null;
};
export type OpPage = { operations: Operation[]; next_before?: number };
export type OutputChunk = { text: string; next_offset: number; total: number; truncated: boolean; live: boolean };

export type ScheduleAction = 'backup' | 'start' | 'stop' | 'restart' | 'deploy';
export type Schedule = {
	id: string;
	action: ScheduleAction;
	spec: string;
	timezone: string;
	enabled: boolean;
	owner_email: string;
	next_run_at_ms: number | null;
	last_run_at_ms: number | null;
	last_status: 'ok' | 'failed' | 'skipped' | 'missed' | 'denied' | null;
	last_message: string | null;
	upcoming: number[];
	can_edit: boolean;
	created_at_ms: number;
};

export type Capacity = {
	bots: number;
	max_bots: number;
	memory_bytes: number;
	max_memory_bytes: number;
	exempt: boolean;
	build_memory_bytes: number;
	node?: { running: number; reserved_bytes: number; budget_bytes: number };
};
