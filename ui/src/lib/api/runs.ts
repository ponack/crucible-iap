// SPDX-License-Identifier: AGPL-3.0-or-later
import { auth } from '$lib/stores/auth.svelte';
import { request, type Paginated } from './base';

export interface Run {
	id: string;
	stack_id: string;
	stack_name?: string;
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
	var_overrides?: string[];
	annotation?: string;
	my_stack_role?: 'admin' | 'approver' | 'viewer';
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

const BASE = '/api/v1';

export const runs = {
	listAll: (
		offset = 0,
		limit = 50,
		filters: { status?: string; type?: string; tags?: string[] } = {}
	) => {
		const p = new URLSearchParams({ limit: String(limit), offset: String(offset) });
		if (filters.status) p.set('status', filters.status);
		if (filters.type) p.set('type', filters.type);
		filters.tags?.forEach((t) => p.append('tag', t));
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
	retrigger: (id: string) => request<Run>(`/runs/${id}/retrigger`, { method: 'POST' }),
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
