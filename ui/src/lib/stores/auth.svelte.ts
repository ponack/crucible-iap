// Auth store — manages access/refresh tokens and current user state.
// Persists to localStorage so sessions survive page refreshes.

export interface User {
	id: string;
	email: string;
	name: string;
	avatar_url?: string;
	is_admin: boolean;
}

interface AuthState {
	user: User | null;
	accessToken: string | null;
	refreshToken: string | null;
	loading: boolean;
}

const STORAGE_KEY = 'crucible_auth';

function loadStored(): Omit<AuthState, 'loading'> {
	if (typeof localStorage === 'undefined') {
		return { user: null, accessToken: null, refreshToken: null };
	}
	try {
		const raw = localStorage.getItem(STORAGE_KEY);
		if (raw) return JSON.parse(raw);
	} catch {}
	return { user: null, accessToken: null, refreshToken: null };
}

function createAuthStore() {
	const stored = loadStored();
	let state = $state<AuthState>({ ...stored, loading: false });

	function persist() {
		if (typeof localStorage === 'undefined') return;
		localStorage.setItem(
			STORAGE_KEY,
			JSON.stringify({
				user: state.user,
				accessToken: state.accessToken,
				refreshToken: state.refreshToken
			})
		);
	}

	return {
		get user() {
			return state.user;
		},
		get accessToken() {
			return state.accessToken;
		},
		get refreshToken() {
			return state.refreshToken;
		},
		get loading() {
			return state.loading;
		},
		get isAuthenticated() {
			return state.user !== null && state.accessToken !== null;
		},

		setTokens(accessToken: string, refreshToken: string | null, user: User) {
			state.accessToken = accessToken;
			state.refreshToken = refreshToken;
			state.user = user;
			state.loading = false;
			persist();
		},

		setAccessToken(token: string) {
			state.accessToken = token;
			persist();
		},

		clear() {
			state.accessToken = null;
			state.refreshToken = null;
			state.user = null;
			state.loading = false;
			if (typeof localStorage !== 'undefined') {
				localStorage.removeItem(STORAGE_KEY);
			}
		}
	};
}

export const auth = createAuthStore();
