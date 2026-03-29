// Auth store — manages access/refresh tokens and current user state.
// Uses Svelte 5 Runes ($state, $derived).

interface User {
	id: string;
	email: string;
	name: string;
	avatar_url?: string;
	is_admin: boolean;
}

interface AuthState {
	user: User | null;
	accessToken: string | null;
	loading: boolean;
}

function createAuthStore() {
	let state = $state<AuthState>({
		user: null,
		accessToken: null,
		loading: true
	});

	return {
		get user() { return state.user; },
		get accessToken() { return state.accessToken; },
		get loading() { return state.loading; },
		get isAuthenticated() { return state.user !== null && state.accessToken !== null; },

		setTokens(accessToken: string, user: User) {
			state.accessToken = accessToken;
			state.user = user;
			state.loading = false;
		},

		clear() {
			state.accessToken = null;
			state.user = null;
			state.loading = false;
		},

		setLoading(v: boolean) {
			state.loading = v;
		}
	};
}

export const auth = createAuthStore();
