// SPDX-License-Identifier: AGPL-3.0-or-later
import { request } from './base';

export type SIEMDestinationType =
	| 'splunk'
	| 'datadog'
	| 'elasticsearch'
	| 'webhook'
	| 'chronicle'
	| 'wazuh'
	| 'graylog';

export interface SIEMDestination {
	id: string;
	name: string;
	type: SIEMDestinationType;
	enabled: boolean;
	created_at: string;
	updated_at: string;
}

export interface SIEMDelivery {
	id: string;
	event_id: number;
	destination_id: string;
	destination_name: string;
	status: 'pending' | 'delivered' | 'failed';
	attempts: number;
	last_error?: string;
	delivered_at?: string;
	created_at: string;
}

export interface SIEMTestResult {
	ok: string;
	error?: string;
}

export interface CreateSIEMDestinationPayload {
	name: string;
	type: SIEMDestinationType;
	config: Record<string, unknown>;
	enabled?: boolean;
}

export interface UpdateSIEMDestinationPayload {
	name?: string;
	config?: Record<string, unknown>;
	enabled?: boolean;
}

export const siemApi = {
	list(): Promise<SIEMDestination[]> {
		return request<SIEMDestination[]>('/siem/destinations');
	},
	create(payload: CreateSIEMDestinationPayload): Promise<SIEMDestination> {
		return request<SIEMDestination>('/siem/destinations', {
			method: 'POST',
			body: JSON.stringify(payload)
		});
	},
	update(id: string, payload: UpdateSIEMDestinationPayload): Promise<SIEMDestination> {
		return request<SIEMDestination>(`/siem/destinations/${id}`, {
			method: 'PUT',
			body: JSON.stringify(payload)
		});
	},
	delete(id: string): Promise<void> {
		return request<void>(`/siem/destinations/${id}`, { method: 'DELETE' });
	},
	test(id: string): Promise<SIEMTestResult> {
		return request<SIEMTestResult>(`/siem/destinations/${id}/test`, { method: 'POST' });
	},
	listDeliveries(params?: { destination_id?: string; status?: string }): Promise<{ items: SIEMDelivery[]; total: number }> {
		const qs = new URLSearchParams();
		if (params?.destination_id) qs.set('destination_id', params.destination_id);
		if (params?.status) qs.set('status', params.status);
		const q = qs.toString();
		return request(`/siem/deliveries${q ? '?' + q : ''}`);
	}
};
