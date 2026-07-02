PYTHON  := .venv/bin/python3
SCRIPTS := scripts

SYSTEM_SPEC    := spec/system.ttl
ETL_SPEC       := spec/auto-etl.ttl
SYSTEM_SHAPES  := shapes/shapes.ttl
ETL_SHAPES     := shapes/etl-shapes.ttl
TESTS          := tests

.PHONY: all format parse lint validate test clean

all: parse lint validate test

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

clean:
	@true
