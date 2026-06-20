package auth

// roleWeight maps role names to a numeric weight so we can compare "highest role wins".
// Higher number = more powerful.
var roleWeight = map[string]int{
	"viewer":   1,
	"operator": 2,
	"admin":    3,
}

// bindingMatches reports whether a RoleBinding applies to the given (serverID, stackID) pair.
// An empty string means "any" (i.e. the binding is global at that level).
//
//	A binding matches when:
//	  (binding.ServerID == "" OR binding.ServerID == serverID)
//	  AND
//	  (binding.StackID  == "" OR binding.StackID  == stackID)
func bindingMatches(b RoleBinding, serverID, stackID string) bool {
	if b.ServerID != "" && b.ServerID != serverID {
		return false
	}
	if b.StackID != "" && b.StackID != stackID {
		return false
	}
	return true
}

// bestRole returns the highest-weight role among all bindings that match (serverID, stackID).
// Returns "" if no binding matches.
func bestRole(u *UserCtx, serverID, stackID string) string {
	best := ""
	bestW := 0
	for _, b := range u.Bindings {
		if !bindingMatches(b, serverID, stackID) {
			continue
		}
		w := roleWeight[b.Role]
		if w > bestW {
			best = b.Role
			bestW = w
		}
	}
	return best
}

// ─── Global helpers ────────────────────────────────────────────────────────────

// IsAdmin returns true if the user has a global admin binding.
func IsAdmin(u *UserCtx) bool {
	if u == nil {
		return false
	}
	for _, b := range u.Bindings {
		if b.Role == "admin" {
			return true
		}
	}
	return false
}

// CanManageSecrets returns true if the user is admin or has a secrets-manager binding.
func CanManageSecrets(u *UserCtx) bool {
	if u == nil {
		return false
	}
	if IsAdmin(u) {
		return true
	}
	for _, b := range u.Bindings {
		if b.Role == "secrets-manager" {
			return true
		}
	}
	return false
}

// HasAnyOperator returns true if the user is admin or has at least one operator binding
// (regardless of scope). Used to gate global stack-definition creation/editing.
func HasAnyOperator(u *UserCtx) bool {
	if u == nil {
		return false
	}
	if IsAdmin(u) {
		return true
	}
	for _, b := range u.Bindings {
		if b.Role == "operator" {
			return true
		}
	}
	return false
}

// ─── Server/Stack-scoped helpers ───────────────────────────────────────────────

// CanViewOn returns true if the user has at least viewer-level access on (serverID, stackID).
// admin always passes. stackID may be "" when the operation is not stack-specific.
func CanViewOn(u *UserCtx, serverID, stackID string) bool {
	if u == nil {
		return false
	}
	if IsAdmin(u) {
		return true
	}
	role := bestRole(u, serverID, stackID)
	return roleWeight[role] >= roleWeight["viewer"]
}

// CanOperateOn returns true if the user has at least operator-level access on (serverID, stackID).
// admin always passes. stackID may be "" when the operation is not stack-specific.
func CanOperateOn(u *UserCtx, serverID, stackID string) bool {
	if u == nil {
		return false
	}
	if IsAdmin(u) {
		return true
	}
	role := bestRole(u, serverID, stackID)
	return roleWeight[role] >= roleWeight["operator"]
}

// CanViewServer returns true if the user has any access to the given server
// (any binding where server_id matches or is global). Used to filter server lists.
func CanViewServer(u *UserCtx, serverID string) bool {
	if u == nil {
		return false
	}
	if IsAdmin(u) {
		return true
	}
	// Any matching binding (any role, any stack scope) grants server visibility.
	for _, b := range u.Bindings {
		if b.Role == "secrets-manager" {
			continue // secrets-manager is not a server-access role
		}
		if b.ServerID == "" || b.ServerID == serverID {
			if roleWeight[b.Role] > 0 {
				return true
			}
		}
	}
	return false
}

// ─── Stack visibility (derived from assignments) ───────────────────────────────

// CanViewStack returns true if the user may see the given stack.
// assignedServerIDs should be the list of server IDs the stack is currently assigned to
// (pass nil/empty for unassigned stacks).
//
//   - admin + any global viewer/operator → always true
//   - unassigned stack → any operator may see it (to assign it)
//   - assigned stack → true if CanViewOn(serverID, stackID) for any assigned server
func CanViewStack(u *UserCtx, stackID string, assignedServerIDs []string) bool {
	if u == nil {
		return false
	}
	if IsAdmin(u) {
		return true
	}
	// Global viewer or operator sees all stacks.
	if bestRole(u, "", "") != "" {
		return roleWeight[bestRole(u, "", "")] >= roleWeight["viewer"]
	}
	// Unassigned stacks: any operator (any scope) may see/assign them.
	if len(assignedServerIDs) == 0 {
		return HasAnyOperator(u)
	}
	// Assigned stacks: check per-server access.
	for _, sid := range assignedServerIDs {
		if CanViewOn(u, sid, stackID) {
			return true
		}
	}
	return false
}

// CanEditStack returns true if the user may create a new version of the given stack
// or delete it. Same logic as CanViewStack but requires operator level.
func CanEditStack(u *UserCtx, stackID string, assignedServerIDs []string) bool {
	if u == nil {
		return false
	}
	if IsAdmin(u) {
		return true
	}
	// Global operator sees all stacks.
	if roleWeight[bestRole(u, "", "")] >= roleWeight["operator"] {
		return true
	}
	// Unassigned stacks: any operator.
	if len(assignedServerIDs) == 0 {
		return HasAnyOperator(u)
	}
	// Assigned stacks: operator on any assigned server.
	for _, sid := range assignedServerIDs {
		if CanOperateOn(u, sid, stackID) {
			return true
		}
	}
	return false
}

// CanDeleteStack returns true only if the user has operator access on ALL servers
// the stack is assigned to (or is admin, or the stack is unassigned).
func CanDeleteStack(u *UserCtx, stackID string, assignedServerIDs []string) bool {
	if u == nil {
		return false
	}
	if IsAdmin(u) {
		return true
	}
	if len(assignedServerIDs) == 0 {
		return HasAnyOperator(u)
	}
	for _, sid := range assignedServerIDs {
		if !CanOperateOn(u, sid, stackID) {
			return false
		}
	}
	return true
}
