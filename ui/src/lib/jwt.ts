// Decode a JWT payload without verification (server already validated the token).
export function decodeJWTPayload(token: string): Record<string, unknown> {
	return JSON.parse(atob(token.split('.')[1]));
}
