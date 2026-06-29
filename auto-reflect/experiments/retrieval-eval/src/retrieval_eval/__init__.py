"""Retrieval evaluation harness for auto-reflect.

A durable, returnable experiment: compare candidate playbook-retrieval methods
against an LLM-oracle golden set, to inform future changes to the Go matcher
(`auto-reflect/internal/rules/match.go`). The Python `baseline` is the frozen v1
hard-gate reference; the shipped matcher is `variants[SHIPPED]` (`idf-tag`). The
conformance harness pins the Go CLI against `variants[SHIPPED]`, while
`hard-gate == baseline` stays a v1 self-check.
"""
