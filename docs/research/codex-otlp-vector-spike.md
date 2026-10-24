---
hash: "a749367c"
id: "a06313d0"
read_when: "routing coding-agent OpenTelemetry into Vector, or evaluating Codex native telemetry as an ingestion source"
summary: "Spike validating that Codex CLI exports OTLP logs, metrics and traces to a local Vector pipeline with file sinks; verdict GO."
title: "Tech Spike Report: Codex OTLP Export to Vector File Sinks"
---

# Tech Spike Report: Codex OTLP Export to Vector File Sinks

**Date:** 2026-10-05
**Scope:** Validate that a local Codex CLI Session can export OpenTelemetry to Vector and leave inspectable local files.
**Verdict:** GO — Codex 0.154.0 exported logs, metrics, and traces to Vector 0.50.0 over loopback OTLP/HTTP protobuf.

## Context

The spike tests whether native Codex telemetry can enter the same lightweight Vector pipeline proposed for other coding agents, before durable object storage is introduced.

## Assumptions Tested

| # | Assumption | Result | Confidence |
|---|-----------|--------|------------|
| 1 | Codex CLI can export its Session telemetry to an OTLP endpoint. | VALIDATED | High |
| 2 | Vector can receive this data and write inspectable local files by signal. | VALIDATED | High |
| 3 | The records contain useful coding-agent Session signals. | VALIDATED | High |

## Detailed Findings

### Assumption 1: Codex CLI can export its Session telemetry to an OTLP endpoint

**Result:** VALIDATED

**What we did:** Ran Codex 0.154.0 in read-only mode against this Project with an OTLP/HTTP log, trace, and metric exporter targeting `127.0.0.1:4318`. The prompt required bounded exploration of the top-level README and ubiquitous-language document.

**What we found:** Codex completed the Session successfully and emitted 18 log records, 221 metric records, and 748 trace spans. The captured metrics include token usage, end-to-end turn duration, tool-call count, time-to-first-token, startup, hook, skill, and WebSocket measurements. Logs include `codex.conversation_starts`, `codex.user_prompt`, `codex.api_request`, and startup/timing events.

**Evidence:** `.tmp/codex-vector-otel/output/logs.jsonl`, `.tmp/codex-vector-otel/output/metrics.jsonl`, `.tmp/codex-vector-otel/output/traces.jsonl`, and `.tmp/codex-vector-otel/codex-events.jsonl`.

**Implications:** Codex requires explicit `otel.*` configuration; setting only standard `OTEL_*` environment variables does not choose a destination. Standard variables still apply to resource attributes and metric cadence. The successful configuration is preserved in `.tmp/codex-vector-otel/run-codex.sh`.

### Assumption 2: Vector can receive this data and write inspectable local files by signal

**Result:** VALIDATED

**What we did:** Ran Vector 0.50.0 in Docker, exposing OTLP/gRPC and OTLP/HTTP only on loopback. Its OpenTelemetry source dispatched each signal to a JSONL file sink mounted under `.tmp/codex-vector-otel/output/`.

**What we found:** Vector created three signal files after its idle-close flush interval:

- `logs.jsonl`: 18 records, 19,942 bytes
- `metrics.jsonl`: 221 records, 282,430 bytes
- `traces.jsonl`: 748 records, 740,717 bytes

**Evidence:** `.tmp/codex-vector-otel/vector.yaml` and the three output files above.

**Implications:** Vector is sufficient for a local, inspectable archive stage. Use `protocol = "binary"` for Codex OTLP/HTTP with this Vector receiver. The initial JSON-protocol run created no Vector output; it is retained only as a failed preliminary attempt in the original `codex-events.jsonl`/stderr files and is not a supported spike configuration.

### Assumption 3: The records contain useful coding-agent Session signals

**Result:** VALIDATED

**What we did:** Counted signal records and inspected event names, metric names, trace spans, and correlation attributes.

**What we found:** The records have `conversation.id`, model, originator, Session source, trace IDs, and resource-level deployment/namespace metadata. Metrics recorded this exploration as 5,065 ms end-to-end, 24,662 total tokens, and one tool call. Trace output also includes substantial Codex-internal implementation detail (for example `append_items`, `persist_rollout_items`, authentication, and WebSocket spans).

**Evidence:** `logs.jsonl` carries `conversation.id` and `trace_id`; `metrics.jsonl` carries the `codex.turn.*` series; `traces.jsonl` carries span topology.

**Implications:** The source is immediately useful for Session-level cost, latency, and activity analysis. It is noisy for code-intelligence purposes, so a production Vector transform should retain Codex business events and high-level turn metrics while dropping or routing internal spans separately.

## Surprises & Secondary Findings

- Codex's native OTEL configuration is documented under `otel.exporter`, `otel.trace_exporter`, and `otel.metrics_exporter`; it is not enabled merely by an `OTEL_EXPORTER_OTLP_ENDPOINT` environment variable.
- Enabling `otel.log_user_prompt = true` exported the entire prompt. Each record also contained the authenticated user email and account ID. Do not enable prompt logging by default in a shared archive.
- `otel.log_agent_responses = true` did not produce an agent-response record for this `codex exec` run, despite the completed terminal response. Treat response capture as a separate scenario to test before depending on it.
- Vector's local file sink flushes after idle closure, so files did not appear immediately after Codex exited.

## Risks Identified

- Raw prompts, identity fields, file paths, command arguments, and tool output can be sensitive. Redact them in Vector before any S3 sink.
- Trace export is high-volume and includes low-level Codex internals. Retention and sampling/filtering rules are needed before long-term storage.
- Vector's JSONL archive is a practical normalized event format, not a lossless OTLP wire archive.

## Recommendations

Proceed with Vector as a local OTLP gateway. Begin with three pipelines: log business events, selected turn-level metrics, and a filtered trace stream. Add VRL redaction for prompt/body, `user.email`, account IDs, authorization fields, and tool arguments before adding S3. Keep this spike's binary OTLP/HTTP configuration as the Codex adapter baseline.

## Appendix: Reproduction

The self-contained artifacts live in `.tmp/codex-vector-otel/`.

1. Start a Vector container with `.tmp/codex-vector-otel/vector.yaml` mounted at `/etc/vector/vector.yaml`, ports 4317 and 4318 bound to loopback, and `output/` mounted at `/data`.
2. Run `.tmp/codex-vector-otel/run-codex.sh` from the Project root.
3. Wait at least 30 seconds for Vector's file sink idle-close flush, then inspect `output/*.jsonl`.

The script sets `OTEL_SERVICE_NAME`, `OTEL_RESOURCE_ATTRIBUTES`, and `OTEL_METRIC_EXPORT_INTERVAL`, then configures Codex's three OTLP/HTTP protobuf exporters with `-c` overrides.
