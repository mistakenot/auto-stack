package config

import (
	"os"
	"testing"
)

func validSettings() Settings {
	return Settings{
		Endpoint:         "https://s3.eu-west-1.amazonaws.com",
		Bucket:           "test-bucket",
		Region:           "eu-west-1",
		AccessKeyID:      "AKIATEST",
		SecretAccessKey:  "secret",
		DefaultRetention: "90d",
	}
}

// TestWriteSecureMode0600 asserts the credential file and its parent dir are
// written with tight permissions (the secret access key must not be
// world-readable).
func TestWriteSecureMode0600(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	path, err := SettingsPath()
	if err != nil {
		t.Fatalf("SettingsPath: %v", err)
	}
	if err := WriteSecure(path, validSettings()); err != nil {
		t.Fatalf("WriteSecure: %v", err)
	}

	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat settings: %v", err)
	}
	if mode := fi.Mode().Perm(); mode != 0o600 {
		t.Errorf("settings.json mode = %o, want 600", mode)
	}

	dir, err := ArtifactDir()
	if err != nil {
		t.Fatalf("ArtifactDir: %v", err)
	}
	di, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat dir: %v", err)
	}
	if mode := di.Mode().Perm(); mode != 0o700 {
		t.Errorf("artifact dir mode = %o, want 700", mode)
	}

	// Round-trips through strict validation.
	if _, err := LoadValidated(path); err != nil {
		t.Errorf("LoadValidated after WriteSecure: %v", err)
	}
}

// TestWriteSecureTightensExistingFile ensures a pre-existing loose-mode file is
// chmodded down to 0600.
func TestWriteSecureTightensExistingFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	path, _ := SettingsPath()
	dir, _ := ArtifactDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WriteSecure(path, validSettings()); err != nil {
		t.Fatalf("WriteSecure: %v", err)
	}
	fi, _ := os.Stat(path)
	if mode := fi.Mode().Perm(); mode != 0o600 {
		t.Errorf("existing file not tightened: mode = %o, want 600", mode)
	}
}

func TestValidate(t *testing.T) {
	if errs := Validate("p", validSettings()); len(errs) != 0 {
		t.Errorf("valid settings produced errors: %+v", errs)
	}
	missing := Settings{}
	errs := Validate("p", missing)
	if len(errs) < 5 {
		t.Errorf("empty settings: got %d errors, want >=5", len(errs))
	}
	bad := validSettings()
	bad.DefaultRetention = "60d"
	if errs := Validate("p", bad); len(errs) != 1 || errs[0].Field != "default_retention" {
		t.Errorf("bad retention not flagged: %+v", errs)
	}
	httpEndpoint := validSettings()
	httpEndpoint.Endpoint = "http://s3.example.com"
	if errs := Validate("p", httpEndpoint); len(errs) != 1 || errs[0].Field != "endpoint" {
		t.Errorf("http endpoint not flagged: %+v", errs)
	}
}
