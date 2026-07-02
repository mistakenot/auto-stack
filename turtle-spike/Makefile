PYTHON  := /home/vscode/src/auto-stack/turtle-spike/.venv/bin/python3
PYSHACL := /home/vscode/src/auto-stack/turtle-spike/.venv/bin/pyshacl
SPEC    := spec/system.ttl
SHAPES  := shapes/shapes.ttl
TESTS   := tests/
SCRIPTS := scripts/

.PHONY: all format parse lint validate test clean

all: parse lint validate test

format:
	$(PYTHON) $(SCRIPTS)/format.py $(SPEC)

parse:
	$(PYTHON) $(SCRIPTS)/parse.py $(SPEC)

lint:
	$(PYTHON) $(SCRIPTS)/lint.py $(SPEC)

validate:
	$(PYTHON) $(SCRIPTS)/validate.py $(SPEC) $(SHAPES)

test:
	$(PYTHON) $(SCRIPTS)/test_sparql.py $(SPEC) $(TESTS)

clean:
	@true
