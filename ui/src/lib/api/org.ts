// SPDX-License-Identifier: AGPL-3.0-or-later
import { request } from './base';

export type IntegrationType =
	| 'github' | 'gitlab' | 'gitea'
	| 'aws_sm' | 'hc_vault' | 'bitwarden_sm' | 'vaultwarden';

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
	token?: string;
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

export interface ServiceAccountToken {
	id: string;
	name: string;
	role: 'admin' | 'member' | 'viewer';
	created_at: string;
	last_used_at?: string;
	token?: string;
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
