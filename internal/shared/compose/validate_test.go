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

func hasError(diags []Diagnostic) bool {
	for _, d := range diags {
		if d.Severity == SeverityError {
			return true
		}
	}
	return false
}

func TestValidateCompose_NamedVolumeIsError(t *testing.T) {
	// Short syntax: a bare name source is a named volume, not a bind mount — the #70 data-loss case.
	yaml := "services:\n  db:\n    image: postgres:16\n    volumes:\n      - dbdata:/var/lib/postgresql/data\nvolumes:\n  dbdata:\n"
	if diags := ValidateCompose(yaml); !hasError(diags) {
		t.Fatalf("expected an error diagnostic for a named volume, got %+v", diags)
	}
}

func TestValidateCompose_LongFormNamedVolumeIsError(t *testing.T) {
	yaml := "services:\n  db:\n    image: postgres:16\n    volumes:\n      - type: volume\n        source: dbdata\n        target: /data\n"
	if diags := ValidateCompose(yaml); !hasError(diags) {
		t.Fatalf("expected an error diagnostic for a long-form named volume, got %+v", diags)
	}
}

func TestValidateCompose_TmpfsVolumeIsError(t *testing.T) {
	yaml := "services:\n  app:\n    image: nginx\n    volumes:\n      - type: tmpfs\n        target: /cache\n"
	if diags := ValidateCompose(yaml); !hasError(diags) {
		t.Fatalf("expected an error diagnostic for a tmpfs mount, got %+v", diags)
	}
}

func TestValidateCompose_BindMountsAreAllowed(t *testing.T) {
	// Absolute, relative, and long-form bind mounts are all supported and must not be flagged.
	yaml := "services:\n  app:\n    image: nginx\n    volumes:\n" +
		"      - /srv/data:/data\n" +
		"      - ./config:/etc/app:ro\n" +
		"      - type: bind\n        source: /srv/logs\n        target: /logs\n"
	if diags := ValidateCompose(yaml); len(diags) != 0 {
		t.Errorf("expected no diagnostics for bind mounts, got %+v", diags)
	}
}
