package main

import (
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

func TestParseParquetTag(t *testing.T) {
	cases := []struct {
		tag      string
		wantName string
		wantDict bool
	}{
		{"id", "id", false},
		{"session_id,dict", "session_id", true},
		{"role,dict", "role", true},
		{"content", "content", false},
		{" host_id , dict ", "host_id", true}, // tolerant of spaces
		{"name=session_id", "session_id", false},
		{"name=host_id,dict", "host_id", true}, // legacy key=value + flag
		{"", "", false},
	}
	for _, c := range cases {
		gotName, gotDict := parseParquetTag(c.tag)
		if gotName != c.wantName || gotDict != c.wantDict {
			t.Errorf("parseParquetTag(%q) = (%q, %v), want (%q, %v)",
				c.tag, gotName, gotDict, c.wantName, c.wantDict)
		}
	}
}

func TestExtractFields(t *testing.T) {
	src := `package model
type Sample struct {
	ID        string ` + "`parquet:\"id\"`" + `
	SessionID string ` + "`parquet:\"session_id,dict\"`" + `
	Count     int32  ` + "`parquet:\"count\"`" + `
	Total     int64  ` + "`parquet:\"total\"`" + `
	Flag      bool   ` + "`parquet:\"flag\"`" + `
	OptName   *string ` + "`parquet:\"opt_name\"`" + `
	Untagged  string
}`
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "sample.go", src, 0)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	structs := collectStructs(f)
	st, ok := structs["Sample"]
	if !ok {
		t.Fatal("Sample struct not found")
	}
	fields, err := extractFields(st)
	if err != nil {
		t.Fatalf("extractFields: %v", err)
	}
	// Untagged field is dropped; 6 tagged fields remain.
	if len(fields) != 6 {
		t.Fatalf("got %d fields, want 6", len(fields))
	}

	byName := map[string]field{}
	for _, fl := range fields {
		byName[fl.parquet] = fl
	}

	if got := byName["id"]; got.goType != "string" || !got.isRequired || got.dictCoded {
		t.Errorf("id: %+v", got)
	}
	if got := byName["session_id"]; !got.dictCoded || !got.isRequired {
		t.Errorf("session_id should be dict-encoded + required: %+v", got)
	}
	if got := byName["count"]; got.goType != "int32" {
		t.Errorf("count goType = %q, want int32", got.goType)
	}
	if got := byName["total"]; got.goType != "int64" {
		t.Errorf("total goType = %q, want int64", got.goType)
	}
	if got := byName["flag"]; got.goType != "bool" {
		t.Errorf("flag goType = %q, want bool", got.goType)
	}
	// Pointer field => not required, underlying type resolved.
	if got := byName["opt_name"]; got.isRequired || got.goType != "string" {
		t.Errorf("opt_name should be optional string: %+v", got)
	}
}

func TestRenderProducesValidatableTriples(t *testing.T) {
	src := `package model
const SchemaVersion = 6
type AgentSession struct {
	ID string ` + "`parquet:\"id\"`" + `
}
type AgentMessage struct {
	ID string ` + "`parquet:\"id\"`" + `
}`
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "schema.go", src, 0)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	out, err := render(collectStructs(f), collectSchemaVersion(f))
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	for _, want := range []string{
		"etl:SessionsDataset",
		"etl:MessagesDataset",
		"etl:sess_id",
		"etl:msg_id",
		"etl:schemaVersion 6 ;",
		"a etl:Dataset ;",
		"a etl:Field ;",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("rendered output missing %q", want)
		}
	}
}
