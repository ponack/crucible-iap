// SPDX-License-Identifier: AGPL-3.0-or-later
import { request } from './base';

export interface Policy {
	id: string;
	name: string;
	description?: string;
	type: 'pre_plan' | 'post_plan' | 'pre_apply' | 'approval' | 'trigger' | 'login';
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

export interface PolicyGitSource {
	id: string;
	name: string;
	repo_url: string;
	branch: string;
	path: string;
	vcs_integration_id?: string;
	webhook_secret?: string;
	mirror_mode: boolean;
	last_synced_at?: string;
	last_sync_sha: string;
	last_sync_error?: string;
	created_at: string;
}

export const policyGit = {
	list: () => request<PolicyGitSource[]>('/policy-git-sources'),
	get: (id: string) => request<PolicyGitSource>(`/policy-git-sources/${id}`),
	create: (body: {
		name: string;
		repo_url: string;
		branch?: string;
		path?: string;
		vcs_integration_id?: string;
		mirror_mode?: boolean;
	}) => request<PolicyGitSource>('/policy-git-sources', { method: 'POST', body: JSON.stringify(body) }),
	update: (id: string, body: Partial<{ name: string; repo_url: string; branch: string; path: string; vcs_integration_id: string; mirror_mode: boolean }>) =>
		request<PolicyGitSource>(`/policy-git-sources/${id}`, { method: 'PATCH', body: JSON.stringify(body) }),
	delete: (id: string) => request<null>(`/policy-git-sources/${id}`, { method: 'DELETE' }),
	sync: (id: string) => request<{ status: string }>(`/policy-git-sources/${id}/sync`, { method: 'POST' }),
};
