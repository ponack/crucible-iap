// SPDX-License-Identifier: AGPL-3.0-or-later
// Package access resolves per-stack effective roles for a calling user.
//
// Stack membership is opt-in. When a stack has no explicit members every org
// member gets "approver" access and every org viewer gets "viewer" access —
// matching the behaviour that existed before stack-level RBAC was introduced.
// Once at least one stack_member row exists the stack is "restricted": only
// listed members and org admins may access it.
//
// Effective role values: "admin" | "approver" | "viewer" | "" (no access).
package access

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// StackRole resolves the calling user's effective access level for a stack.
// SA tokens that bypass organization_members should skip this call (check
// c.Get("saRole") in the handler first).
func StackRole(ctx context.Context, pool *pgxpool.Pool, stackID, userID, orgID string) (string, error) {
	var orgRole string
	var smRole *string
	var hasMembers bool
	err := pool.QueryRow(ctx, `
		SELECT om.role,
		       sm.role,
		       EXISTS(SELECT 1 FROM stack_members WHERE stack_id = $1)
		FROM stacks s
		JOIN organization_members om ON om.org_id = s.org_id AND om.user_id = $2
		LEFT JOIN stack_members sm ON sm.stack_id = s.id AND sm.user_id = $2
		WHERE s.id = $1 AND s.org_id = $3
	`, stackID, userID, orgID).Scan(&orgRole, &smRole, &hasMembers)
	if err != nil {
		return "", err
	}
	return resolve(orgRole, smRole, hasMembers), nil
}

// StackRoleForRun resolves the effective access level via a run ID.
func StackRoleForRun(ctx context.Context, pool *pgxpool.Pool, runID, userID, orgID string) (string, error) {
	var orgRole string
	var smRole *string
	var hasMembers bool
	err := pool.QueryRow(ctx, `
		SELECT om.role,
		       sm.role,
		       EXISTS(SELECT 1 FROM stack_members WHERE stack_id = r.stack_id)
		FROM runs r
		JOIN stacks s ON s.id = r.stack_id
		JOIN organization_members om ON om.org_id = s.org_id AND om.user_id = $2
		LEFT JOIN stack_members sm ON sm.stack_id = r.stack_id AND sm.user_id = $2
		WHERE r.id = $1 AND s.org_id = $3
	`, runID, userID, orgID).Scan(&orgRole, &smRole, &hasMembers)
	if err != nil {
		return "", err
	}
	return resolve(orgRole, smRole, hasMembers), nil
}

// resolve maps (orgRole, stackMemberRole, hasMembers) to an effective role.
func resolve(orgRole string, smRole *string, hasMembers bool) string {
	if orgRole == "admin" {
		return "admin"
	}
	if !hasMembers {
		if orgRole == "member" {
			return "approver"
		}
		return "viewer"
	}
	if smRole != nil {
		return *smRole
	}
	return "" // restricted stack, user not listed
}

// stackRoleSQL returns a SQL CASE expression that computes the effective role
// for a given user inline. References the aliases om (organization_members)
// and sm (stack_members) which must be LEFT JOINed in the outer query.
// The expression uses the same logic as resolve().
const StackRoleSQL = `
	CASE
		WHEN COALESCE(om.role, 'admin') = 'admin' THEN 'admin'
		WHEN NOT EXISTS (SELECT 1 FROM stack_members sm2 WHERE sm2.stack_id = s.id)
			THEN CASE WHEN om.role = 'member' THEN 'approver' ELSE 'viewer' END
		WHEN sm.user_id IS NOT NULL THEN sm.role
		ELSE 'viewer'
	END`

// IsRestrictedSQL is a boolean expression indicating whether the stack has any
// explicit members configured.
const IsRestrictedSQL = `EXISTS(SELECT 1 FROM stack_members WHERE stack_id = s.id)`

// AccessFilterSQL is a WHERE predicate that excludes stacks the user cannot
// see. Must be ANDed into the outer query's WHERE clause.
const AccessFilterSQL = `(
	COALESCE(om.role, 'admin') = 'admin'
	OR NOT EXISTS (SELECT 1 FROM stack_members sm2 WHERE sm2.stack_id = s.id)
	OR sm.user_id IS NOT NULL
)`
