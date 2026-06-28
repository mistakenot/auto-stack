package render

import (
	"strconv"
	"strings"
	"testing"

	"pgregory.net/rapid"
)

// pathGen constructs slash- and backslash-separated paths from valid segments,
// freely combining a leading "/", a "./" prefix, "."-only segments, and a
// trailing "/". These were the cases Phase 1 deliberately avoided to dodge a
// normalizePath non-idempotence bug (`/./x`, `././x` needed a second pass); that
// bug is now fixed (normalizePath trims slashes, then loops stripping leading
// "./" to a fixed point), so the generator is unrestricted.
func pathGen() *rapid.Generator[string] {
	segName := rapid.StringMatching(`[A-Za-z0-9][A-Za-z0-9._-]{0,7}`)
	return rapid.Custom(func(t *rapid.T) string {
		n := rapid.IntRange(0, 5).Draw(t, "segments")
		parts := make([]string, n)
		for i := range n {
			// A "."-only segment exercises the "./" / "././" collapse paths.
			if rapid.Bool().Draw(t, "dot_segment") {
				parts[i] = "."
			} else {
				parts[i] = segName.Draw(t, "segment")
			}
		}
		sep := rapid.SampledFrom([]string{"/", "\\"}).Draw(t, "sep")
		p := strings.Join(parts, sep)

		// All three affixes combine freely now, including the formerly-avoided
		// "/./x" shape (leading slash + dot-slash).
		if rapid.Bool().Draw(t, "dot_slash_prefix") {
			p = "./" + p
		}
		if rapid.Bool().Draw(t, "leading_slash") {
			p = "/" + p
		}
		if rapid.Bool().Draw(t, "trailing_slash") {
			p = p + "/"
		}
		return p
	})
}

// TestPropNormalizePathIdempotent asserts R4: normalizePath(normalizePath(p)) ==
// normalizePath(p) — normalizing an already-normalized path is a no-op, for the
// full unrestricted path space (leading slash, "./", "."-only segments).
func TestPropNormalizePathIdempotent(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		p := pathGen().Draw(t, "path")

		once := normalizePath(p)
		twice := normalizePath(once)

		if once != twice {
			t.Fatalf("not idempotent: normalizePath(%q) = %q, normalizePath(%q) = %q",
				p, once, once, twice)
		}
	})
}

// scalarGen produces a YAML-safe single-line scalar value: it starts with a
// letter and contains only characters that need no quoting, so a "key: value"
// line is always a valid YAML mapping entry and never embeds a "---" delimiter.
func scalarGen() *rapid.Generator[string] {
	return rapid.StringMatching(`[A-Za-z][A-Za-z0-9 ._]{0,19}`)
}

// identGen produces a valid lowercase identifier usable as both a YAML key and a
// {{ .field }} template variable.
func identGen() *rapid.Generator[string] {
	return rapid.StringMatching(`[a-z][a-z0-9]{0,6}`)
}

// stableBodyGen produces a multi-line body that survives text canonicalization
// unchanged: words contain no spaces/braces/dashes, lines are single-space
// joined (no trailing whitespace), there is no "{{ }}" template syntax, no
// "---" delimiter sequence, and no trailing blank lines. So the body appears
// verbatim in the canonicalized emitted SKILL.md.
func stableBodyGen() *rapid.Generator[string] {
	word := rapid.StringMatching(`[A-Za-z0-9.]{1,10}`)
	return rapid.Custom(func(t *rapid.T) string {
		nlines := rapid.IntRange(0, 5).Draw(t, "body_lines")
		lines := make([]string, nlines)
		for i := range nlines {
			nwords := rapid.IntRange(0, 4).Draw(t, "body_words")
			ws := make([]string, nwords)
			for j := range nwords {
				ws[j] = word.Draw(t, "body_word")
			}
			lines[i] = strings.Join(ws, " ")
		}
		// Drop trailing newlines so canonicalization (which trims them) is a
		// no-op on the body, keeping it byte-for-byte present in the output.
		return strings.TrimRight(strings.Join(lines, "\n"), "\n")
	})
}

// frontmatterGen produces a non-empty, valid YAML mapping as raw frontmatter
// text with no trailing newline and no "---" delimiter sequence — the input
// shape splitFrontmatter must recover from composeSkillMD.
func frontmatterGen() *rapid.Generator[string] {
	return rapid.Custom(func(t *rapid.T) string {
		n := rapid.IntRange(1, 4).Draw(t, "fm_keys")
		lines := make([]string, n)
		for i := range n {
			// Index-suffix the key so the mapping keys are always distinct
			// (construct-don't-reject: no draw filtering).
			k := identGen().Draw(t, "fm_key") + strconv.Itoa(i)
			v := scalarGen().Draw(t, "fm_val")
			lines[i] = k + ": " + v
		}
		return strings.Join(lines, "\n")
	})
}

// sideFilesGen produces 2-4 side files with guaranteed-unique, normalized,
// non-SKILL.md paths (so the rendered tree always has >=3 files — enough for the
// R1 kill test, where shuffling the hashed-file order must perturb the digest).
func sideFilesGen() *rapid.Generator[[]InputFile] {
	return rapid.Custom(func(t *rapid.T) []InputFile {
		nf := rapid.IntRange(2, 4).Draw(t, "nfiles")
		files := make([]InputFile, nf)
		for i := range nf {
			seg := identGen().Draw(t, "file_seg")
			text := rapid.StringMatching("[A-Za-z0-9 .\n]{0,30}").Draw(t, "file_text")
			var data []byte
			if rapid.Bool().Draw(t, "file_binary") {
				// A leading NUL forces the binary (verbatim) classification path.
				data = append([]byte{0x00}, []byte(text)...)
			} else {
				data = []byte(text)
			}
			files[i] = InputFile{
				Path: "ref" + strconv.Itoa(i) + "/" + seg + ".md",
				Mode: rapid.SampledFrom([]string{ModeFile, ModeExecutable, ""}).Draw(t, "file_mode"),
				Data: data,
			}
		}
		return files
	})
}

// renderInputGen builds a full RenderInput with LITERAL replacements only: a
// SKILL.md template whose customize: block declares 1-4 vars, a body that
// references exactly those vars via {{ .var }}, literal values for each, and a
// set of side files. File-ref replacement values are out of scope (they need a
// FileRefResolver — see "Known Gaps").
func renderInputGen() *rapid.Generator[RenderInput] {
	return rapid.Custom(func(t *rapid.T) RenderInput {
		name := scalarGen().Draw(t, "name")
		desc := scalarGen().Draw(t, "description")
		vars := rapid.SliceOfNDistinct(identGen(), 1, 4, func(s string) string { return s }).Draw(t, "vars")
		lit := rapid.StringMatching(`[A-Za-z0-9 ._-]{0,15}`)

		var fm strings.Builder
		fm.WriteString("name: " + name + "\n")
		fm.WriteString("description: " + desc + "\n")
		fm.WriteString("customize:\n")
		for _, v := range vars {
			fm.WriteString("  " + v + ":\n    required: false\n")
		}

		var body strings.Builder
		body.WriteString("Intro\n")
		values := make(map[string]ReplacementValue, len(vars))
		for _, v := range vars {
			body.WriteString("Set {{ ." + v + " }}\n")
			values[v] = ReplacementValue{Literal: lit.Draw(t, "literal")}
		}

		skillMD := "---\n" + fm.String() + "---\n" + body.String()
		return RenderInput{
			SkillMD: []byte(skillMD),
			Values:  values,
			Files:   sideFilesGen().Draw(t, "files"),
		}
	})
}

// TestPropRenderHashDeterministic asserts R1: rendering identical inputs twice
// yields the same (non-empty) SkillVersion. Catches map-iteration order leaks,
// timestamp leaks, and any other non-determinism in the hashing pipeline.
func TestPropRenderHashDeterministic(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		in := renderInputGen().Draw(t, "input")

		a, err := Render(in)
		if err != nil {
			t.Fatalf("render (first): %v", err)
		}
		b, err := Render(in)
		if err != nil {
			t.Fatalf("render (second): %v", err)
		}
		if a.SkillVersion == "" {
			t.Fatal("empty skill_version")
		}
		if a.SkillVersion != b.SkillVersion {
			t.Fatalf("non-deterministic skill_version: %q != %q", a.SkillVersion, b.SkillVersion)
		}
	})
}

// TestPropRenderPassthroughBody asserts R2: a RenderInput with no replacements
// (no customize: block, no {{ }} in the body) produces an emitted SKILL.md whose
// body contains the template body verbatim — the template engine never corrupts
// non-template content.
func TestPropRenderPassthroughBody(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		name := scalarGen().Draw(t, "name")
		desc := scalarGen().Draw(t, "description")
		body := stableBodyGen().Draw(t, "body")

		skillMD := "---\nname: " + name + "\ndescription: " + desc + "\n---\n" + body
		in := RenderInput{SkillMD: []byte(skillMD)}

		tree, err := Render(in)
		if err != nil {
			t.Fatalf("render: %v", err)
		}

		var skData []byte
		found := false
		for _, f := range tree.Files {
			if f.Path == SkillMDPath {
				skData = f.Data
				found = true
				break
			}
		}
		if !found {
			t.Fatal("SKILL.md missing from tree")
		}
		if !strings.Contains(string(skData), body) {
			t.Fatalf("emitted SKILL.md does not contain body verbatim\nbody:\n%q\nemitted:\n%q", body, skData)
		}
	})
}

// TestPropFrontmatterRoundTrip asserts R3:
// splitFrontmatter(composeSkillMD(front, body)) recovers the original front
// (modulo the trailing-newline trim composeSkillMD performs) and body, for any
// valid-YAML-mapping front and any body free of "---" delimiter sequences.
func TestPropFrontmatterRoundTrip(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		front := frontmatterGen().Draw(t, "front")
		body := stableBodyGen().Draw(t, "body")

		composed := composeSkillMD(front, body)
		gotFront, gotBody, err := splitFrontmatter([]byte(composed))
		if err != nil {
			t.Fatalf("splitFrontmatter(%q): %v", composed, err)
		}

		// composeSkillMD trims trailing newlines from front before fencing it;
		// splitFrontmatter recovers exactly that trimmed form.
		wantFront := strings.TrimRight(front, "\n")
		if gotFront != wantFront {
			t.Fatalf("front round-trip mismatch:\n want %q\n  got %q\ncomposed: %q", wantFront, gotFront, composed)
		}
		if gotBody != body {
			t.Fatalf("body round-trip mismatch:\n want %q\n  got %q\ncomposed: %q", body, gotBody, composed)
		}
	})
}
