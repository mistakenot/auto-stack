package writer

import (
	"fmt"
	"log"
	"path/filepath"

	"github.com/mistakenot/auto-etl/internal/model"
)

// WriteHooks outputs hook event rows as monthly-partitioned parquet files,
// using read-merge-write by ID to deduplicate.
func WriteHooks(outputDir string, rows []model.HookEventRow) error {
	if len(rows) == 0 {
		return nil
	}

	partitions := groupHooksByMonth(rows)
	for key, hookRows := range partitions {
		dir := filepath.Join(outputDir, "hooks", fmt.Sprintf("year=%d", key.Year), fmt.Sprintf("month=%02d", key.Month))
		path := filepath.Join(dir, "hooks.parquet")

		existing, err := readExistingParquet[model.HookEventRow](path)
		if err != nil {
			log.Printf("warning: could not read existing %s: %v (overwriting)", path, err)
		}

		merged := mergeByID(existing, hookRows, func(r *model.HookEventRow) string { return r.ID })
		if err := writeParquet(path, merged); err != nil {
			return fmt.Errorf("write hooks year=%d/month=%02d: %w", key.Year, key.Month, err)
		}
		log.Printf("wrote %s (%d rows)", path, len(merged))
	}
	return nil
}

func groupHooksByMonth(rows []model.HookEventRow) map[partKey][]model.HookEventRow {
	m := make(map[partKey][]model.HookEventRow)
	for i := range rows {
		k := partKey{Year: int(rows[i].Year), Month: int(rows[i].Month)}
		m[k] = append(m[k], rows[i])
	}
	return m
}
