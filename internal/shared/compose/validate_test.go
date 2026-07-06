package compose

import "testing"

func TestValidateCompose_NameIsManaged(t *testing.T) {
	yaml := "name: my-project\nservices:\n  web:\n    image: nginx\n"
	diags := ValidateCompose(yaml)
	var found *Diagnostic
	for i := range diags {
		if diags[i].Line == 1 {
			found = &diags[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("expected a diagnostic for top-level name, got %+v", diags)
	}
	if found.Severity != SeverityWarning {
		t.Errorf("expected warning severity, got %q", found.Severity)
	}
}

func TestValidateCompose_ValidHasNoDiagnostics(t *testing.T) {
	yaml := "services:\n  web:\n    image: nginx\n    ports:\n      - \"80:80\"\n"
	if diags := ValidateCompose(yaml); len(diags) != 0 {
		t.Errorf("expected no diagnostics, got %+v", diags)
	}
}
