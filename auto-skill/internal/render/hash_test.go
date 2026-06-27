package render

import (
	"bytes"
	"strings"
	"testing"
)

const hashTestSkillMD = `---
name: demo
description: a demo skill
customize:
  who:
    required: true
---
Hello {{ .who }}.
`

func baseInput() RenderInput {
	return RenderInput{
		SkillMD: []byte(hashTestSkillMD),
		Values:  map[string]ReplacementValue{"who": {Literal: "world"}},
		Files: []InputFile{
			{Path: "references/api.md", Mode: ModeFile, Data: []byte("# API\nsome text\n")},
			{Path: "scripts/run.sh", Mode: ModeExecutable, Data: []byte("#!/bin/sh\necho hi\n")},
		},
	}
}

func mustRender(t *testing.T, in RenderInput) Tree {
	t.Helper()
	tree, err := Render(in)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	return tree
}

func TestSkillVersion_DeterministicAcrossRenders(t *testing.T) {
	a := mustRender(t, baseInput())
	b := mustRender(t, baseInput())
	if a.SkillVersion != b.SkillVersion {
		t.Fatalf("non-deterministic skill_version: %q != %q", a.SkillVersion, b.SkillVersion)
	}
	if a.SkillVersion == "" {
		t.Fatal("empty skill_version")
	}
	// byte-identical trees too
	if len(a.Files) != len(b.Files) {
		t.Fatalf("file count differs: %d vs %d", len(a.Files), len(b.Files))
	}
	for i := range a.Files {
		if a.Files[i].Path != b.Files[i].Path || !bytes.Equal(a.Files[i].Data, b.Files[i].Data) {
			t.Fatalf("file %d differs across renders", i)
		}
	}
}

func TestSkillVersion_SideFileChangeMovesVersion(t *testing.T) {
	base := mustRender(t, baseInput())

	in := baseInput()
	in.Files[0].Data = []byte("# API\nDIFFERENT text\n")
	changed := mustRender(t, in)

	if base.SkillVersion == changed.SkillVersion {
		t.Fatal("side-file change did not move skill_version")
	}
}

func TestSkillVersion_ModeIsPartOfDigest(t *testing.T) {
	base := mustRender(t, baseInput())

	in := baseInput()
	in.Files[1].Mode = ModeFile // flip executable -> file
	changed := mustRender(t, in)

	if base.SkillVersion == changed.SkillVersion {
		t.Fatal("mode change did not move skill_version")
	}
}

func TestSkillVersion_TextCanonicalizationHashEqualsEmittedBytes(t *testing.T) {
	// CRLF + trailing whitespace + missing/extra trailing newlines must
	// canonicalize identically, so the emitted bytes ARE what is hashed.
	in1 := baseInput()
	in1.Files[0].Data = []byte("# API  \r\nsome text\t\r\n\r\n\r\n")
	in2 := baseInput()
	in2.Files[0].Data = []byte("# API\nsome text\n")

	a := mustRender(t, in1)
	b := mustRender(t, in2)
	if a.SkillVersion != b.SkillVersion {
		t.Fatalf("canonicalization mismatch: %q != %q", a.SkillVersion, b.SkillVersion)
	}
	// emitted bytes are the canonical form
	for _, f := range a.Files {
		if f.Path == "references/api.md" {
			if string(f.Data) != "# API\nsome text\n" {
				t.Fatalf("canonical bytes wrong: %q", f.Data)
			}
		}
	}
}

// craftPNG returns bytes with a NUL byte (PNG signature) so they classify as
// binary and must be copied verbatim, never canonicalized.
func craftPNG() []byte {
	// 8-byte PNG signature includes 0x00-free header? The signature is
	// \x89PNG\r\n\x1a\n followed by an IHDR chunk whose length bytes contain NUL.
	sig := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}
	ihdr := []byte{0x00, 0x00, 0x00, 0x0d, 'I', 'H', 'D', 'R', 0x00, 0x00, 0x00, 0x01}
	return append(sig, ihdr...)
}

func TestSkillVersion_BinaryCopiedVerbatim(t *testing.T) {
	png := craftPNG()
	in := baseInput()
	in.Files = append(in.Files, InputFile{Path: "assets/logo.png", Mode: ModeFile, Data: png})

	tree := mustRender(t, in)
	var found bool
	for _, f := range tree.Files {
		if f.Path == "assets/logo.png" {
			found = true
			if !f.Binary {
				t.Fatal("PNG not classified as binary")
			}
			if !bytes.Equal(f.Data, png) {
				t.Fatalf("binary asset corrupted:\n got %v\nwant %v", f.Data, png)
			}
		}
	}
	if !found {
		t.Fatal("png asset missing from tree")
	}

	// A binary byte change moves the version.
	in2 := baseInput()
	png2 := craftPNG()
	png2[len(png2)-1] = 0x02
	in2.Files = append(in2.Files, InputFile{Path: "assets/logo.png", Mode: ModeFile, Data: png2})
	tree2 := mustRender(t, in2)
	if tree.SkillVersion == tree2.SkillVersion {
		t.Fatal("binary byte change did not move skill_version")
	}
}

func TestSkillVersion_StampExcludedFromDigest(t *testing.T) {
	in1 := baseInput()
	in1.Provenance = &Provenance{Source: "github.com/acme/skills", Commit: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}

	in2 := baseInput()
	in2.Provenance = &Provenance{Source: "github.com/acme/skills", Commit: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}

	a := mustRender(t, in1)
	b := mustRender(t, in2)

	// Different provenance (commit) must NOT move the version: the stamp is
	// excluded from the digest.
	if a.SkillVersion != b.SkillVersion {
		t.Fatalf("provenance stamp leaked into digest: %q != %q", a.SkillVersion, b.SkillVersion)
	}

	// And the version with provenance equals the version without it.
	plain := mustRender(t, baseInput())
	if plain.SkillVersion != a.SkillVersion {
		t.Fatalf("stamping changed skill_version: plain=%q stamped=%q", plain.SkillVersion, a.SkillVersion)
	}

	// The stamp IS actually present in the emitted SKILL.md (and differs).
	skA := string(skillFile(t, a).Data)
	skB := string(skillFile(t, b).Data)
	if !strings.Contains(skA, "auto_skill") || !strings.Contains(skA, "managed: true") {
		t.Fatalf("stamp not embedded in SKILL.md:\n%s", skA)
	}
	if !strings.Contains(skA, "aaaaaaaa") || !strings.Contains(skB, "bbbbbbbb") {
		t.Fatalf("provenance commit not embedded")
	}
	if skA == skB {
		t.Fatal("expected differing SKILL.md bytes for differing provenance")
	}
	// The stamp carries the computed skill_version.
	if !strings.Contains(skA, a.SkillVersion) {
		t.Fatalf("stamp missing skill_version %q:\n%s", a.SkillVersion, skA)
	}
}

func TestIsTextClassification(t *testing.T) {
	cases := []struct {
		data []byte
		text bool
	}{
		{[]byte("plain ascii"), true},
		{[]byte("utf8 → ✓"), true},
		{[]byte{0x00, 0x01}, false},
		{[]byte{0xff, 0xfe, 0xfd}, false}, // invalid utf-8
		{[]byte(""), true},
	}
	for _, tc := range cases {
		if got := isText(tc.data); got != tc.text {
			t.Fatalf("isText(%v) = %v, want %v", tc.data, got, tc.text)
		}
	}
}
