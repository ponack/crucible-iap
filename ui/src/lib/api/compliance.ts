// SPDX-License-Identifier: AGPL-3.0-or-later
import { request } from './base';

export interface PolicyPack {
	id: string;
	slug: string;
	name: string;
	last_synced_at: string | null;
	last_sync_sha: string;
	last_sync_error?: string;
	policy_count: number;
}

export interface CatalogEntry {
	slug: string;
	name: string;
	description: string;
	policy_count: number;
	installed?: PolicyPack;
}

export const complianceApi = {
	getCatalog(): Promise<CatalogEntry[]> {
		return request<CatalogEntry[]>('/compliance/catalog');
	},
	install(slug: string): Promise<PolicyPack> {
		return request<PolicyPack>('/compliance/packs', { method: 'POST', body: JSON.stringify({ slug }) });
	},
	sync(id: string): Promise<void> {
		return request<void>(`/compliance/packs/${id}/sync`, { method: 'POST' });
	},
	uninstall(id: string): Promise<void> {
		return request<void>(`/compliance/packs/${id}`, { method: 'DELETE' });
	},
	listStackPacks(stackId: string): Promise<PolicyPack[]> {
		return request<PolicyPack[]>(`/stacks/${stackId}/policy-packs`);
	},
	attachPack(stackId: string, packId: string): Promise<void> {
		return request<void>(`/stacks/${stackId}/policy-packs`, { method: 'POST', body: JSON.stringify({ pack_id: packId }) });
	},
	detachPack(stackId: string, packId: string): Promise<void> {
		return request<void>(`/stacks/${stackId}/policy-packs/${packId}`, { method: 'DELETE' });
	}
};
