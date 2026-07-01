# auto-etl Alloy Models

This folder contains Alloy models owned by `auto-etl`.

The first model, [`session_message_conformance.als`](session_message_conformance.als), promotes the
Session/Message conformance spike into a repeatable Project artifact. It models the normalized
`Session` and `Message` datasets as small relational worlds, plus a `DataError` side channel for
recoverable malformed source data.

From the Project root, run:

```bash
cd auto-etl
./alloy/run.sh
```

From inside `auto-etl`, the same command is:

```bash
./alloy/run.sh
```

The runner stores the Alloy jar under `alloy/.cache/` and generated instances under
`alloy/output/`. Both directories are ignored by git.

## Interpreting Results

For an Alloy `check` command:

- `SAT` means Alloy found a counterexample.
- `UNSAT` means Alloy found no counterexample within the selected finite scope.

For an Alloy `run` command:

- `SAT` means Alloy found an example world.
- `UNSAT` means Alloy could not construct that example within the selected finite scope.

The intended shape is:

- example `run` commands return `SAT`;
- canonical-output `check` commands return `UNSAT`.

That means recoverable source problems can exist as `DataError` rows, while canonical
`Session`/`Message` output remains safe for downstream commands to query.
