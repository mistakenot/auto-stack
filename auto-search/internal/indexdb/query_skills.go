package indexdb

import (
	"database/sql"
	"fmt"
	"strings"
)

// SkillUsage holds a skill name and its usage count.
type SkillUsage struct {
	SkillName        string
	Count            int
	DistinctSessions int
}

// SkillFilter configures optional filters for ListSkillUsages.
type SkillFilter struct {
	StartMs *int64
	EndMs   *int64
	CWD     string
}

// ListSkillUsages returns all distinct skill names and their message counts,
// ordered by count descending.
func ListSkillUsages(db *sql.DB, filter *SkillFilter) ([]SkillUsage, error) {
	where := []string{"skill_name != ''"}
	var args []any

	if filter != nil {
		if filter.StartMs != nil {
			where = append(where, "timestamp >= ?")
			args = append(args, *filter.StartMs)
		}
		if filter.EndMs != nil {
			where = append(where, "timestamp < ?")
			args = append(args, *filter.EndMs)
		}
		if filter.CWD != "" {
			where = append(where, "workspace = ?")
			args = append(args, filter.CWD)
		}
	}

	q := fmt.Sprintf(`
		SELECT skill_name, COUNT(*) AS count, COUNT(DISTINCT session_id) AS distinct_sessions
		FROM messages
		WHERE %s
		GROUP BY skill_name
		ORDER BY count DESC
	`, strings.Join(where, " AND "))

	rows, err := db.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("query skill usages: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var usages []SkillUsage
	for rows.Next() {
		var u SkillUsage
		if err := rows.Scan(&u.SkillName, &u.Count, &u.DistinctSessions); err != nil {
			return nil, fmt.Errorf("scan skill usage: %w", err)
		}
		usages = append(usages, u)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate skill usages: %w", err)
	}
	return usages, nil
}
