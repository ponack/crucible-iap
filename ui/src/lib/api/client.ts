// Typed API client for the Crucible backend.
import { auth } from '$lib/stores/auth.svelte';
import { decodeJWTPayload } from '$lib/jwt';

const BASE = '/api/v1';

async function request<T>(path: string, init: RequestInit = {}, retry = true): Promise<T> {
	const headers: Record<string, string> = {
		'Content-Type': 'application/json',
		...(init.headers as Record<string, string>)
	};

	if (auth.accessToken) {
		headers['Authorization'] = `Bearer ${auth.accessToken}`;
	}

	const res = await fetch(BASE + path, { ...init, headers });

	// Attempt silent token refresh on 401, once.
	if (res.status === 401 && retry) {
		const refreshed = await tryRefresh();
		if (refreshed) return request<T>(path, init, false);
		auth.clear();
		window.location.href = '/login';
		throw new Error('Unauthorized');
	}

	if (res.status === 401) {
		auth.clear();
		window.location.href = '/login';
		throw new Error('Unauthorized');
	}

	if (!res.ok) {
		const err = await res.json().catch(() => ({ error: res.statusText }));
		throw new Error(err.error ?? 'Request failed');
	}

	if (res.status === 204) return null as T;
	return res.json() as Promise<T>;
}

// tryRefresh silently exchanges the httpOnly refresh cookie for a new access token.
// Exported so the layout can call it on startup to restore the session after a page reload.
export async function tryRefresh(): Promise<boolean> {
	try {
		const res = await fetch('/auth/refresh', { method: 'POST' });
		if (!res.ok) return false;
		const { access_token } = await res.json();
		try {
			const payload = decodeJWTPayload(access_token);
			auth.setTokens(access_token, {
				id: payload.uid,
				email: payload.email,
				name: payload.name,
				is_admin: false
			});
		} catch {
			auth.setAccessToken(access_token);
		}
		return true;
	} catch {
		return false;
	}
}

// ── Pagination ────────────────────────────────────────────────────────────────

export interface PageMeta {
	limit: number;
	offset: number;
	total: number;
	has_more: boolean;
}

export interface Paginated<T> {
	data: T[];
	pagination: PageMeta;
}

// ── Stacks ────────────────────────────────────────────────────────────────────

export interface Stack {
	id: string;
	org_id: string;
	slug: string;
	name: string;
	description?: string;
	tool: 'opentofu' | 'terraform' | 'ansible' | 'pulumi';
	tool_version?: string;
	repo_url: string;
	repo_branch: string;
	project_root: string;
	runner_image?: string;
	auto_apply: boolean;
	drift_detection: boolean;
	drift_schedule?: string;
	auto_remediate_drift: boolean;
	vcs_provider: 'github' | 'gitlab' | 'gitea';
	vcs_base_url?: string;
	has_vcs_token: boolean;
	has_slack_webhook: boolean;
	gotify_url?: string;
	has_gotify_token: boolean;
	ntfy_url?: string;
	has_ntfy_token: boolean;
	notify_email?: string;
	notify_events: string[];
	vcs_integration_id?: string;
	secret_integration_id?: string;
	has_state_backend: boolean;
	state_backend_provider?: string;
	is_disabled: boolean;
	is_locked: boolean;
	lock_reason?: string;
	scheduled_destroy_at?: string;
	plan_schedule?: string;
	apply_schedule?: string;
	destroy_schedule?: string;
	plan_next_run_at?: string;
	apply_next_run_at?: string;
	destroy_next_run_at?: string;
	is_restricted: boolean;    // true = stack has explicit members configured
	my_stack_role: 'admin' | 'approver' | 'viewer';
	last_run_status?: string;
	last_run_at?: string;
	upstream_count: number;
	downstream_count: number;
	upstream_stacks: { id: string; name: string }[];
	downstream_stacks: { id: string; name: string }[];
	module_namespace?: string;
	module_name?: string;
	module_provider?: string;
	pre_plan_hook?: string;
	post_plan_hook?: string;
	pre_apply_hook?: string;
	post_apply_hook?: string;
	max_concurrent_runs?: number;
	pr_preview_enabled: boolean;
	pr_preview_template_id?: string;
	is_preview: boolean;
	preview_source_stack_id?: string;
	preview_pr_number?: number;
	preview_pr_url?: string;
	preview_branch?: string;
	worker_pool_id?: string;
	worker_pool_name?: string;
	created_at: string;
	updated_at: string;
}

// ── Worker pools ──────────────────────────────────────────────────────────────

export interface WorkerPool {
	id: string;
	name: string;
	description: string;
	capacity: number;
	is_disabled: boolean;
	last_seen_at?: string;
	created_at: string;
}

export const workerPools = {
	list: () => request<Paginated<WorkerPool>>('/worker-pools'),
	create: (body: { name: string; description?: string; capacity?: number }) =>
		request<{ pool: WorkerPool; token: string }>('/worker-pools', { method: 'POST', body: JSON.stringify(body) }),
	get: (id: string) => request<WorkerPool>(`/worker-pools/${id}`),
	update: (id: string, body: Partial<{ description: string; capacity: number; is_disabled: boolean }>) =>
		request<void>(`/worker-pools/${id}`, { method: 'PATCH', body: JSON.stringify(body) }),
	delete: (id: string) => request<void>(`/worker-pools/${id}`, { method: 'DELETE' }),
	rotateToken: (id: string) => request<{ token: string }>(`/worker-pools/${id}/rotate-token`, { method: 'POST' })
};

// ── Org integrations ──────────────────────────────────────────────────────────

export type IntegrationType =
	| 'github' | 'gitlab' | 'gitea'       // VCS
	| 'aws_sm' | 'hc_vault' | 'bitwarden_sm' | 'vaultwarden'; // Secret stores

export interface Integration {
	id: string;
	name: string;
	type: IntegrationType;
	created_at: string;
	updated_at: string;
}

export interface VCSIntegrationConfig {
	token: string;
}

export interface AWSSecretStoreConfig {
	region: string;
	access_key_id?: string;
	secret_access_key?: string;
	secret_names: string[];
}

export interface HCVaultSecretStoreConfig {
	address: string;
	namespace?: string;
	token?: string;
	role_id?: string;
	secret_id?: string;
	mount: string;
	path: string;
}

export interface BitwardenSecretStoreConfig {
	access_token: string;
	project_id?: string;
	org_id?: string;
	api_url?: string;
	identity_url?: string;
}

export interface VaultwardenSecretStoreConfig {
	url: string;
	client_id: string;
	client_secret: string;
	email: string;
	master_password: string;
	folder_name?: string;
}

// ── External state backend ────────────────────────────────────────────────────

export type StateBackendProvider = 's3' | 'gcs' | 'azurerm';

export interface StateBackendInfo {
	provider: StateBackendProvider;
}

export interface S3StateBackendConfig {
	region: string;
	bucket: string;
	key_prefix?: string;
	access_key_id?: string;
	secret_access_key?: string;
	endpoint_url?: string;
}

export interface GCSStateBackendConfig {
	bucket: string;
	key_prefix?: string;
	service_account_json: string;
}

export interface AzureStateBackendConfig {
	account_name: string;
	account_key: string;
	container: string;
	key_prefix?: string;
}

export interface StackToken {
	id: string;
	stack_id: string;
	name: string;
	secret?: string; // only present on creation
	created_at: string;
	last_used?: string;
}

export const stacks = {
	list: (offset = 0, limit = 50, filters: { q?: string; tool?: string; status?: string } = {}) => {
		const p = new URLSearchParams({ limit: String(limit), offset: String(offset) });
		if (filters.q) p.set('q', filters.q);
		if (filters.tool) p.set('tool', filters.tool);
		if (filters.status) p.set('status', filters.status);
		return request<Paginated<Stack>>(`/stacks?${p}`);
	},
	get: (id: string) => request<Stack>(`/stacks/${id}`),
	create: (data: Partial<Stack>) =>
		request<Stack>('/stacks', { method: 'POST', body: JSON.stringify(data) }),
	update: (id: string, data: Partial<Stack>) =>
		request<Stack>(`/stacks/${id}`, { method: 'PATCH', body: JSON.stringify(data) }),
	delete: (id: string) => request<null>(`/stacks/${id}`, { method: 'DELETE' }),

	tokens: {
		list: (stackID: string) => request<StackToken[]>(`/stacks/${stackID}/tokens`),
		create: (stackID: string, name: string) =>
			request<StackToken>(`/stacks/${stackID}/tokens`, {
				method: 'POST',
				body: JSON.stringify({ name })
			}),
		revoke: (stackID: string, tokenID: string) =>
			request<null>(`/stacks/${stackID}/tokens/${tokenID}`, { method: 'DELETE' })
	},

	env: {
		list: (stackID: string) => request<StackEnvVar[]>(`/stacks/${stackID}/env`),
		upsert: (stackID: string, name: string, value: string, isSecret = true) =>
			request<StackEnvVar>(`/stacks/${stackID}/env`, {
				method: 'PUT',
				body: JSON.stringify({ name, value, is_secret: isSecret })
			}),
		delete: (stackID: string, name: string) =>
			request<null>(`/stacks/${stackID}/env/${encodeURIComponent(name)}`, { method: 'DELETE' })
	},

	notifications: {
		update: (
			stackID: string,
			data: {
				vcs_provider?: string;
				vcs_base_url?: string;
				vcs_token?: string;
				slack_webhook?: string;
				gotify_url?: string;
				gotify_token?: string;
				ntfy_url?: string;
				ntfy_token?: string;
				notify_events?: string[];
			}
		) =>
			request<null>(`/stacks/${stackID}/notifications`, {
				method: 'PUT',
				body: JSON.stringify(data)
			}),
		test: (stackID: string) =>
			request<null>(`/stacks/${stackID}/notifications/test`, { method: 'POST' }),
		testGotify: (stackID: string) =>
			request<null>(`/stacks/${stackID}/notifications/test-gotify`, { method: 'POST' }),
		testNtfy: (stackID: string) =>
			request<null>(`/stacks/${stackID}/notifications/test-ntfy`, { method: 'POST' }),
		testEmail: (stackID: string) =>
			request<null>(`/stacks/${stackID}/notifications/test-email`, { method: 'POST' })
	},

	integrations: {
		set: (stackID: string, vcsIntegrationID: string | null, secretIntegrationID: string | null) =>
			request<null>(`/stacks/${stackID}/integrations`, {
				method: 'PUT',
				body: JSON.stringify({
					vcs_integration_id: vcsIntegrationID,
					secret_integration_id: secretIntegrationID
				})
			})
	},

	stateBackend: {
		get: (stackID: string) => request<StateBackendInfo>(`/stacks/${stackID}/state-backend`),
		upsert: (
			stackID: string,
			provider: StateBackendProvider,
			config: S3StateBackendConfig | GCSStateBackendConfig | AzureStateBackendConfig
		) =>
			request<StateBackendInfo>(`/stacks/${stackID}/state-backend`, {
				method: 'PUT',
				body: JSON.stringify({ provider, config })
			}),
		delete: (stackID: string) =>
			request<null>(`/stacks/${stackID}/state-backend`, { method: 'DELETE' })
	},

	lock: (stackID: string, reason?: string) =>
		request<null>(`/stacks/${stackID}/lock`, {
			method: 'POST',
			body: JSON.stringify({ reason: reason ?? '' })
		}),
	unlock: (stackID: string) =>
		request<null>(`/stacks/${stackID}/unlock`, { method: 'POST' }),

	webhook: {
		rotateSecret: (stackID: string) =>
			request<{ webhook_secret: string }>(`/stacks/${stackID}/webhook/rotate`, { method: 'POST' }),
		deliveries: (stackID: string, offset = 0, limit = 50) =>
			request<Paginated<WebhookDelivery>>(`/stacks/${stackID}/webhook-deliveries?offset=${offset}&limit=${limit}`),
		deliveryPayload: (stackID: string, deliveryID: string) =>
			request<{ payload: unknown }>(`/stacks/${stackID}/webhook-deliveries/${deliveryID}/payload`),
		redeliver: (stackID: string, deliveryID: string) =>
			request<{ run_id: string }>(`/stacks/${stackID}/webhook-deliveries/${deliveryID}/redeliver`, { method: 'POST' })
	},

	outgoingWebhooks: {
		list: (stackID: string) =>
			request<OutgoingWebhook[]>(`/stacks/${stackID}/outgoing-webhooks`),
		create: (stackID: string, data: { url: string; event_types: string[]; headers: Record<string, string>; with_secret: boolean }) =>
			request<OutgoingWebhook>(`/stacks/${stackID}/outgoing-webhooks`, {
				method: 'POST',
				body: JSON.stringify(data)
			}),
		update: (stackID: string, whID: string, data: { url?: string; event_types?: string[]; headers?: Record<string, string>; is_active?: boolean }) =>
			request<null>(`/stacks/${stackID}/outgoing-webhooks/${whID}`, {
				method: 'PATCH',
				body: JSON.stringify(data)
			}),
		rotateSecret: (stackID: string, whID: string) =>
			request<{ secret: string }>(`/stacks/${stackID}/outgoing-webhooks/${whID}/rotate-secret`, { method: 'POST' }),
		delete: (stackID: string, whID: string) =>
			request<null>(`/stacks/${stackID}/outgoing-webhooks/${whID}`, { method: 'DELETE' }),
		deliveries: (stackID: string, whID: string) =>
			request<OutgoingWebhookDelivery[]>(`/stacks/${stackID}/outgoing-webhooks/${whID}/deliveries`)
	},

	remoteState: {
		list: (stackID: string) =>
			request<RemoteStateSource[]>(`/stacks/${stackID}/remote-state-sources`),
		add: (stackID: string, sourceStackID: string) =>
			request<RemoteStateSource>(`/stacks/${stackID}/remote-state-sources`, {
				method: 'POST',
				body: JSON.stringify({ source_stack_id: sourceStackID })
			}),
		remove: (stackID: string, sourceID: string) =>
			request<null>(`/stacks/${stackID}/remote-state-sources/${sourceID}`, { method: 'DELETE' })
	},
	state: {
		resources: (stackID: string) => request<StateResource[]>(`/stacks/${stackID}/state/resources`),
		forceUnlock: (stackID: string) => request<{ cleared_lock_id: string }>(`/stacks/${stackID}/lock`, { method: 'DELETE' })
	}
};

// ── Integrations (org-level) ──────────────────────────────────────────────────

type IntegrationConfig =
	| VCSIntegrationConfig
	| AWSSecretStoreConfig
	| HCVaultSecretStoreConfig
	| BitwardenSecretStoreConfig
	| VaultwardenSecretStoreConfig;

export const integrations = {
	list: () => request<Integration[]>('/integrations'),
	create: (name: string, type: IntegrationType, config: IntegrationConfig) =>
		request<Integration>('/integrations', {
			method: 'POST',
			body: JSON.stringify({ name, type, config })
		}),
	update: (id: string, data: { name?: string; config?: IntegrationConfig }) =>
		request<Integration>(`/integrations/${id}`, {
			method: 'PUT',
			body: JSON.stringify(data)
		}),
	delete: (id: string) => request<null>(`/integrations/${id}`, { method: 'DELETE' })
};

// ── Stack env vars ────────────────────────────────────────────────────────────

export interface StackEnvVar {
	id: string;
	name: string;
	is_secret: boolean;
	value?: string;
	created_at: string;
	updated_at: string;
}

// ── Stack members (per-stack RBAC) ───────────────────────────────────────────

export interface StackMember {
	user_id: string;
	email: string;
	name: string;
	role: 'viewer' | 'approver';
	created_at: string;
}

export const stackMembers = {
	list: (stackID: string) =>
		request<StackMember[]>(`/stacks/${stackID}/members`),
	upsert: (stackID: string, userID: string, role: 'viewer' | 'approver') =>
		request<null>(`/stacks/${stackID}/members/${userID}`, {
			method: 'PUT',
			body: JSON.stringify({ role })
		}),
	remove: (stackID: string, userID: string) =>
		request<null>(`/stacks/${stackID}/members/${userID}`, { method: 'DELETE' })
};

// ── Runs ──────────────────────────────────────────────────────────────────────

export interface Run {
	id: string;
	stack_id: string;
	stack_name?: string; // populated by listAll
	status:
		| 'queued'
		| 'preparing'
		| 'planning'
		| 'unconfirmed'
		| 'pending_approval'
		| 'confirmed'
		| 'applying'
		| 'finished'
		| 'failed'
		| 'canceled'
		| 'discarded';
	type: 'tracked' | 'proposed' | 'destroy';
	trigger: string;
	commit_sha?: string;
	commit_message?: string;
	branch?: string;
	is_drift: boolean;
	pr_number?: number;
	pr_url?: string;
	plan_add?: number;
	plan_change?: number;
	plan_destroy?: number;
	cost_add?: number;
	cost_change?: number;
	cost_remove?: number;
	cost_currency?: string;
	has_plan?: boolean;
	triggered_by_name?: string;
	triggered_by_email?: string;
	approved_by_name?: string;
	approved_by_email?: string;
	approved_at?: string;
	queued_at: string;
	started_at?: string;
	finished_at?: string;
	var_overrides?: string[]; // KEY=value pairs; only present on Get, not list responses
	annotation?: string;
	my_stack_role?: 'admin' | 'approver' | 'viewer'; // caller's effective level; only on Get
}

export interface RunPolicyResult {
	id: string;
	run_id: string;
	policy_id?: string;
	policy_name: string;
	policy_type: string;
	hook: string;
	allow: boolean;
	deny_msgs: string[];
	warn_msgs: string[];
	trigger_ids: string[];
	evaluated_at: string;
}

export interface RunScanResult {
	id: string;
	run_id: string;
	tool: string;
	severity: 'CRITICAL' | 'HIGH' | 'MEDIUM' | 'LOW' | 'UNKNOWN';
	check_id: string;
	check_name: string;
	resource: string;
	filename: string;
	line_start?: number;
	line_end?: number;
	passed: boolean;
	created_at: string;
}

export const runs = {
	listAll: (offset = 0, limit = 50, filters: { status?: string; type?: string } = {}) => {
		const p = new URLSearchParams({ limit: String(limit), offset: String(offset) });
		if (filters.status) p.set('status', filters.status);
		if (filters.type) p.set('type', filters.type);
		return request<Paginated<Run>>(`/runs?${p}`);
	},
	list: (stackID: string, offset = 0, limit = 50, filters: { status?: string; type?: string } = {}) => {
		const p = new URLSearchParams({ limit: String(limit), offset: String(offset) });
		if (filters.status) p.set('status', filters.status);
		if (filters.type) p.set('type', filters.type);
		return request<Paginated<Run>>(`/stacks/${stackID}/runs?${p}`);
	},
	get: (id: string) => request<Run>(`/runs/${id}`),
	create: (
		stackID: string,
		type = 'tracked',
		varOverrides: { key: string; value: string }[] = []
	) =>
		request<Run>(`/stacks/${stackID}/runs`, {
			method: 'POST',
			body: JSON.stringify({ type, var_overrides: varOverrides })
		}),
	triggerDrift: (stackID: string) =>
		request<Run>(`/stacks/${stackID}/drift`, { method: 'POST' }),
	confirm: (id: string) => request<null>(`/runs/${id}/confirm`, { method: 'POST' }),
	approve: (id: string) => request<null>(`/runs/${id}/approve`, { method: 'POST' }),
	discard: (id: string) => request<null>(`/runs/${id}/discard`, { method: 'POST' }),
	cancel: (id: string) => request<null>(`/runs/${id}/cancel`, { method: 'POST' }),
	remove: (id: string) => request<null>(`/runs/${id}`, { method: 'DELETE' }),
	annotate: (id: string, annotation: string) =>
		request<null>(`/runs/${id}/annotation`, {
			method: 'PATCH',
			body: JSON.stringify({ annotation })
		}),
	policyResults: (id: string) => request<RunPolicyResult[]>(`/runs/${id}/policy-results`),
	scanResults: (id: string) => request<RunScanResult[]>(`/runs/${id}/scan-results`),
	explain: (id: string) => request<{ explanation: string }>(`/runs/${id}/explain`, { method: 'POST' }),
	downloadPlan: async (id: string): Promise<Blob> => {
		const headers: Record<string, string> = {};
		if (auth.accessToken) headers['Authorization'] = `Bearer ${auth.accessToken}`;
		const res = await fetch(`${BASE}/runs/${id}/plan`, { headers });
		if (!res.ok) {
			const err = await res.json().catch(() => ({ error: res.statusText }));
			throw new Error(err.error ?? 'Download failed');
		}
		return res.blob();
	}
};

// ── Audit ─────────────────────────────────────────────────────────────────────

export interface AuditEvent {
	id: number;
	occurred_at: string;
	actor_id?: string;
	actor_type: string;
	action: string;
	resource_id?: string;
	resource_type?: string;
	org_id?: string;
	ip_address?: string;
	context: Record<string, unknown>;
}

export const audit = {
	list: (offset = 0, limit = 50, filters: { action?: string; resource_type?: string; actor_id?: string } = {}) => {
		const p = new URLSearchParams({ limit: String(limit), offset: String(offset) });
		if (filters.action) p.set('action', filters.action);
		if (filters.resource_type) p.set('resource_type', filters.resource_type);
		if (filters.actor_id) p.set('actor_id', filters.actor_id);
		return request<Paginated<AuditEvent>>(`/audit?${p}`);
	},
	exportCSV: async (filters: { action?: string; resource_type?: string; actor_id?: string } = {}): Promise<Blob> => {
		const p = new URLSearchParams();
		if (filters.action) p.set('action', filters.action);
		if (filters.resource_type) p.set('resource_type', filters.resource_type);
		if (filters.actor_id) p.set('actor_id', filters.actor_id);
		const headers: Record<string, string> = {};
		if (auth.accessToken) headers['Authorization'] = `Bearer ${auth.accessToken}`;
		const res = await fetch(`${BASE}/audit/export?${p}`, { headers });
		if (!res.ok) throw new Error(`Export failed: ${res.statusText}`);
		return res.blob();
	}
};

// ── Webhook deliveries ────────────────────────────────────────────────────────

export interface WebhookDelivery {
	id: string;
	forge: string;
	event_type: string;
	delivery_id?: string;
	outcome: 'triggered' | 'skipped' | 'rejected';
	skip_reason?: string;
	run_id?: string;
	received_at: string;
}

// ── Stack dependencies ────────────────────────────────────────────────────────

export interface StackDep {
	id: string;
	name: string;
	slug: string;
	created_at: string;
}

export const deps = {
	upstream: (stackID: string) => request<StackDep[]>(`/stacks/${stackID}/upstream`),
	downstream: (stackID: string) => request<StackDep[]>(`/stacks/${stackID}/downstream`),
	addDownstream: (stackID: string, downstreamID: string) =>
		request<StackDep | null>(`/stacks/${stackID}/downstream/${downstreamID}`, { method: 'PUT' }),
	removeDownstream: (stackID: string, downstreamID: string) =>
		request<null>(`/stacks/${stackID}/downstream/${downstreamID}`, { method: 'DELETE' })
};

// ── Variable sets ─────────────────────────────────────────────────────────────

export interface VarSet {
	id: string;
	name: string;
	description: string;
	var_count: number;
	created_at: string;
	updated_at: string;
}

export interface VarMeta {
	id: string;
	name: string;
	is_secret: boolean;
	created_at: string;
	updated_at: string;
}

export interface VarSetDetail extends VarSet {
	vars: VarMeta[];
}

export interface StackVarSetRef {
	id: string;
	name: string;
	description: string;
	var_count: number;
	attached_at: string;
}

export const varSets = {
	list: () => request<VarSet[]>('/variable-sets'),
	get: (id: string) => request<VarSetDetail>(`/variable-sets/${id}`),
	create: (data: { name: string; description?: string }) =>
		request<VarSet>('/variable-sets', { method: 'POST', body: JSON.stringify(data) }),
	update: (id: string, data: { name?: string; description?: string }) =>
		request<VarSet>(`/variable-sets/${id}`, { method: 'PATCH', body: JSON.stringify(data) }),
	delete: (id: string) => request<null>(`/variable-sets/${id}`, { method: 'DELETE' }),
	upsertVar: (id: string, name: string, value: string, isSecret = true) =>
		request<VarMeta>(`/variable-sets/${id}/vars/${encodeURIComponent(name)}`, {
			method: 'PUT',
			body: JSON.stringify({ value, is_secret: isSecret })
		}),
	deleteVar: (id: string, name: string) =>
		request<null>(`/variable-sets/${id}/vars/${encodeURIComponent(name)}`, { method: 'DELETE' }),
	forStack: (stackID: string) => request<StackVarSetRef[]>(`/stacks/${stackID}/variable-sets`),
	attachToStack: (stackID: string, vsID: string) =>
		request<null>(`/stacks/${stackID}/variable-sets/${vsID}`, { method: 'PUT' }),
	detachFromStack: (stackID: string, vsID: string) =>
		request<null>(`/stacks/${stackID}/variable-sets/${vsID}`, { method: 'DELETE' })
};

// ── Service account tokens ────────────────────────────────────────────────────

export interface ServiceAccountToken {
	id: string;
	name: string;
	role: 'admin' | 'member' | 'viewer';
	created_at: string;
	last_used_at?: string;
	token?: string; // only present on creation
}

export const serviceAccountTokens = {
	list: () => request<ServiceAccountToken[]>('/org/service-account-tokens'),
	create: (name: string, role: string) =>
		request<ServiceAccountToken>('/org/service-account-tokens', {
			method: 'POST',
			body: JSON.stringify({ name, role })
		}),
	revoke: (id: string) =>
		request<null>(`/org/service-account-tokens/${id}`, { method: 'DELETE' })
};

// ── Remote state sources ──────────────────────────────────────────────────────

export interface RemoteStateSource {
	id: string;
	source_stack_id: string;
	source_stack_name: string;
	source_stack_slug: string;
	env_var_prefix: string;
	created_at: string;
}

export interface StateResource {
	address: string;
	type: string;
	name: string;
	module?: string;
	mode: string;
	instance_count: number;
}

export interface SystemSettings {
	runner_default_image: string;
	runner_max_concurrent: number;
	runner_job_timeout_mins: number;
	runner_memory_limit: string;
	runner_cpu_limit: string;
	default_slack_webhook: string;
	default_vcs_provider: string;
	default_vcs_base_url: string;
	default_gotify_url: string;
	default_gotify_token: string;
	default_ntfy_url: string;
	default_ntfy_token: string;
	smtp_host: string;
	smtp_port: number;
	smtp_username: string;
	smtp_password?: string; // write-only — never returned by GET, only sent on update
	smtp_from: string;
	smtp_tls: boolean;
	artifact_retention_days: number;
	oidc_provider?: string;
	oidc_aws_role_arn?: string;
	oidc_aws_session_duration_secs?: number;
	oidc_gcp_audience?: string;
	oidc_gcp_service_account_email?: string;
	oidc_azure_tenant_id?: string;
	oidc_azure_client_id?: string;
	oidc_azure_subscription_id?: string;
	oidc_vault_addr?: string;
	oidc_vault_role?: string;
	oidc_vault_mount?: string;
	oidc_authentik_url?: string;
	oidc_authentik_client_id?: string;
	oidc_generic_token_url?: string;
	oidc_generic_client_id?: string;
	oidc_generic_scope?: string;
	oidc_audience_override?: string;
	infracost_api_key?: string; // write-only — never returned by GET, only sent on update
	infracost_pricing_api_endpoint?: string;
	scan_tool?: 'none' | 'checkov' | 'trivy';
	scan_severity_threshold?: 'CRITICAL' | 'HIGH' | 'MEDIUM' | 'LOW';
	updated_at: string;
}


// ── Policies ──────────────────────────────────────────────────────────────────

export interface Policy {
	id: string;
	name: string;
	description?: string;
	type: 'pre_plan' | 'post_plan' | 'pre_apply' | 'trigger' | 'login';
	body: string;
	is_active: boolean;
	created_at: string;
	updated_at: string;
}

export interface StackPolicyRef {
	policy_id: string;
	name: string;
	type: string;
	is_active: boolean;
}

export interface PolicyResult {
	allow: boolean;
	deny?: string[];
	warn?: string[];
	trigger?: string[];
	require_approval?: boolean;
	trace?: string;
}

export const policies = {
	validate: (type: string, body: string) =>
		request<{ ok: boolean; error?: string }>('/policies/validate', {
			method: 'POST',
			body: JSON.stringify({ type, body })
		}),
	test: (type: string, body: string, input: unknown, trace = false) =>
		request<{ ok: boolean; error?: string; result?: PolicyResult; trace?: string }>('/policies/validate', {
			method: 'POST',
			body: JSON.stringify({ type, body, input, trace })
		}),
	list: () => request<Policy[]>('/policies'),
	get: (id: string) => request<Policy>(`/policies/${id}`),
	create: (data: Partial<Policy>) =>
		request<Policy>('/policies', { method: 'POST', body: JSON.stringify(data) }),
	update: (id: string, data: Partial<Policy>) =>
		request<Policy>(`/policies/${id}`, { method: 'PATCH', body: JSON.stringify(data) }),
	delete: (id: string) => request<null>(`/policies/${id}`, { method: 'DELETE' }),

	isOrgDefault: (id: string) => request<{ is_org_default: boolean }>(`/policies/${id}/org-default`),
	setOrgDefault: (id: string) => request<null>(`/policies/${id}/org-default`, { method: 'PUT' }),
	unsetOrgDefault: (id: string) => request<null>(`/policies/${id}/org-default`, { method: 'DELETE' }),
	forStack: (stackID: string) => request<StackPolicyRef[]>(`/stacks/${stackID}/policies`),
	attach: (stackID: string, policyID: string) =>
		request<null>(`/stacks/${stackID}/policies/${policyID}`, { method: 'PUT' }),
	detach: (stackID: string, policyID: string) =>
		request<null>(`/stacks/${stackID}/policies/${policyID}`, { method: 'DELETE' })
};

// ── Org ───────────────────────────────────────────────────────────────────────

export interface OrgMember {
	user_id: string;
	email: string;
	name: string;
	role: 'admin' | 'member' | 'viewer';
	joined_at: string;
}

export interface OrgInvite {
	id: string;
	email: string;
	role: 'admin' | 'member' | 'viewer';
	expires_at: string;
	created_at: string;
	token?: string; // only present on creation
}

export interface OrgSummary {
	id: string;
	name: string;
	slug: string;
	role: string;
}

export interface OrgDetail {
	id: string;
	name: string;
	slug: string;
}

export interface OrgGroupMap {
	id: string;
	group_claim: string;
	role: 'admin' | 'member' | 'viewer';
	created_at: string;
}

export const org = {
	me: () => request<{ role: string }>('/org/me'),
	get: () => request<OrgDetail>('/org'),
	update: (name: string) =>
		request<{ name: string }>('/org', { method: 'PATCH', body: JSON.stringify({ name }) }),
	list: () => request<OrgSummary[]>('/orgs'),
	switchOrg: (orgID: string) =>
		request<{ access_token: string }>('/auth/switch-org', {
			method: 'POST',
			body: JSON.stringify({ org_id: orgID })
		}),
	members: {
		list: () => request<OrgMember[]>('/org/members'),
		update: (userID: string, role: string) =>
			request<null>(`/org/members/${userID}`, { method: 'PATCH', body: JSON.stringify({ role }) }),
		remove: (userID: string) => request<null>(`/org/members/${userID}`, { method: 'DELETE' })
	},
	invites: {
		list: () => request<OrgInvite[]>('/org/invites'),
		create: (email: string, role: string) =>
			request<OrgInvite>('/org/invites', { method: 'POST', body: JSON.stringify({ email, role }) }),
		revoke: (inviteID: string) => request<null>(`/org/invites/${inviteID}`, { method: 'DELETE' }),
		accept: (token: string) =>
			request<{ org_id: string; role: string }>(`/invites/${token}/accept`, { method: 'POST' })
	},
	groupMaps: {
		list: () => request<OrgGroupMap[]>('/org/sso-group-maps'),
		create: (group_claim: string, role: string) =>
			request<OrgGroupMap>('/org/sso-group-maps', {
				method: 'POST',
				body: JSON.stringify({ group_claim, role })
			}),
		delete: (id: string) => request<null>(`/org/sso-group-maps/${id}`, { method: 'DELETE' })
	}
};

// ── Outgoing webhooks ─────────────────────────────────────────────────────────

export interface OutgoingWebhook {
	id: string;
	url: string;
	event_types: string[];
	headers: Record<string, string>;
	is_active: boolean;
	has_secret: boolean;
	created_at: string;
	secret?: string; // only present on creation or secret rotation
}

export interface OutgoingWebhookDelivery {
	id: string;
	event_type: string;
	attempt: number;
	status_code?: number;
	error?: string;
	run_id?: string;
	delivered_at: string;
}

// ── Stack templates ───────────────────────────────────────────────────────────

export interface StackTemplate {
	id: string;
	name: string;
	description: string;
	tool: string;
	tool_version: string;
	repo_url: string;
	repo_branch: string;
	project_root: string;
	runner_image: string;
	auto_apply: boolean;
	drift_detection: boolean;
	drift_schedule: string;
	auto_remediate_drift: boolean;
	vcs_provider: string;
	created_at: string;
	updated_at: string;
}

export const stackTemplates = {
	list: () => request<StackTemplate[]>('/stack-templates'),
	get: (id: string) => request<StackTemplate>(`/stack-templates/${id}`),
	create: (data: Partial<StackTemplate>) =>
		request<StackTemplate>('/stack-templates', { method: 'POST', body: JSON.stringify(data) }),
	update: (id: string, data: Partial<StackTemplate>) =>
		request<StackTemplate>(`/stack-templates/${id}`, { method: 'PATCH', body: JSON.stringify(data) }),
	delete: (id: string) => request<null>(`/stack-templates/${id}`, { method: 'DELETE' })
};

// ── Blueprints ────────────────────────────────────────────────────────────────

export interface BlueprintParam {
	id: string;
	name: string;
	label: string;
	description: string;
	type: 'string' | 'number' | 'bool' | 'select';
	options: string[];
	default_value: string;
	required: boolean;
	env_prefix: string;
	sort_order: number;
}

export interface Blueprint {
	id: string;
	name: string;
	description: string;
	tool: string;
	tool_version: string;
	repo_url: string;
	repo_branch: string;
	project_root: string;
	runner_image: string;
	auto_apply: boolean;
	drift_detection: boolean;
	drift_schedule: string;
	auto_remediate_drift: boolean;
	vcs_provider: string;
	is_published: boolean;
	params: BlueprintParam[];
	created_at: string;
	updated_at: string;
}

export const blueprints = {
	list: () => request<Blueprint[]>('/blueprints'),
	get: (id: string) => request<Blueprint>(`/blueprints/${id}`),
	create: (data: Partial<Blueprint>) =>
		request<Blueprint>('/blueprints', { method: 'POST', body: JSON.stringify(data) }),
	update: (id: string, data: Partial<Blueprint>) =>
		request<Blueprint>(`/blueprints/${id}`, { method: 'PATCH', body: JSON.stringify(data) }),
	delete: (id: string) => request<null>(`/blueprints/${id}`, { method: 'DELETE' }),
	publish: (id: string, published: boolean) =>
		request<null>(`/blueprints/${id}/publish`, { method: 'PUT', body: JSON.stringify({ published }) }),
	upsertParam: (id: string, data: Partial<BlueprintParam>) =>
		request<BlueprintParam>(`/blueprints/${id}/params/${encodeURIComponent(data.name ?? '')}`, {
			method: 'PUT',
			body: JSON.stringify(data)
		}),
	deleteParam: (id: string, name: string) =>
		request<null>(`/blueprints/${id}/params/${encodeURIComponent(name)}`, { method: 'DELETE' }),
	deploy: (id: string, stackName: string, values: Record<string, string>) =>
		request<{ stack_id: string }>(`/blueprints/${id}/deploy`, {
			method: 'POST',
			body: JSON.stringify({ stack_name: stackName, values })
		})
};

// ── Export / Import ───────────────────────────────────────────────────────────

export interface ImportResult {
	stacks:        { created: number; skipped: number };
	policies:      { created: number; skipped: number };
	variable_sets: { created: number; skipped: number };
	stack_templates: { created: number; skipped: number };
	blueprints:    { created: number; skipped: number };
	worker_pools:  { created: number; skipped: number };
}

export const configExport = {
	exportURL: () => `${BASE}/export`,
	import: (manifest: unknown) =>
		request<ImportResult>('/import', { method: 'POST', body: JSON.stringify(manifest) })
};

// ── Health / version ──────────────────────────────────────────────────────────

export interface HealthStatus {
	status: 'ok' | 'degraded';
	db: string;
	uptime: string;
	version: string;
	latest_version?: string;
	update_available?: boolean;
}

export const system = {
	health: () => fetch('/health').then((r) => r.json() as Promise<HealthStatus>),
	settings: {
		get: () => request<SystemSettings>('/system/settings'),
		update: (data: Partial<Omit<SystemSettings, 'updated_at'>>) =>
			request<SystemSettings>('/system/settings', { method: 'PUT', body: JSON.stringify(data) })
	},
	notifications: {
		testSlack: () => request<void>('/system/notifications/test-slack', { method: 'POST' }),
		testGotify: () => request<void>('/system/notifications/test-gotify', { method: 'POST' }),
		testNtfy: () => request<void>('/system/notifications/test-ntfy', { method: 'POST' }),
	},
};

// ── Module Registry ───────────────────────────────────────────────────────────

export interface RegistryModule {
	id: string;
	namespace: string;
	name: string;
	provider: string;
	version: string;
	readme?: string;
	yanked: boolean;
	published_by?: string;
	published_at: string;
	download_count: number;
}

async function requestForm<T>(path: string, body: FormData, retry = true): Promise<T> {
	const headers: Record<string, string> = {};
	if (auth.accessToken) headers['Authorization'] = `Bearer ${auth.accessToken}`;
	const res = await fetch('/api/v1' + path, { method: 'POST', headers, body });
	if (res.status === 401 && retry) {
		const refreshed = await tryRefresh();
		if (refreshed) return requestForm<T>(path, body, false);
		auth.clear();
		window.location.href = '/login';
		throw new Error('Unauthorized');
	}
	if (!res.ok) {
		const err = await res.json().catch(() => ({ error: res.statusText }));
		throw new Error((err as { error?: string }).error ?? 'Request failed');
	}
	if (res.status === 204) return null as T;
	return res.json() as Promise<T>;
}

export const registry = {
	list: (q?: string) =>
		request<RegistryModule[]>(`/registry/modules${q ? `?q=${encodeURIComponent(q)}` : ''}`),
	get: (id: string) => request<RegistryModule>(`/registry/modules/${id}`),
	publish: (form: FormData) => requestForm<RegistryModule>('/registry/modules', form),
	yank: (id: string) => request<null>(`/registry/modules/${id}`, { method: 'DELETE' }),
};

// ── Cloud OIDC workload identity federation ───────────────────────────────────

export interface CloudOIDCConfig {
	stack_id: string;
	provider: 'aws' | 'gcp' | 'azure' | 'vault' | 'authentik' | 'generic';
	aws_role_arn?: string;
	aws_session_duration_secs?: number;
	gcp_workload_identity_audience?: string;
	gcp_service_account_email?: string;
	azure_tenant_id?: string;
	azure_client_id?: string;
	azure_subscription_id?: string;
	vault_addr?: string;
	vault_role?: string;
	vault_mount?: string;
	authentik_url?: string;
	authentik_client_id?: string;
	generic_token_url?: string;
	generic_client_id?: string;
	generic_scope?: string;
	audience_override?: string;
	created_at: string;
	updated_at: string;
}

export const cloudOIDC = {
	get: (stackID: string) => request<CloudOIDCConfig>(`/stacks/${stackID}/cloud-oidc`),
	upsert: (stackID: string, cfg: Partial<CloudOIDCConfig>) =>
		request<CloudOIDCConfig>(`/stacks/${stackID}/cloud-oidc`, {
			method: 'PUT',
			body: JSON.stringify(cfg)
		}),
	delete: (stackID: string) => request<null>(`/stacks/${stackID}/cloud-oidc`, { method: 'DELETE' })
};
