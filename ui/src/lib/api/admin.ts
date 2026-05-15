// SPDX-License-Identifier: AGPL-3.0-or-later
import { request } from './base';

export interface AdminOrg {
	id: string;
	name: string;
	slug: string;
	member_count: number;
	created_at: string;
	archived_at?: string;
}

export interface AdminOrgMember {
	user_id: string;
	email: string;
	name: string;
	role: string;
	joined_at: string;
}

export const adminApi = {
	listOrgs: (archived = false) =>
		request<AdminOrg[]>(`/admin/orgs${archived ? '?archived=true' : ''}`),

	createOrg: (name: string, slug: string, adminEmail?: string) =>
		request<{ id: string; slug: string; name: string }>('/admin/orgs', {
			method: 'POST',
			body: JSON.stringify({ name, slug, admin_email: adminEmail ?? '' })
		}),

	getOrg: (id: string) => request<AdminOrg>(`/admin/orgs/${id}`),

	archiveOrg: (id: string) =>
		request<null>(`/admin/orgs/${id}/archive`, { method: 'POST' }),

	unarchiveOrg: (id: string) =>
		request<null>(`/admin/orgs/${id}/unarchive`, { method: 'POST' }),

	listOrgMembers: (id: string) =>
		request<AdminOrgMember[]>(`/admin/orgs/${id}/members`),

	addOrgMember: (id: string, email: string, role: string) =>
		request<{ user_id: string; role: string }>(`/admin/orgs/${id}/members`, {
			method: 'POST',
			body: JSON.stringify({ email, role })
		}),

	grantInstanceAdmin: (userID: string) =>
		request<null>(`/admin/users/${userID}/grant-instance-admin`, { method: 'POST' }),

	revokeInstanceAdmin: (userID: string) =>
		request<null>(`/admin/users/${userID}/revoke-instance-admin`, { method: 'POST' })
};
