package mail_test

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/mistakenot/auto-mail/mail"
)

// moduleRoot is auto-mail/ — this test file lives in auto-mail/mail/.
func moduleRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatalf("resolve module root: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("module root %q has no go.mod: %v", root, err)
	}
	return root
}

func goFiles(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "testdata" {
				return fs.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(path, ".go") {
			out = append(out, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	if len(out) == 0 {
		t.Fatalf("no Go files under %s", root)
	}
	return out
}

// messageWord matches "message" as a whole word component of an identifier,
// case-insensitively and across camelCase boundaries (Message, mailMessage,
// MessageID, messages).
var messageWord = regexp.MustCompile(`(?i)(^|[^a-z])message`)

// TestNoMessageEntity is the greppable half of G16 (AC-1): nothing in auto-mail
// names the stored unit a message. That word is already bound to "a single
// role-tagged exchange within a Session" in the ubiquitous language, which is
// the collision D-4 exists to avoid. The stored unit is Mail.
//
// The check is scoped to names that outlive a function: declared types,
// functions and methods, struct field names, and JSON field tags. The one
// allowed use is the `--message` body flag and the {"message": …} body key it
// writes — those name the *body's own field*, not the entity — so string
// literals and local variables are deliberately out of scope.
func TestNoMessageEntity(t *testing.T) {
	root := moduleRoot(t)
	fset := token.NewFileSet()

	for _, path := range goFiles(t, root) {
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		rel, _ := filepath.Rel(root, path)

		report := func(kind, name string, pos token.Pos) {
			if messageWord.MatchString(name) {
				t.Errorf("%s:%d: %s %q names the stored unit a message — "+
					"use Mail (docs/concepts/UBIQUITOUS_LANGUAGE.md); "+
					"`--message` is the body field's own name and stays a flag",
					rel, fset.Position(pos).Line, kind, name)
			}
		}

		ast.Inspect(file, func(n ast.Node) bool {
			switch node := n.(type) {
			case *ast.TypeSpec:
				report("type", node.Name.Name, node.Name.Pos())
			case *ast.FuncDecl:
				report("func", node.Name.Name, node.Name.Pos())
			case *ast.StructType:
				for _, field := range node.Fields.List {
					for _, name := range field.Names {
						report("struct field", name.Name, name.Pos())
					}
					if field.Tag == nil {
						continue
					}
					tag := reflect.StructTag(strings.Trim(field.Tag.Value, "`"))
					jsonName, _, _ := strings.Cut(tag.Get("json"), ",")
					if jsonName != "" {
						report("json field", jsonName, field.Tag.Pos())
					}
				}
			}
			return true
		})
	}
}

// sqlMessageTable matches a SQL statement naming a `message`/`messages` table.
var sqlMessageTable = regexp.MustCompile(`(?i)\b(create\s+table(\s+if\s+not\s+exists)?|insert\s+into|update|from|join)\s+messages?\b`)

// alphaEventType matches an event-type string literal in the module.
var alphaEventType = regexp.MustCompile(`"(alpha\.[a-z.]+)"`)

// TestSchemaAndEventTypesUseTheMailVocabulary: no table is named message, and
// every event type carries the `alpha.` prefix G10 requires and the `mail`
// noun D-4 settled.
func TestSchemaAndEventTypesUseTheMailVocabulary(t *testing.T) {
	root := moduleRoot(t)
	for _, path := range goFiles(t, root) {
		if strings.HasSuffix(path, "mail_test.go") {
			continue // this file names the forbidden word in its own patterns
		}
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		rel, _ := filepath.Rel(root, path)
		if loc := sqlMessageTable.FindString(string(src)); loc != "" {
			t.Errorf("%s: SQL names a message table (%q) — the stored unit is `mail`", rel, loc)
		}
		for _, m := range alphaEventType.FindAllStringSubmatch(string(src), -1) {
			if !strings.HasPrefix(m[1], "alpha.mail.") {
				t.Errorf("%s: event type %q is not under the alpha.mail.* namespace", rel, m[1])
			}
			if messageWord.MatchString(m[1]) {
				t.Errorf("%s: event type %q names a message — the stored unit is mail", rel, m[1])
			}
		}
	}
}

// TestNewDirectOpensTheAlphaStore: the alpha marker is in the store filename,
// not only in the docs (G10 / D-2), and the client needs no daemon (D-11).
func TestNewDirectOpensTheAlphaStore(t *testing.T) {
	home := t.TempDir()

	client, err := mail.NewDirect(home)
	if err != nil {
		t.Fatalf("NewDirect: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	want := filepath.Join(home, ".auto", "mail", "alpha-store.db")
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("store not created at %s: %v", want, err)
	}

	deliveries, err := client.List(context.Background(), mail.ListInput{})
	if err != nil {
		t.Fatalf("List on a fresh store: %v", err)
	}
	if deliveries == nil {
		t.Error("List returned a nil slice; want an empty one so it marshals as []")
	}
	if len(deliveries) != 0 {
		t.Errorf("List returned %d deliveries on a fresh store, want 0", len(deliveries))
	}
}

// TestUnimplementedVerbsSaySo: the seam is complete before the implementation
// is, so the verbs phase 2 fills must fail loudly rather than silently
// succeeding with an empty result.
func TestUnimplementedVerbsSaySo(t *testing.T) {
	client, err := mail.NewDirect(t.TempDir())
	if err != nil {
		t.Fatalf("NewDirect: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	ctx := context.Background()

	if _, err := client.Subscribe(ctx, mail.SubscribeInput{Address: "auto-web/bugs"}); err == nil {
		t.Error("Subscribe returned nil error in the walking skeleton")
	}
	if _, err := client.Send(ctx, mail.SendInput{To: "auto-web/bugs"}); err == nil {
		t.Error("Send returned nil error in the walking skeleton")
	}
	if _, err := client.Ack(ctx, mail.AckInput{MailID: "01K9X2QF7M3B0V8N"}); err == nil {
		t.Error("Ack returned nil error in the walking skeleton")
	}
	if _, err := client.Reset(ctx); err == nil {
		t.Error("Reset returned nil error in the walking skeleton")
	}
}
