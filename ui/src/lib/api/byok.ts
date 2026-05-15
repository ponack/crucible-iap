// SPDX-License-Identifier: AGPL-3.0-or-later
import { request } from './base';

export type KMSProvider = 'aws_kms' | 'hc_vault_transit' | 'azure_kv';

export interface BYOKStatus {
	enabled: boolean;
	provider?: KMSProvider;
	key_id?: string;
}

export const byok = {
	status: () => request<BYOKStatus>('/byok'),
	test: (provider: KMSProvider, keyID: string) =>
		request<null>('/byok/test', {
			method: 'POST',
			body: JSON.stringify({ provider, key_id: keyID })
		}),
	enable: (provider: KMSProvider, keyID: string) =>
		request<null>('/byok/enable', {
			method: 'POST',
			body: JSON.stringify({ provider, key_id: keyID })
		}),
	rotate: () => request<null>('/byok/rotate', { method: 'POST' }),
	disable: () => request<null>('/byok/disable', { method: 'POST' })
};
