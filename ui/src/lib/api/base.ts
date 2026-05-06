// SPDX-License-Identifier: AGPL-3.0-or-later
import { auth } from '$lib/stores/auth.svelte';
import { decodeJWTPayload } from '$lib/jwt';

const BASE = '/api/v1';

export async function request<T>(path: string, init: RequestInit = {}, retry = true): Promise<T> {
	const headers: Record<string, string> = {
		'Content-Type': 'application/json',
		...(init.headers as Record<string, string>)
	};

	if (auth.accessToken) {
		headers['Authorization'] = `Bearer ${auth.accessToken}`;
	}

	const res = await fetch(BASE + path, { ...init, headers });

	// Attempt silent token refresh on 401, once.
	if (res.status === 401 && retry) {
		const refreshed = await tryRefresh();
		if (refreshed) return request<T>(path, init, false);
		auth.clear();
		window.location.href = '/login';
		throw new Error('Unauthorized');
	}

	if (res.status === 401) {
		auth.clear();
		window.location.href = '/login';
		throw new Error('Unauthorized');
	}

	if (!res.ok) {
		const err = await res.json().catch(() => ({ error: res.statusText }));
		throw new Error(err.error ?? 'Request failed');
	}

	if (res.status === 204) return null as T;
	return res.json() as Promise<T>;
}

// tryRefresh silently exchanges the httpOnly refresh cookie for a new access token.
// Exported so the layout can call it on startup to restore the session after a page reload.
export async function tryRefresh(): Promise<boolean> {
	try {
		const res = await fetch('/auth/refresh', { method: 'POST' });
		if (!res.ok) return false;
		const { access_token } = await res.json();
		try {
			const payload = decodeJWTPayload(access_token);
			auth.setTokens(access_token, {
				id: payload.uid,
				email: payload.email,
				name: payload.name,
				is_admin: false
			});
		} catch {
			auth.setAccessToken(access_token);
		}
		return true;
	} catch {
		return false;
	}
}

export async function requestForm<T>(path: string, body: FormData, retry = true): Promise<T> {
	const headers: Record<string, string> = {};
	if (auth.accessToken) headers['Authorization'] = `Bearer ${auth.accessToken}`;
	const res = await fetch('/api/v1' + path, { method: 'POST', headers, body });
	if (res.status === 401 && retry) {
		const refreshed = await tryRefresh();
		if (refreshed) return requestForm<T>(path, body, false);
		auth.clear();
		window.location.href = '/login';
		throw new Error('Unauthorized');
	}
	if (!res.ok) {
		const err = await res.json().catch(() => ({ error: res.statusText }));
		throw new Error((err as { error?: string }).error ?? 'Request failed');
	}
	if (res.status === 204) return null as T;
	return res.json() as Promise<T>;
}

// ── Pagination ────────────────────────────────────────────────────────────────

export interface PageMeta {
	limit: number;
	offset: number;
	total: number;
	has_more: boolean;
}

export interface Paginated<T> {
	data: T[];
	pagination: PageMeta;
}
