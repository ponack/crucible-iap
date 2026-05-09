// SPDX-License-Identifier: AGPL-3.0-or-later
import { request } from './base';

export interface Project {
	id: string;
	slug: string;
	name: string;
	description: string;
	created_at: string;
	updated_at: string;
	stack_count: number;
	member_count: number;
}

export interface ProjectStack {
	id: string;
	slug: string;
	name: string;
	description: string;
	tool: string;
	repo_branch: string;
	updated_at: string;
}

export interface ProjectMember {
	user_id: string;
	email: string;
	name: string;
	role: 'admin' | 'member' | 'viewer';
	added_at: string;
}

export interface ProjectDetail extends Project {
	stacks: ProjectStack[];
	members: ProjectMember[];
}

export const projects = {
	list: () => request<Project[]>('/projects'),
	create: (data: { name: string; description?: string; slug?: string }) =>
		request<Project>('/projects', { method: 'POST', body: JSON.stringify(data) }),
	get: (id: string) => request<ProjectDetail>(`/projects/${id}`),
	update: (id: string, data: { name: string; description?: string }) =>
		request<Project>(`/projects/${id}`, { method: 'PUT', body: JSON.stringify(data) }),
	delete: (id: string) => request<null>(`/projects/${id}`, { method: 'DELETE' }),
	listMembers: (id: string) => request<ProjectMember[]>(`/projects/${id}/members`),
	upsertMember: (id: string, userID: string, role: 'admin' | 'member' | 'viewer') =>
		request<null>(`/projects/${id}/members/${userID}`, {
			method: 'PUT',
			body: JSON.stringify({ role })
		}),
	removeMember: (id: string, userID: string) =>
		request<null>(`/projects/${id}/members/${userID}`, { method: 'DELETE' })
};
