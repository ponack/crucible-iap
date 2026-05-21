// SPDX-License-Identifier: AGPL-3.0-or-later
import { request } from './base';

export interface OrgQuota {
	org_id: string;
	max_concurrent_runs: number | null;
	updated_at: string;
}

export interface OrgQuotaStatus {
	max_concurrent_runs: number | null;
	active_concurrent_runs: number;
}

export const orgQuotas = {
	get: () => request<OrgQuota>('/org/quotas'),
	status: () => request<OrgQuotaStatus>('/org/quotas/status'),
	update: (maxConcurrentRuns: number | null) =>
		request<OrgQuota>('/org/quotas', {
			method: 'PUT',
			body: JSON.stringify({ max_concurrent_runs: maxConcurrentRuns })
		})
};
