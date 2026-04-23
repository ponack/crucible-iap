// Auth store — manages access token (memory only) and user/orgRole (localStorage).
// The refresh token lives in an httpOnly cookie managed by the server.

export interface User {
	id: string;
	email: string;
	name: string;
	avatar_url?: string;
	is_admin: boolean;
}

export type OrgRole = 'admin' | 'member' | 'viewer';

interface AuthState {
	user: User | null;
	accessToken: string | null;
	orgRole: OrgRole | null;
	loading: boolean;
}

const STORAGE_KEY = 'crucible_auth';

function loadStored(): Omit<AuthState, 'loading'> {
	if (typeof localStorage === 'undefined') {
		return { user: null, accessToken: null, orgRole: null };
	}
	try {
		const raw = localStorage.getItem(STORAGE_KEY);
		if (raw) {
			const parsed = JSON.parse(raw);
			// accessToken intentionally not loaded — silently refreshed via httpOnly cookie on mount.
			return { user: parsed.user ?? null, accessToken: null, orgRole: parsed.orgRole ?? null };
		}
	} catch {}
	return { user: null, accessToken: null, orgRole: null };
}

function createAuthStore() {
	const stored = loadStored();
	let state = $state<AuthState>({ ...stored, loading: false });

	function persist() {
		if (typeof localStorage === 'undefined') return;
		// accessToken excluded — memory only, prevents XSS theft.
		localStorage.setItem(
			STORAGE_KEY,
			JSON.stringify({ user: state.user, orgRole: state.orgRole })
		);
	}

	return {
		get user() {
			return state.user;
		},
		get accessToken() {
			return state.accessToken;
		},
		get orgRole() {
			return state.orgRole;
		},
		get loading() {
			return state.loading;
		},
		get isAuthenticated() {
			return state.user !== null && state.accessToken !== null;
		},
		get isAdmin() {
			return state.orgRole === 'admin';
		},
		get isMemberOrAbove() {
			return state.orgRole === 'admin' || state.orgRole === 'member';
		},

		setTokens(accessToken: string, user: User) {
			state.accessToken = accessToken;
			state.user = user;
			state.loading = false;
			persist();
		},

		setAccessToken(token: string) {
			state.accessToken = token;
		},

		setOrgRole(role: OrgRole) {
			state.orgRole = role;
			persist();
		},

		clear() {
			state.accessToken = null;
			state.user = null;
			state.orgRole = null;
			state.loading = false;
			if (typeof localStorage !== 'undefined') {
				localStorage.removeItem(STORAGE_KEY);
			}
		}
	};
}

export const auth = createAuthStore();
