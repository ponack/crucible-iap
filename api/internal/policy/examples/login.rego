package crucible.login

# Example login policy: map IdP groups to Crucible roles.
# Input: { "email": "...", "groups": [...], "sub": "..." }

# Grant admin if user is in the platform-admins group
allow_admin if {
	input.groups[_] == "platform-admins"
}

# Deny login for users not in any known group
deny contains msg if {
	count(input.groups) == 0
	msg := "user has no group memberships; access denied"
}
