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
	created_at: string;
	updated_at: string;
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
	}
};

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
