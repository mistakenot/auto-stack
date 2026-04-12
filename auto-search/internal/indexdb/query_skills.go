package indexdb

import (
	"database/sql"
	"fmt"
)

// SkillUsage holds a skill name and its usage count.
type SkillUsage struct {
	SkillName string
	Count     int
}

// ListSkillUsages returns all distinct skill names and their message counts,
// ordered by count descending.
func ListSkillUsages(db *sql.DB) ([]SkillUsage, error) {
	rows, err := db.Query(`
		SELECT skill_name, COUNT(*) AS count
		FROM messages
		WHERE skill_name != ''
		GROUP BY skill_name
		ORDER BY count DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("query skill usages: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var usages []SkillUsage
	for rows.Next() {
		var u SkillUsage
		if err := rows.Scan(&u.SkillName, &u.Count); err != nil {
			return nil, fmt.Errorf("scan skill usage: %w", err)
		}
		usages = append(usages, u)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate skill usages: %w", err)
	}
	return usages, nil
}
