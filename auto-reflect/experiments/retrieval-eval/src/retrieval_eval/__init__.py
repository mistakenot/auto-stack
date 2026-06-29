"""Retrieval evaluation harness for auto-reflect.

A durable, returnable experiment: compare candidate playbook-retrieval methods
against an LLM-oracle golden set, to inform future changes to the Go matcher
(`auto-reflect/internal/rules/match.go`). The Python `baseline` is held in
lockstep with the shipped matcher and pinned by the conformance harness.
"""
