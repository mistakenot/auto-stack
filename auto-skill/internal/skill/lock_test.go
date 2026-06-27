package skill

import (
	"testing"

	"github.com/mistakenot/auto-shared/config"
)

const validLockJSON = `{
  "version": 1,
  "skills": {
    "remote-skill": {
      "source": "github.com/owner/repo",
      "url": "https://github.com/owner/repo",
      "version_spec": "tag:v1.0",
      "ref": "v1.0",
      "commit": "abcdef1234567",
      "subpath": "skills/remote-skill",
      "private": false,
      "local": false,
      "state": "resolved"
    }
  }
}`

func lockCodes(errs []config.ValidationError) map[string]bool {
	m := make(map[string]bool)
	for _, e := range errs {
		m[e.Code] = true
	}
	return m
}

func TestParseLockStrict(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		lock, err := ParseLock([]byte(validLockJSON))
		if err != nil {
			t.Fatalf("ParseLock: %v", err)
		}
		if lock.Version != 1 {
			t.Errorf("version = %d, want 1", lock.Version)
		}
	})

	t.Run("derived render field rejected", func(t *testing.T) {
		data := `{"version":1,"skills":{"remote-skill":{"state":"resolved","template_hash":"abc"}}}`
		if _, err := ParseLock([]byte(data)); err == nil {
			t.Fatal("ParseLock accepted derived field template_hash, want error")
		}
	})
}

func TestValidateLock(t *testing.T) {
	t.Run("valid baseline has no errors", func(t *testing.T) {
		lock, err := ParseLock([]byte(validLockJSON))
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		if errs := ValidateLock(lock); len(errs) != 0 {
			t.Fatalf("ValidateLock = %+v, want none", errs)
		}
	})

	t.Run("credential userinfo URL", func(t *testing.T) {
		lock := &Lock{Skills: map[string]LockEntry{
			"remote-skill": {State: "resolved", Source: "s", Commit: "abcdef1", URL: "https://user:token@github.com/o/r"},
		}}
		if !lockCodes(ValidateLock(lock))[CodeCredentialsInURL] {
			t.Error("expected credentials_in_url for userinfo URL")
		}
	})

	t.Run("credential token query param", func(t *testing.T) {
		lock := &Lock{Skills: map[string]LockEntry{
			"remote-skill": {State: "resolved", Source: "s", Commit: "abcdef1", URL: "https://github.com/o/r?access_token=secret"},
		}}
		if !lockCodes(ValidateLock(lock))[CodeCredentialsInURL] {
			t.Error("expected credentials_in_url for token query param")
		}
	})

	t.Run("invalid state", func(t *testing.T) {
		lock := &Lock{Skills: map[string]LockEntry{"remote-skill": {State: "weird"}}}
		if !lockCodes(ValidateLock(lock))[CodeInvalidState] {
			t.Error("expected invalid_state")
		}
	})

	t.Run("missing required fields when resolved", func(t *testing.T) {
		lock := &Lock{Skills: map[string]LockEntry{"remote-skill": {State: "resolved"}}}
		if !lockCodes(ValidateLock(lock))[CodeRequired] {
			t.Error("expected required for missing source/url/commit")
		}
	})

	t.Run("unresolved entry needs no required fields", func(t *testing.T) {
		lock := &Lock{Skills: map[string]LockEntry{"remote-skill": {State: "unresolved"}}}
		if errs := ValidateLock(lock); len(errs) != 0 {
			t.Fatalf("ValidateLock = %+v, want none for unresolved", errs)
		}
	})

	t.Run("bad skill name", func(t *testing.T) {
		lock := &Lock{Skills: map[string]LockEntry{"Bad_Name": {State: "unresolved"}}}
		if !lockCodes(ValidateLock(lock))[CodeInvalidSkillName] {
			t.Error("expected invalid_skill_name")
		}
	})
}
