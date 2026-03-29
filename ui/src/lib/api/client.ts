// Typed API client for the Crucible backend.
import { auth } from '$lib/stores/auth.svelte';

const BASE = '/api/v1';

async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
	const headers: Record<string, string> = {
		'Content-Type': 'application/json',
		...(init.headers as Record<string, string>)
	};

	if (auth.accessToken) {
		headers['Authorization'] = `Bearer ${auth.accessToken}`;
	}

	const res = await fetch(BASE + path, { ...init, headers });

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
	auto_apply: boolean;
	drift_detection: boolean;
	created_at: string;
	updated_at: string;
}

export const stacks = {
	list: () => request<Stack[]>('/stacks'),
	get: (id: string) => request<Stack>(`/stacks/${id}`),
	create: (data: Partial<Stack>) =>
		request<Stack>('/stacks', { method: 'POST', body: JSON.stringify(data) }),
	update: (id: string, data: Partial<Stack>) =>
		request<Stack>(`/stacks/${id}`, { method: 'PATCH', body: JSON.stringify(data) }),
	delete: (id: string) => request<null>(`/stacks/${id}`, { method: 'DELETE' })
};

// ── Runs ──────────────────────────────────────────────────────────────────────

export interface Run {
	id: string;
	stack_id: string;
	status: 'queued' | 'preparing' | 'planning' | 'unconfirmed' | 'confirmed' | 'applying' | 'finished' | 'failed' | 'canceled' | 'discarded';
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
	list: (stackID: string) => request<Run[]>(`/stacks/${stackID}/runs`),
	get: (id: string) => request<Run>(`/runs/${id}`),
	create: (stackID: string, type = 'tracked') =>
		request<Run>(`/stacks/${stackID}/runs`, { method: 'POST', body: JSON.stringify({ type }) }),
	confirm: (id: string) => request<null>(`/runs/${id}/confirm`, { method: 'POST' }),
	discard: (id: string) => request<null>(`/runs/${id}/discard`, { method: 'POST' }),
	cancel: (id: string) => request<null>(`/runs/${id}/cancel`, { method: 'POST' })
};
