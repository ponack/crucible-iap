// SPDX-License-Identifier: AGPL-3.0-or-later
import { request, requestForm, type Paginated } from './base';

export interface TagRef {
	id: string;
	name: string;
	color: string;
}

export interface Tag {
	id: string;
	org_id: string;
	name: string;
	color: string;
	stack_count: number;
	created_at: string;
}

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
	has_discord_webhook: boolean;
	has_teams_webhook: boolean;
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
	is_restricted: boolean;
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
	github_installation_uuid?: string;
	project_id?: string;
	health_score: number;
	health_status: 'healthy' | 'degraded' | 'unhealthy' | 'unknown';
	is_pinned: boolean;
	tags: TagRef[];
	created_at: string;
	updated_at: string;
}

export interface StackToken {
	id: string;
	stack_id: string;
	name: string;
	secret?: string;
	created_at: string;
	last_used?: string;
}

export interface StackEnvVar {
	id: string;
	name: string;
	is_secret: boolean;
	value?: string;
	created_at: string;
	updated_at: string;
}

export interface StackMember {
	user_id: string;
	email: string;
	name: string;
	role: 'viewer' | 'approver';
	created_at: string;
}

export interface StackDep {
	id: string;
	name: string;
	slug: string;
	created_at: string;
}

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

export interface StateVersion {
	id: string;
	run_id?: string;
	serial: number;
	resource_count: number;
	created_at: string;
}

export interface DiffEntry {
	address: string;
	type: string;
	instance_count: number;
}

export interface ChangedEntry {
	address: string;
	type: string;
	before: Record<string, unknown>;
	after: Record<string, unknown>;
}

export interface StateDiff {
	from_version_id: string | null;
	to_version_id: string;
	added: DiffEntry[];
	removed: DiffEntry[];
	changed: ChangedEntry[];
}

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

export interface OutgoingWebhook {
	id: string;
	url: string;
	event_types: string[];
	headers: Record<string, string>;
	is_active: boolean;
	has_secret: boolean;
	created_at: string;
	secret?: string;
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

export const stacks = {
	list: (
		offset = 0,
		limit = 50,
		filters: { q?: string; tool?: string; status?: string; tags?: string[]; pinned?: boolean; project?: string } = {}
	) => {
		const p = new URLSearchParams({ limit: String(limit), offset: String(offset) });
		if (filters.q) p.set('q', filters.q);
		if (filters.tool) p.set('tool', filters.tool);
		if (filters.status) p.set('status', filters.status);
		if (filters.pinned) p.set('pinned', 'true');
		if (filters.project) p.set('project', filters.project);
		filters.tags?.forEach((t) => p.append('tag', t));
		return request<Paginated<Stack>>(`/stacks?${p}`);
	},
	get: (id: string) => request<Stack>(`/stacks/${id}`),
	create: (data: Partial<Stack>) =>
		request<Stack>('/stacks', { method: 'POST', body: JSON.stringify(data) }),
	update: (id: string, data: Partial<Stack>) =>
		request<Stack>(`/stacks/${id}`, { method: 'PATCH', body: JSON.stringify(data) }),
	delete: (id: string) => request<null>(`/stacks/${id}`, { method: 'DELETE' }),
	clone: (id: string, name: string, slug?: string) =>
		request<{ stack_id: string }>(`/stacks/${id}/clone`, {
			method: 'POST',
			body: JSON.stringify({ name, slug: slug || undefined })
		}),
	pin: (id: string) => request<null>(`/stacks/${id}/pin`, { method: 'POST' }),
	unpin: (id: string) => request<null>(`/stacks/${id}/pin`, { method: 'DELETE' }),
	listTags: (id: string) => request<TagRef[]>(`/stacks/${id}/tags`),
	setTags: (id: string, tagIDs: string[]) =>
		request<null>(`/stacks/${id}/tags`, { method: 'PUT', body: JSON.stringify({ tag_ids: tagIDs }) }),

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
			request<null>(`/stacks/${stackID}/notifications/test-email`, { method: 'POST' }),
		testDiscord: (stackID: string) =>
			request<null>(`/stacks/${stackID}/notifications/test-discord`, { method: 'POST' }),
		testTeams: (stackID: string) =>
			request<null>(`/stacks/${stackID}/notifications/test-teams`, { method: 'POST' })
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
		versions: (stackID: string) => request<StateVersion[]>(`/stacks/${stackID}/state/versions`),
		versionDiff: (stackID: string, versionID: string) =>
			request<StateDiff>(`/stacks/${stackID}/state/versions/${versionID}/diff`),
		forceUnlock: (stackID: string) => request<{ cleared_lock_id: string }>(`/stacks/${stackID}/lock`, { method: 'DELETE' })
	}
};

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

export const deps = {
	upstream: (stackID: string) => request<StackDep[]>(`/stacks/${stackID}/upstream`),
	downstream: (stackID: string) => request<StackDep[]>(`/stacks/${stackID}/downstream`),
	addDownstream: (stackID: string, downstreamID: string) =>
		request<StackDep | null>(`/stacks/${stackID}/downstream/${downstreamID}`, { method: 'PUT' }),
	removeDownstream: (stackID: string, downstreamID: string) =>
		request<null>(`/stacks/${stackID}/downstream/${downstreamID}`, { method: 'DELETE' })
};

export const cloudOIDC = {
	get: (stackID: string) => request<CloudOIDCConfig>(`/stacks/${stackID}/cloud-oidc`),
	upsert: (stackID: string, cfg: Partial<CloudOIDCConfig>) =>
		request<CloudOIDCConfig>(`/stacks/${stackID}/cloud-oidc`, {
			method: 'PUT',
			body: JSON.stringify(cfg)
		}),
	delete: (stackID: string) => request<null>(`/stacks/${stackID}/cloud-oidc`, { method: 'DELETE' })
};

export const orgTags = {
	list: () => request<Tag[]>('/tags'),
	create: (name: string, color: string) =>
		request<Tag>('/tags', { method: 'POST', body: JSON.stringify({ name, color }) }),
	update: (id: string, data: { name?: string; color?: string }) =>
		request<null>(`/tags/${id}`, { method: 'PATCH', body: JSON.stringify(data) }),
	delete: (id: string) => request<null>(`/tags/${id}`, { method: 'DELETE' })
};

export const stackTemplates = {
	list: () => request<StackTemplate[]>('/stack-templates'),
	get: (id: string) => request<StackTemplate>(`/stack-templates/${id}`),
	create: (data: Partial<StackTemplate>) =>
		request<StackTemplate>('/stack-templates', { method: 'POST', body: JSON.stringify(data) }),
	update: (id: string, data: Partial<StackTemplate>) =>
		request<StackTemplate>(`/stack-templates/${id}`, { method: 'PATCH', body: JSON.stringify(data) }),
	delete: (id: string) => request<null>(`/stack-templates/${id}`, { method: 'DELETE' })
};

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

export const registry = {
	list: (q?: string) =>
		request<RegistryModule[]>(`/registry/modules${q ? `?q=${encodeURIComponent(q)}` : ''}`),
	get: (id: string) => request<RegistryModule>(`/registry/modules/${id}`),
	publish: (form: FormData) => requestForm<RegistryModule>('/registry/modules', form),
	yank: (id: string) => request<null>(`/registry/modules/${id}`, { method: 'DELETE' }),
};

export interface RegistryProvider {
	id: string;
	namespace: string;
	type: string;
	version: string;
	os: string;
	arch: string;
	filename: string;
	shasum: string;
	protocols: string[];
	readme?: string;
	yanked: boolean;
	published_by?: string;
	published_at: string;
	download_count: number;
}

export interface ProviderGPGKey {
	id: string;
	namespace: string;
	key_id: string;
	ascii_armor: string;
	created_by?: string;
	created_at: string;
}

export const providers = {
	list: (q?: string) =>
		request<RegistryProvider[]>(`/registry/providers${q ? `?q=${encodeURIComponent(q)}` : ''}`),
	get: (id: string) => request<RegistryProvider>(`/registry/providers/${id}`),
	publish: (form: FormData) => requestForm<RegistryProvider>('/registry/providers', form),
	yank: (id: string) => request<null>(`/registry/providers/${id}`, { method: 'DELETE' }),
	listGPGKeys: () => request<ProviderGPGKey[]>('/registry/provider-gpg-keys'),
	addGPGKey: (body: { namespace: string; key_id: string; ascii_armor: string }) =>
		request<ProviderGPGKey>('/registry/provider-gpg-keys', { method: 'POST', body: JSON.stringify(body) }),
	deleteGPGKey: (id: string) => request<null>(`/registry/provider-gpg-keys/${id}`, { method: 'DELETE' }),
};
