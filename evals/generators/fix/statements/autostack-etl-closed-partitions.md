### `auto etl run` permanently drops Messages that arrive after a week has ended

The ETL writes Messages into weekly parquet partitions under the output
directory. When new Messages for a past week arrive after that week's partition
already exists, for example a Session that kept running past midnight on Sunday
or a source that was synced late, they never appear in the parquet. Every later
run parses and transforms them, then drops them again. Measured on a live
corpus, 17.8% of Messages were missing, and one week was missing 81%. Sessions
partitions have the same problem.

Expected behaviour of `writer.Write`:

- New rows for an existing partition of a past week or month end up in that
  partition, alongside the rows already stored there. Nothing already stored is
  lost.
- `auto etl run` runs from cron every 10 minutes and can overlap a manual run.
  Concurrent writes to the same partition must not lose any row.
- If an existing partition file cannot be read as parquet, the write fails with
  an error and leaves that file untouched, rather than replacing it and
  discarding what it held.
