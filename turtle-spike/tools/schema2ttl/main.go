// Command schema2ttl parses the auto-shared parquet model structs via go/ast
// and emits an RDF/Turtle description of the ETL datasets and their fields in
// the etl: namespace (http://auto.dev/ontology/etl#).
//
// It is the source-of-truth extractor for the turtle-spike ontology: rather
// than hand-maintaining etl:Field triples, we derive them directly from the Go
// struct definitions and their parquet tags. The output is self-contained (it
// re-declares the DataType and PartitionScheme vocabulary it references) so it
// validates standalone against shapes/etl-shapes.ttl.
//
// Usage:
//
//	schema2ttl -schema <path/to/schema.go> -out <path/to/output.ttl>
package main

import (
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"reflect"
	"strconv"
	"strings"
)

// datasetConfig carries the dataset-level metadata that is NOT derivable from
// the struct itself (output path, partition scheme). Keyed by Go struct name.
type datasetConfig struct {
	structName  string // Go struct to extract
	datasetIRI  string // etl: local name for the dataset individual
	label       string // rdfs:label / dataset name
	fieldPrefix string // per-field IRI prefix, e.g. "sess" -> etl:sess_id
	outputPath  string
	partition   string // etl: local name of the PartitionScheme individual
	description string
}

// ordered so the emitted TTL is deterministic.
var datasets = []datasetConfig{
	{
		structName:  "AgentSession",
		datasetIRI:  "SessionsDataset",
		label:       "sessions",
		fieldPrefix: "sess",
		outputPath:  "~/.auto/etl/output/sessions/",
		partition:   "MonthlyPartition",
		description: "One row per coding agent session (generated from AgentSession).",
	},
	{
		structName:  "AgentMessage",
		datasetIRI:  "MessagesDataset",
		label:       "messages",
		fieldPrefix: "msg",
		outputPath:  "~/.auto/etl/output/messages/",
		partition:   "WeeklyPartition",
		description: "One row per message within a session (generated from AgentMessage).",
	},
}

// goTypeMapping maps a Go primitive to its etl:DataType individual and the
// XSD datatype it corresponds to in the parquet storage layer.
type goTypeMapping struct {
	dataType string // etl: local name, e.g. "StringType"
	xsd      string // xsd: local name, e.g. "string"
	label    string // human label for the DataType individual
}

var typeMap = map[string]goTypeMapping{
	"string": {"StringType", "string", "string"},
	"int32":  {"Int32Type", "int", "int32"},
	"int64":  {"Int64Type", "long", "int64"},
	"bool":   {"BoolType", "boolean", "bool"},
}

// partitionGranularity gives the granularity label for each partition scheme.
var partitionGranularity = map[string]string{
	"MonthlyPartition": "month",
	"WeeklyPartition":  "week",
}

// field is one extracted parquet column.
type field struct {
	goName     string
	parquet    string // parquet column name
	goType     string // resolved Go primitive (pointer unwrapped)
	dictCoded  bool
	isRequired bool // non-pointer => required
}

func main() {
	schemaPath := flag.String("schema", "../auto-shared/model/schema.go", "path to the Go schema source file")
	outPath := flag.String("out", "spec/auto-etl-generated.ttl", "path to write the generated TTL (- for stdout)")
	flag.Parse()

	fset := token.NewFileSet()
	fileAST, err := parser.ParseFile(fset, *schemaPath, nil, parser.ParseComments)
	if err != nil {
		fatalf("parse %s: %v", *schemaPath, err)
	}

	structs := collectStructs(fileAST)
	schemaVersion := collectSchemaVersion(fileAST)

	ttl, err := render(structs, schemaVersion)
	if err != nil {
		fatalf("render: %v", err)
	}

	if *outPath == "-" {
		fmt.Print(ttl)
		return
	}
	if err := os.WriteFile(*outPath, []byte(ttl), 0o644); err != nil {
		fatalf("write %s: %v", *outPath, err)
	}
	fmt.Fprintf(os.Stderr, "wrote %s\n", *outPath)
}

// collectStructs returns a map of struct name -> its *ast.StructType.
func collectStructs(f *ast.File) map[string]*ast.StructType {
	out := map[string]*ast.StructType{}
	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.TYPE {
			continue
		}
		for _, spec := range gd.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok {
				continue
			}
			st, ok := ts.Type.(*ast.StructType)
			if !ok {
				continue
			}
			out[ts.Name.Name] = st
		}
	}
	return out
}

// collectSchemaVersion reads `const SchemaVersion = N`; defaults to 0 if absent.
func collectSchemaVersion(f *ast.File) int {
	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			continue
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for i, name := range vs.Names {
				if name.Name != "SchemaVersion" || i >= len(vs.Values) {
					continue
				}
				if lit, ok := vs.Values[i].(*ast.BasicLit); ok && lit.Kind == token.INT {
					if n, err := strconv.Atoi(lit.Value); err == nil {
						return n
					}
				}
			}
		}
	}
	return 0
}

// extractFields walks a struct's fields, resolving type + parquet tag.
func extractFields(st *ast.StructType) ([]field, error) {
	var fields []field
	for _, astField := range st.Fields.List {
		if len(astField.Names) == 0 {
			continue // embedded field; none expected in the target structs
		}
		if astField.Tag == nil {
			continue // no struct tag => not a parquet column
		}
		raw, err := strconv.Unquote(astField.Tag.Value)
		if err != nil {
			return nil, fmt.Errorf("unquote tag %q: %w", astField.Tag.Value, err)
		}
		parquetTag := reflect.StructTag(raw).Get("parquet")
		if parquetTag == "" {
			continue
		}
		name, dict := parseParquetTag(parquetTag)
		if name == "" {
			continue
		}

		goType, isPtr := resolveType(astField.Type)
		if _, known := typeMap[goType]; !known {
			return nil, fmt.Errorf("field %s: unsupported Go type %q (parquet %q)", astField.Names[0].Name, goType, name)
		}

		fields = append(fields, field{
			goName:     astField.Names[0].Name,
			parquet:    name,
			goType:     goType,
			dictCoded:  dict,
			isRequired: !isPtr,
		})
	}
	return fields, nil
}

// parseParquetTag extracts the column name and dict flag from a parquet tag.
// Supports both the segmentio-style `name,flag,flag` form used by this
// codebase and the legacy `name=col, type=...` key=value form.
func parseParquetTag(tag string) (name string, dict bool) {
	for i, seg := range strings.Split(tag, ",") {
		seg = strings.TrimSpace(seg)
		if seg == "" {
			continue
		}
		if k, v, found := strings.Cut(seg, "="); found {
			if strings.TrimSpace(k) == "name" {
				name = strings.TrimSpace(v)
			}
			continue
		}
		if i == 0 {
			name = seg // bare first segment is the column name
			continue
		}
		if seg == "dict" {
			dict = true
		}
	}
	return name, dict
}

// resolveType returns the underlying Go primitive name and whether the field
// was a pointer (pointers are treated as optional / not required).
func resolveType(expr ast.Expr) (goType string, isPtr bool) {
	if star, ok := expr.(*ast.StarExpr); ok {
		t, _ := resolveType(star.X)
		return t, true
	}
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name, false
	case *ast.SelectorExpr:
		return t.Sel.Name, false // e.g. time.Time -> "Time" (unmapped => error upstream)
	default:
		return "", false
	}
}

// render produces the full TTL document.
func render(structs map[string]*ast.StructType, schemaVersion int) (string, error) {
	var b strings.Builder

	b.WriteString(prefixBlock)
	b.WriteString(headerBlock)

	// --- DataType vocabulary (self-contained, with XSD mapping) ---------
	b.WriteString(sectionRule("Data types (Go primitive → etl:DataType → XSD)"))
	for _, goType := range []string{"string", "int32", "int64", "bool"} {
		m := typeMap[goType]
		b.WriteString(fmt.Sprintf(
			"etl:%s\n    a          etl:DataType ;\n    rdfs:label \"%s\" ;\n    etl:xsdType xsd:%s .\n\n",
			m.dataType, m.label, m.xsd))
	}

	// --- PartitionScheme vocabulary -------------------------------------
	b.WriteString(sectionRule("Partition schemes"))
	for _, p := range []string{"MonthlyPartition", "WeeklyPartition"} {
		b.WriteString(fmt.Sprintf(
			"etl:%s\n    a etl:PartitionScheme ;\n    etl:partitionGranularity \"%s\"^^xsd:string ;\n    rdfs:label \"%s\" .\n\n",
			p, partitionGranularity[p], p))
	}

	// --- Datasets + fields ----------------------------------------------
	for _, ds := range datasets {
		st, ok := structs[ds.structName]
		if !ok {
			return "", fmt.Errorf("struct %s not found in schema", ds.structName)
		}
		fields, err := extractFields(st)
		if err != nil {
			return "", fmt.Errorf("%s: %w", ds.structName, err)
		}
		if len(fields) == 0 {
			return "", fmt.Errorf("%s: no parquet fields extracted", ds.structName)
		}
		renderDataset(&b, ds, fields, schemaVersion)
	}

	return b.String(), nil
}

func renderDataset(b *strings.Builder, ds datasetConfig, fields []field, schemaVersion int) {
	b.WriteString(sectionRule(fmt.Sprintf("%s dataset — generated from %s", ds.label, ds.structName)))

	// Dataset individual.
	fmt.Fprintf(b, "etl:%s\n    a etl:Dataset ;\n    rdfs:label \"%s\" ;\n    etl:outputPath \"%s\"^^xsd:string ;\n    etl:partitionedBy etl:%s ;\n    etl:schemaVersion %d ;\n    dcterms:description \"%s\" ;\n    etl:hasField ",
		ds.datasetIRI, ds.label, ds.outputPath, ds.partition, schemaVersion, ds.description)

	fieldIRIs := make([]string, len(fields))
	for i, f := range fields {
		fieldIRIs[i] = fmt.Sprintf("etl:%s_%s", ds.fieldPrefix, f.parquet)
	}
	b.WriteString(strings.Join(fieldIRIs, " ,\n                 "))
	b.WriteString(" .\n\n")

	// Field individuals, in struct order.
	for _, f := range fields {
		m := typeMap[f.goType]
		fmt.Fprintf(b, "etl:%s_%s\n    a etl:Field ;\n    etl:fieldName \"%s\"^^xsd:string ;\n    etl:fieldType etl:%s ;\n",
			ds.fieldPrefix, f.parquet, f.parquet, m.dataType)
		if f.dictCoded {
			b.WriteString("    etl:isDictEncoded true ;\n")
		}
		fmt.Fprintf(b, "    etl:isRequired %t ;\n    rdfs:label \"%s\" .\n\n",
			f.isRequired, labelFor(f.parquet))
	}
}

// labelFor turns a snake_case column name into a readable label.
func labelFor(parquet string) string {
	return strings.ReplaceAll(parquet, "_", " ")
}

func sectionRule(title string) string {
	const rule = "# --------------------------------------------------------------------------\n"
	return "\n" + rule + "# " + title + "\n" + rule + "\n"
}

const prefixBlock = `@prefix rdf:     <http://www.w3.org/1999/02/22-rdf-syntax-ns#> .
@prefix rdfs:    <http://www.w3.org/2000/01/rdf-schema#> .
@prefix owl:     <http://www.w3.org/2002/07/owl#> .
@prefix xsd:     <http://www.w3.org/2001/XMLSchema#> .
@prefix dcterms: <http://purl.org/dc/terms/> .
@prefix auto:    <http://auto.dev/ontology#> .
@prefix etl:     <http://auto.dev/ontology/etl#> .
`

const headerBlock = `
# ==========================================================================
# GENERATED FILE — DO NOT EDIT BY HAND.
#
# Produced by tools/schema2ttl from auto-shared/model/schema.go.
# Regenerate with: make extract
#
# Emits etl:Dataset + etl:Field triples derived directly from the parquet
# struct tags on AgentSession / AgentMessage. Self-contained: re-declares the
# etl:DataType and etl:PartitionScheme vocabulary it references so it validates
# standalone against shapes/etl-shapes.ttl.
# ==========================================================================
`

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "schema2ttl: "+format+"\n", args...)
	os.Exit(1)
}
