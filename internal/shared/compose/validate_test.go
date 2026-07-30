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
	return hasSeverity(diags, SeverityError)
}

func hasWarning(diags []Diagnostic) bool {
	return hasSeverity(diags, SeverityWarning)
}

func hasSeverity(diags []Diagnostic, sev Severity) bool {
	for _, d := range diags {
		if d.Severity == sev {
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

func TestValidateCompose_IncludeIsError(t *testing.T) {
	yaml := "include:\n  - common.yaml\nservices:\n  web:\n    image: nginx\n"
	if diags := ValidateCompose(yaml); !hasError(diags) {
		t.Fatalf("expected an error diagnostic for top-level include, got %+v", diags)
	}
}

func TestValidateCompose_ProfilesIsError(t *testing.T) {
	yaml := "services:\n  web:\n    image: nginx\n    profiles:\n      - debug\n"
	if diags := ValidateCompose(yaml); !hasError(diags) {
		t.Fatalf("expected an error diagnostic for profiles, got %+v", diags)
	}
}

func TestValidateCompose_ServiceConfigsIsError(t *testing.T) {
	yaml := "services:\n  web:\n    image: nginx\n    configs:\n      - source: my_cfg\n        target: /etc/cfg\n"
	if diags := ValidateCompose(yaml); !hasError(diags) {
		t.Fatalf("expected an error diagnostic for service configs, got %+v", diags)
	}
}

func TestValidateCompose_ExtendsFileIsError(t *testing.T) {
	yaml := "services:\n  web:\n    extends:\n      file: base.yaml\n      service: app\n"
	if diags := ValidateCompose(yaml); !hasError(diags) {
		t.Fatalf("expected an error diagnostic for extends.file, got %+v", diags)
	}
}

func TestValidateCompose_ExtendsServiceIsSupported(t *testing.T) {
	// In-file extends resolves in compose-go and must not be flagged, in either short or long form.
	long := "services:\n  base:\n    image: nginx\n  web:\n    extends:\n      service: base\n"
	if diags := ValidateCompose(long); hasError(diags) {
		t.Errorf("in-file extends (long form) must not error, got %+v", diags)
	}
	short := "services:\n  base:\n    image: nginx\n  web:\n    extends: base\n"
	if diags := ValidateCompose(short); hasError(diags) {
		t.Errorf("in-file extends (short form) must not error, got %+v", diags)
	}
}

func TestValidateCompose_IgnoredFieldsWarn(t *testing.T) {
	// container_name, healthcheck, logging and secrets are accepted but ignored → a warning, no error.
	yaml := "services:\n  web:\n    image: nginx\n" +
		"    container_name: fixed\n" +
		"    healthcheck:\n      test: [\"CMD\", \"true\"]\n" +
		"    logging:\n      driver: json-file\n" +
		"    secrets:\n      - db_password\n"
	diags := ValidateCompose(yaml)
	if hasError(diags) {
		t.Fatalf("ignored fields must warn, not error: %+v", diags)
	}
	if !hasWarning(diags) {
		t.Fatalf("expected warnings for ignored fields, got %+v", diags)
	}
}

func TestValidateCompose_TopLevelSecretsWarn(t *testing.T) {
	// Resolves the old inconsistency: top-level configs warned but top-level secrets was silent.
	yaml := "services:\n  web:\n    image: nginx\nsecrets:\n  db_password:\n    external: true\n"
	diags := ValidateCompose(yaml)
	if hasError(diags) {
		t.Fatalf("top-level secrets must warn, not error: %+v", diags)
	}
	if !hasWarning(diags) {
		t.Fatalf("expected a warning for top-level secrets, got %+v", diags)
	}
}
