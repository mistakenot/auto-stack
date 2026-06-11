package model

const HookSchemaVersion int32 = 1

// HookEventRow represents a single normalized hook event in parquet.
type HookEventRow struct {
	ID            string `parquet:"id"`
	HostID        string `parquet:"host_id,dict"`
	Agent         string `parquet:"agent,dict"`
	Event         string `parquet:"event,dict"`
	SessionID     string `parquet:"session_id,dict"`
	Cwd           string `parquet:"cwd"`
	Project       string `parquet:"project,dict"`
	Tool          string `parquet:"tool,dict"`
	PathsJSON     string `parquet:"paths_json"`
	CapturedAt    int64  `parquet:"captured_at"`
	RawJSON       string `parquet:"raw_json"`
	SourceFile    string `parquet:"source_file,dict"`
	Year          int32  `parquet:"year"`
	Month         int32  `parquet:"month"`
	SchemaVersion int32  `parquet:"schema_version"`
}
