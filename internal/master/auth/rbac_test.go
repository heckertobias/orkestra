package auth_test

import (
	"testing"

	"github.com/heckertobias/orkestra/internal/master/auth"
)

func user(bindings ...auth.RoleBinding) *auth.UserCtx {
	var globalRoles []string
	for _, b := range bindings {
		if b.ServerID == "" && b.StackID == "" {
			globalRoles = append(globalRoles, b.Role)
		}
	}
	return &auth.UserCtx{Roles: globalRoles, Bindings: bindings}
}

func b(role, serverID, stackID string) auth.RoleBinding {
	return auth.RoleBinding{Role: role, ServerID: serverID, StackID: stackID}
}

func TestIsAdmin(t *testing.T) {
	if auth.IsAdmin(user(b("admin", "", ""))) != true {
		t.Error("global admin should be admin")
	}
	if auth.IsAdmin(user(b("operator", "", ""))) != false {
		t.Error("operator should not be admin")
	}
	if auth.IsAdmin(nil) != false {
		t.Error("nil user should not be admin")
	}
}

func TestCanManageSecrets(t *testing.T) {
	tests := []struct {
		name string
		u    *auth.UserCtx
		want bool
	}{
		{"admin", user(b("admin", "", "")), true},
		{"secrets-manager", user(b("secrets-manager", "", "")), true},
		{"operator", user(b("operator", "", "")), false},
		{"viewer", user(b("viewer", "", "")), false},
		{"nil", nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if auth.CanManageSecrets(tt.u) != tt.want {
				t.Errorf("CanManageSecrets(%v) = %v, want %v", tt.name, !tt.want, tt.want)
			}
		})
	}
}

func TestHasAnyOperator(t *testing.T) {
	if !auth.HasAnyOperator(user(b("operator", "serverA", ""))) {
		t.Error("scoped operator should satisfy HasAnyOperator")
	}
	if !auth.HasAnyOperator(user(b("admin", "", ""))) {
		t.Error("admin should satisfy HasAnyOperator")
	}
	if auth.HasAnyOperator(user(b("viewer", "", ""))) {
		t.Error("viewer should not satisfy HasAnyOperator")
	}
}

func TestCanOperateOn(t *testing.T) {
	globalOp := user(b("operator", "", ""))
	scopedOpA := user(b("operator", "serverA", ""))
	scopedOpAStack1 := user(b("operator", "serverA", "stack1"))
	viewerB := user(b("viewer", "serverB", ""))

	tests := []struct {
		name     string
		u        *auth.UserCtx
		serverID string
		stackID  string
		want     bool
	}{
		{"global-op any server", globalOp, "serverX", "", true},
		{"global-op any stack", globalOp, "serverX", "stack99", true},
		{"scoped-op correct server", scopedOpA, "serverA", "", true},
		{"scoped-op wrong server", scopedOpA, "serverB", "", false},
		{"scoped-op correct server+stack", scopedOpAStack1, "serverA", "stack1", true},
		{"scoped-op correct server wrong stack", scopedOpAStack1, "serverA", "stack2", false},
		{"scoped-op wrong server any stack", scopedOpAStack1, "serverB", "stack1", false},
		{"viewer cannot operate", viewerB, "serverB", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if auth.CanOperateOn(tt.u, tt.serverID, tt.stackID) != tt.want {
				t.Errorf("CanOperateOn(%q, %q) = %v, want %v", tt.serverID, tt.stackID, !tt.want, tt.want)
			}
		})
	}
}

func TestCanViewOn(t *testing.T) {
	viewerA := user(b("viewer", "serverA", ""))
	opB := user(b("operator", "serverB", ""))
	viewerAStack1 := user(b("viewer", "serverA", "stack1"))

	if !auth.CanViewOn(viewerA, "serverA", "") {
		t.Error("viewer on serverA should be able to view serverA")
	}
	if auth.CanViewOn(viewerA, "serverB", "") {
		t.Error("viewer on serverA should not view serverB")
	}
	if !auth.CanViewOn(opB, "serverB", "") {
		t.Error("operator on serverB should be able to view serverB")
	}
	// stack-scoped viewer: can view stack1 on serverA, not stack2
	if !auth.CanViewOn(viewerAStack1, "serverA", "stack1") {
		t.Error("stack-scoped viewer should see their stack")
	}
	if auth.CanViewOn(viewerAStack1, "serverA", "stack2") {
		t.Error("stack-scoped viewer should not see other stacks")
	}
}

func TestCanViewServer(t *testing.T) {
	// Any binding on serverA (viewer, operator, or stack-scoped) grants server visibility.
	for _, role := range []string{"viewer", "operator"} {
		u := user(b(role, "serverA", ""))
		if !auth.CanViewServer(u, "serverA") {
			t.Errorf("%s on serverA should see serverA", role)
		}
		if auth.CanViewServer(u, "serverB") {
			t.Errorf("%s on serverA should not see serverB", role)
		}
	}
	// secrets-manager should not grant server visibility
	sm := user(b("secrets-manager", "", ""))
	if auth.CanViewServer(sm, "serverA") {
		t.Error("secrets-manager should not grant server visibility")
	}
}

func TestCanViewStack(t *testing.T) {
	adminUser := user(b("admin", "", ""))
	globalOp := user(b("operator", "", ""))
	globalViewer := user(b("viewer", "", ""))
	opA := user(b("operator", "serverA", ""))
	viewerA := user(b("viewer", "serverA", ""))
	opAStack1 := user(b("operator", "serverA", "stack1"))
	noAccess := user(b("viewer", "serverB", ""))

	// Assigned stack
	assigned := []string{"serverA"}
	unassigned := []string{}

	if !auth.CanViewStack(adminUser, "stack1", assigned) {
		t.Error("admin should see assigned stack")
	}
	if !auth.CanViewStack(globalOp, "stack1", assigned) {
		t.Error("global operator should see assigned stack")
	}
	if !auth.CanViewStack(globalViewer, "stack1", assigned) {
		t.Error("global viewer should see assigned stack")
	}
	if !auth.CanViewStack(opA, "stack1", assigned) {
		t.Error("operator on assigned server should see stack")
	}
	if !auth.CanViewStack(viewerA, "stack1", assigned) {
		t.Error("viewer on assigned server should see stack")
	}
	if !auth.CanViewStack(opAStack1, "stack1", assigned) {
		t.Error("stack-scoped operator should see their stack")
	}
	if auth.CanViewStack(opAStack1, "stack2", assigned) {
		t.Error("stack-scoped operator should not see other stack")
	}
	if auth.CanViewStack(noAccess, "stack1", assigned) {
		t.Error("viewer on different server should not see stack")
	}
	// Unassigned stack: any operator may see
	if !auth.CanViewStack(opA, "stack-new", unassigned) {
		t.Error("operator should see unassigned stack")
	}
	if auth.CanViewStack(viewerA, "stack-new", unassigned) {
		t.Error("viewer should not see unassigned stack")
	}
}

func TestCanDeleteStack(t *testing.T) {
	opA := user(b("operator", "serverA", ""))
	opB := user(b("operator", "serverB", ""))
	opBoth := user(b("operator", "serverA", ""), b("operator", "serverB", ""))
	adminUser := user(b("admin", "", ""))

	twoServers := []string{"serverA", "serverB"}

	// Need operator on BOTH servers to delete
	if auth.CanDeleteStack(opA, "s", twoServers) {
		t.Error("operator on only one server should not delete stack on two servers")
	}
	if auth.CanDeleteStack(opB, "s", twoServers) {
		t.Error("operator on only one server should not delete stack on two servers")
	}
	if !auth.CanDeleteStack(opBoth, "s", twoServers) {
		t.Error("operator on both servers should be able to delete")
	}
	if !auth.CanDeleteStack(adminUser, "s", twoServers) {
		t.Error("admin should always be able to delete")
	}
}
