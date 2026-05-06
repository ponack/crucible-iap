// SPDX-License-Identifier: AGPL-3.0-or-later
import { auth } from '$lib/stores/auth.svelte';
import { request } from './base';

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
	smtp_password?: string;
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
	infracost_api_key?: string;
	infracost_pricing_api_endpoint?: string;
	scan_tool?: 'none' | 'checkov' | 'trivy';
	scan_severity_threshold?: 'CRITICAL' | 'HIGH' | 'MEDIUM' | 'LOW';
	ai_provider?: 'anthropic' | 'openai';
	ai_model?: string;
	ai_base_url?: string;
	ai_api_key?: string;
	ai_api_key_set?: boolean;
	default_discord_webhook: string;
	default_teams_webhook: string;
	approval_timeout_hours: number;
	updated_at: string;
}

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
		testDiscord: () => request<void>('/system/notifications/test-discord', { method: 'POST' }),
		testTeams: () => request<void>('/system/notifications/test-teams', { method: 'POST' }),
	},
};

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
		return request<import('./base').Paginated<AuditEvent>>(`/audit?${p}`);
	},
	exportCSV: async (filters: { action?: string; resource_type?: string; actor_id?: string } = {}): Promise<Blob> => {
		const p = new URLSearchParams();
		if (filters.action) p.set('action', filters.action);
		if (filters.resource_type) p.set('resource_type', filters.resource_type);
		if (filters.actor_id) p.set('actor_id', filters.actor_id);
		const headers: Record<string, string> = {};
		if (auth.accessToken) headers['Authorization'] = `Bearer ${auth.accessToken}`;
		const res = await fetch(`/api/v1/audit/export?${p}`, { headers });
		if (!res.ok) throw new Error(`Export failed: ${res.statusText}`);
		return res.blob();
	},
	exportJSON: async (filters: { action?: string; resource_type?: string; actor_id?: string } = {}): Promise<Blob> => {
		const p = new URLSearchParams({ format: 'json' });
		if (filters.action) p.set('action', filters.action);
		if (filters.resource_type) p.set('resource_type', filters.resource_type);
		if (filters.actor_id) p.set('actor_id', filters.actor_id);
		const headers: Record<string, string> = {};
		if (auth.accessToken) headers['Authorization'] = `Bearer ${auth.accessToken}`;
		const res = await fetch(`/api/v1/audit/export?${p}`, { headers });
		if (!res.ok) throw new Error(`Export failed: ${res.statusText}`);
		return res.blob();
	}
};

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

export interface BlueprintExport {
	schema_version: number;
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
	params: Omit<BlueprintParam, 'id'>[];
}

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
		}),
	export: (id: string) => request<BlueprintExport>(`/blueprints/${id}/export`),
	importBlueprint: (data: BlueprintExport) =>
		request<Blueprint>('/blueprints/import', { method: 'POST', body: JSON.stringify(data) })
};

export interface ImportResult {
	stacks:          { created: number; skipped: number };
	policies:        { created: number; skipped: number };
	variable_sets:   { created: number; skipped: number };
	stack_templates: { created: number; skipped: number };
	blueprints:      { created: number; skipped: number };
	worker_pools:    { created: number; skipped: number };
}

export const configExport = {
	exportURL: () => `/api/v1/export`,
	import: (manifest: unknown) =>
		request<ImportResult>('/import', { method: 'POST', body: JSON.stringify(manifest) })
};
