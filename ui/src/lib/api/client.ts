// Typed API client for the Crucible backend.
import { auth } from '$lib/stores/auth.svelte';

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
	if (res.status === 401 && retry && auth.refreshToken) {
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

async function tryRefresh(): Promise<boolean> {
	try {
		const res = await fetch('/auth/refresh', {
			method: 'POST',
			headers: { 'Content-Type': 'application/json' },
			body: JSON.stringify({ refresh_token: auth.refreshToken })
		});
		if (!res.ok) return false;
		const { access_token } = await res.json();
		auth.setAccessToken(access_token);
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
	vcs_provider: 'github' | 'gitlab' | 'gitea';
	vcs_base_url?: string;
	has_vcs_token: boolean;
	has_slack_webhook: boolean;
	notify_events: string[];
	has_secret_store: boolean;
	secret_store_provider?: string;
	has_state_backend: boolean;
	state_backend_provider?: string;
	created_at: string;
	updated_at: string;
}

// ── External secret store ─────────────────────────────────────────────────────

export type SecretStoreProvider = 'aws_sm' | 'hc_vault' | 'bitwarden_sm' | 'vaultwarden';

export interface SecretStoreInfo {
	provider: SecretStoreProvider;
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
	list: (offset = 0, limit = 50) =>
		request<Paginated<Stack>>(`/stacks?limit=${limit}&offset=${offset}`),
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
		upsert: (stackID: string, name: string, value: string) =>
			request<StackEnvVar>(`/stacks/${stackID}/env`, {
				method: 'PUT',
				body: JSON.stringify({ name, value })
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
				notify_events?: string[];
			}
		) =>
			request<null>(`/stacks/${stackID}/notifications`, {
				method: 'PUT',
				body: JSON.stringify(data)
			})
	},

	secretStore: {
		get: (stackID: string) => request<SecretStoreInfo>(`/stacks/${stackID}/secret-store`),
		upsert: (
			stackID: string,
			provider: SecretStoreProvider,
			config:
				| AWSSecretStoreConfig
				| HCVaultSecretStoreConfig
				| BitwardenSecretStoreConfig
				| VaultwardenSecretStoreConfig
		) =>
			request<SecretStoreInfo>(`/stacks/${stackID}/secret-store`, {
				method: 'PUT',
				body: JSON.stringify({ provider, config })
			}),
		delete: (stackID: string) =>
			request<null>(`/stacks/${stackID}/secret-store`, { method: 'DELETE' })
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
	}
};

// ── Stack env vars ────────────────────────────────────────────────────────────

export interface StackEnvVar {
	id: string;
	name: string;
	created_at: string;
	updated_at: string;
}

// ── Runs ──────────────────────────────────────────────────────────────────────

export interface Run {
	id: string;
	stack_id: string;
	status:
		| 'queued'
		| 'preparing'
		| 'planning'
		| 'unconfirmed'
		| 'confirmed'
		| 'applying'
		| 'finished'
		| 'failed'
		| 'canceled'
		| 'discarded';
	type: 'tracked' | 'proposed' | 'destroy';
	trigger: string;
	commit_sha?: string;
	branch?: string;
	is_drift: boolean;
	pr_number?: number;
	pr_url?: string;
	plan_add?: number;
	plan_change?: number;
	plan_destroy?: number;
	queued_at: string;
	started_at?: string;
	finished_at?: string;
}

export const runs = {
	list: (stackID: string, offset = 0, limit = 50) =>
		request<Paginated<Run>>(`/stacks/${stackID}/runs?limit=${limit}&offset=${offset}`),
	get: (id: string) => request<Run>(`/runs/${id}`),
	create: (stackID: string, type = 'tracked') =>
		request<Run>(`/stacks/${stackID}/runs`, { method: 'POST', body: JSON.stringify({ type }) }),
	triggerDrift: (stackID: string) =>
		request<Run>(`/stacks/${stackID}/drift`, { method: 'POST' }),
	confirm: (id: string) => request<null>(`/runs/${id}/confirm`, { method: 'POST' }),
	discard: (id: string) => request<null>(`/runs/${id}/discard`, { method: 'POST' }),
	cancel: (id: string) => request<null>(`/runs/${id}/cancel`, { method: 'POST' })
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
	list: (offset = 0, limit = 50) =>
		request<Paginated<AuditEvent>>(`/audit?limit=${limit}&offset=${offset}`)
};

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

export const policies = {
	list: () => request<Policy[]>('/policies'),
	get: (id: string) => request<Policy>(`/policies/${id}`),
	create: (data: Partial<Policy>) =>
		request<Policy>('/policies', { method: 'POST', body: JSON.stringify(data) }),
	update: (id: string, data: Partial<Policy>) =>
		request<Policy>(`/policies/${id}`, { method: 'PATCH', body: JSON.stringify(data) }),
	delete: (id: string) => request<null>(`/policies/${id}`, { method: 'DELETE' }),

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

export const org = {
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
	}
};
