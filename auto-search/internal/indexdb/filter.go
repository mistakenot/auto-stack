package indexdb

import "strings"

// SubstringFilter builds a case-insensitive substring SQL predicate for a given
// column and raw user-supplied filter value.
//
// It returns:
//   - sql: a SQL fragment of the form "LOWER(<column>) LIKE ?", ready to be
//     joined with " AND " into a WHERE clause. Empty if the value is empty
//     after trimming, in which case the caller should skip the predicate.
//   - arg: the matching argument with %-wraps and lowercased; nil if sql is "".
//
// All filter inputs are normalized: leading/trailing whitespace is trimmed
// and the value is lowercased. This matches the project rule that filter
// input must be normalized and case-insensitive.
//
// The column argument is interpolated directly into the SQL fragment and so
// MUST be a trusted, hard-coded identifier (e.g. "m.workspace"). It is never
// derived from user input.
func SubstringFilter(column, raw string) (sql string, arg any) {
	normalized := strings.ToLower(strings.TrimSpace(raw))
	if normalized == "" {
		return "", nil
	}
	return "LOWER(" + column + ") LIKE ?", "%" + normalized + "%"
}
