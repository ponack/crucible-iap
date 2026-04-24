// SPDX-License-Identifier: AGPL-3.0-or-later
// Shared reactive store for the user's org list.
// Lifted out of the root layout so child pages (e.g. org settings) can
// update the list after a rename without requiring a full page reload.
import type { OrgSummary } from '$lib/api/client';

class OrgListStore {
	list = $state<OrgSummary[]>([]);

	set(orgs: OrgSummary[]) {
		this.list = orgs;
	}

	clear() {
		this.list = [];
	}

	updateName(orgID: string, name: string) {
		this.list = this.list.map((o) => (o.id === orgID ? { ...o, name } : o));
	}
}

export const orgListStore = new OrgListStore();
