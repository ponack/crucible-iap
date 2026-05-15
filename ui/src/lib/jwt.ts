export interface JWTPayload {
	uid: string;
	email: string;
	name: string;
	org?: string;
	iadm?: boolean;
	[key: string]: unknown;
}

// Decode a JWT payload without verification (server already validated the token).
export function decodeJWTPayload(token: string): JWTPayload {
	return JSON.parse(atob(token.split('.')[1])) as JWTPayload;
}
