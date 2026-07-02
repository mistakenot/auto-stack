PYTHON  := .venv/bin/python3
SCRIPTS := scripts

SYSTEM_SPEC    := spec/system.ttl
ETL_SPEC       := spec/auto-etl.ttl
SYSTEM_SHAPES  := shapes/shapes.ttl
ETL_SHAPES     := shapes/etl-shapes.ttl
TESTS          := tests

# Go struct → TTL extractor (see tools/schema2ttl).
SCHEMA_SRC     := $(CURDIR)/../auto-shared/model/schema.go
GENERATED_SPEC := $(CURDIR)/spec/auto-etl-generated.ttl
EXTRACTOR_DIR  := tools/schema2ttl

.PHONY: all format parse lint validate test extract validate-generated clean

all: parse lint validate test extract validate-generated

format:
	$(PYTHON) $(SCRIPTS)/format.py $(SYSTEM_SPEC)
	$(PYTHON) $(SCRIPTS)/format.py $(ETL_SPEC)

parse:
	$(PYTHON) $(SCRIPTS)/parse.py $(SYSTEM_SPEC)
	$(PYTHON) $(SCRIPTS)/parse.py $(ETL_SPEC)

lint:
	$(PYTHON) $(SCRIPTS)/lint.py $(SYSTEM_SPEC)
	$(PYTHON) $(SCRIPTS)/lint.py $(ETL_SPEC)

validate:
	$(PYTHON) $(SCRIPTS)/validate.py $(SYSTEM_SPEC) $(SYSTEM_SHAPES)
	$(PYTHON) $(SCRIPTS)/validate.py $(ETL_SPEC) $(ETL_SHAPES) $(SYSTEM_SPEC) $(SYSTEM_SHAPES)

test:
	$(PYTHON) $(SCRIPTS)/test_sparql.py $(SYSTEM_SPEC) $(TESTS)
	$(PYTHON) $(SCRIPTS)/test_sparql.py $(ETL_SPEC) $(TESTS) $(SYSTEM_SPEC)

# Regenerate the ETL field ontology directly from the Go parquet structs.
extract:
	cd $(EXTRACTOR_DIR) && GOWORK=off go run . -schema $(SCHEMA_SRC) -out $(GENERATED_SPEC)

# Prove the generated TTL parses, lints, and passes the same SHACL shapes and
# SPARQL tests as the hand-written spec.
validate-generated: extract
	$(PYTHON) $(SCRIPTS)/parse.py $(GENERATED_SPEC)
	$(PYTHON) $(SCRIPTS)/lint.py $(GENERATED_SPEC)
	$(PYTHON) $(SCRIPTS)/validate.py $(GENERATED_SPEC) $(ETL_SHAPES)
	$(PYTHON) $(SCRIPTS)/test_sparql.py $(GENERATED_SPEC) $(TESTS)

clean:
	@true
