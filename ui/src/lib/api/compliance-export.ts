// SPDX-License-Identifier: AGPL-3.0-or-later
import { auth } from '$lib/stores/auth.svelte';

export interface ComplianceExportRequest {
	start: string; // RFC3339
	end: string;   // RFC3339
	project_id?: string;
	tags?: string[];
}

// downloadExport POSTs the filter body and streams the ZIP response back
// to the browser as a download. We don't go through `request()` because
// the response is binary, not JSON, and we want the filename from the
// server's Content-Disposition.
export async function downloadExport(body: ComplianceExportRequest): Promise<void> {
	const headers: Record<string, string> = { 'Content-Type': 'application/json' };
	if (auth.accessToken) headers['Authorization'] = `Bearer ${auth.accessToken}`;

	const res = await fetch('/api/v1/compliance/exports', {
		method: 'POST',
		headers,
		body: JSON.stringify(body)
	});
	if (!res.ok) {
		const err = await res.json().catch(() => ({ error: res.statusText }));
		throw new Error((err as { error?: string }).error ?? 'Export failed');
	}

	const blob = await res.blob();
	const disposition = res.headers.get('Content-Disposition') ?? '';
	const match = /filename="([^"]+)"/.exec(disposition);
	const filename = match?.[1] ?? `crucible-compliance-${new Date().toISOString().replace(/[:.]/g, '-')}.zip`;

	const url = URL.createObjectURL(blob);
	const a = document.createElement('a');
	a.href = url;
	a.download = filename;
	document.body.appendChild(a);
	a.click();
	a.remove();
	URL.revokeObjectURL(url);
}
