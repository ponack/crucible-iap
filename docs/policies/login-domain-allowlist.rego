# login-domain-allowlist.rego
# Type: login
#
# Restricts login to users whose email addresses belong to approved domains.
# Add your organisation's SSO domain(s) to ALLOWED_DOMAINS.
# Users with disallowed domains are blocked at login time regardless of their
# identity provider account status.

package crucible

# --- configuration ---
ALLOWED_DOMAINS := {"example.com", "contractor.example.com"}
# ---------------------

login := result if {
  result := {
    "allow": allow,
    "deny":  deny_msg,
  }
}

email := input.user.email

domain := parts[1] if {
  parts := split(email, "@")
  count(parts) == 2
}

allow := ALLOWED_DOMAINS[domain]

deny_msg := msg if {
  not allow
  msg := sprintf(
    "login denied: email domain %v is not in the approved list",
    [domain],
  )
} else := ""
