package idhash

import (
	"regexp"
	"testing"
)

func TestDeriveDeterministic(t *testing.T) {
	a := Derive("ob", "correction", "subject", "s1")
	b := Derive("ob", "correction", "subject", "s1")
	if a != b {
		t.Fatalf("expected identical ids for identical parts, got %q and %q", a, b)
	}
}

func TestDeriveFormat(t *testing.T) {
	re := regexp.MustCompile(`^p-[0-9a-f]{8}$`)
	id := Derive("p", "some", "content")
	if !re.MatchString(id) {
		t.Fatalf("id %q does not match %s", id, re)
	}
}

func TestDeriveDifferentPartsDifferentID(t *testing.T) {
	a := Derive("ob", "correction", "subject", "s1")
	b := Derive("ob", "correction", "subject", "s2")
	if a == b {
		t.Fatalf("expected different ids for different parts, both %q", a)
	}
}
