# Security Policy

## Reporting a vulnerability

Please **do not** open public GitHub issues for security vulnerabilities.

Report vulnerabilities privately via one of these channels:

- **GitHub:** Security → [Report a vulnerability](https://github.com/ponack/crucible-iap/security/advisories/new) (preferred)
- **Email:** Contact the maintainer directly (see GitHub profile)

Include as much detail as possible: steps to reproduce, affected versions, potential impact, and any mitigations you are aware of. You will receive an acknowledgement within 72 hours.

## Supported versions

| Version | Supported |
| ------- | --------- |
| latest (`main`) | Yes |
| older releases | Best-effort |

Crucible IAP is in active development. Security fixes are applied to the `main` branch and released promptly.

## Security model

See [docs/security.md](docs/security.md) for the full threat model, security controls, runner isolation details, and the hardening checklist for production deployments.

## Scope

The following are in scope for vulnerability reports:

- Authentication and authorisation bypass (JWT validation, RBAC)
- Privilege escalation between organisations
- Runner container escape or host filesystem access
- State backend credential exposure
- Injection vulnerabilities in the API (SQL, command, SSRF)
- Audit log tampering

The following are **out of scope**:

- Vulnerabilities requiring direct physical or OS-level access to the host
- Issues in third-party dependencies that are already publicly disclosed and have an upstream fix pending
- Denial-of-service attacks on self-hosted instances with no rate limiting configured (document the risk instead)
