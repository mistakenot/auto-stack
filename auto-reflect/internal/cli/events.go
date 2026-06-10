package cli

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/mistakenot/auto-reflect/internal/app"
	"github.com/mistakenot/auto-reflect/internal/events"
	"github.com/mistakenot/auto-reflect/internal/gitutil"
	"github.com/mistakenot/auto-reflect/internal/timefilter"
	"github.com/spf13/cobra"
)

// knownEventTypes is the set of event types the reader recognises, built from the
// events.Type* constants defined on this branch. The `consolidation` type (1.4)
// is intentionally absent here until it lands; the coordinator adds it to this
// list at integration so `events list --type consolidation` validates.
var knownEventTypes = []string{
	events.TypeRuleCreated,
	events.TypeRuleEdited,
	events.TypeRetrieval,
	events.TypeSelection,
	events.TypeFeedback,
	events.TypeObservation,
}

// eventView is the projected, list-friendly envelope plus a compact one-line
// summary of the payload. The raw payload is intentionally omitted: `events list`
// is a scannable index, not a payload dump.
type eventView struct {
	ID        string `json:"id"`
	Type      string `json:"type"`
	TS        string `json:"ts"`
	Seq       int    `json:"seq"`
	SessionID string `json:"session_id,omitempty"`
	Agent     string `json:"agent,omitempty"`
	Summary   string `json:"summary"`
}

func newEventsCmd(application *app.App) *cobra.Command {
	eventsCmd := &cobra.Command{
		Use:   "events",
		Short: "Read the raw event log (read-only)",
	}
	eventsCmd.AddCommand(newEventsListCmd(application))
	return eventsCmd
}

func newEventsListCmd(application *app.App) *cobra.Command {
	var (
		types   []string
		since   string
		after   string
		before  string
		session string
		limit   int
		format  string
	)

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List raw events (newest-first), filtered by type/time/session/limit",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			outputFormat, err := normalizeFormat(format)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			if limit < 0 {
				return &ExitError{Code: 1, Err: fmt.Errorf("invalid --limit: use --limit <n> where n >= 0")}
			}

			typeFilter, err := parseTypeFilter(types)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			window, err := timefilter.Parse(time.Now().UTC(), since, after, before)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			sessionFilter := strings.TrimSpace(session)

			repo, err := gitutil.DetectRepoLenient(application.CWD)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			all, err := events.ReadAll(repo.Root)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			views := make([]eventView, 0, len(all))
			for i := range all {
				e := all[i]
				if len(typeFilter) > 0 {
					if _, ok := typeFilter[e.Type]; !ok {
						continue
					}
				}
				if sessionFilter != "" && e.SessionID != sessionFilter {
					continue
				}
				if !withinWindow(e.TS, window) {
					continue
				}
				views = append(views, eventView{
					ID:        e.ID,
					Type:      e.Type,
					TS:        e.TS,
					Seq:       e.Seq,
					SessionID: e.SessionID,
					Agent:     e.Agent,
					Summary:   summarizePayload(e),
				})
			}

			// Newest-first: descending by ts, then seq, then id for a stable,
			// deterministic order across shards.
			sort.SliceStable(views, func(i, j int) bool {
				if views[i].TS != views[j].TS {
					return views[i].TS > views[j].TS
				}
				if views[i].Seq != views[j].Seq {
					return views[i].Seq > views[j].Seq
				}
				return views[i].ID > views[j].ID
			})

			if limit > 0 && len(views) > limit {
				views = views[:limit]
			}

			if outputFormat == "text" {
				for i := range views {
					printEventLine(cmd, &views[i])
				}
				return nil
			}
			if err := writeJSON(cmd.OutOrStdout(), map[string]any{"scope": "repo", "events": views}); err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			return nil
		},
	}

	cmd.Flags().StringArrayVar(&types, "type", nil, "filter by event type (repeatable); one of "+strings.Join(knownEventTypes, ", "))
	cmd.Flags().StringVar(&since, "since", "", "relative window, e.g. 5m, 12h, 7d, 1w")
	cmd.Flags().StringVar(&after, "after", "", "absolute lower bound (YYYY-MM-DD or RFC3339)")
	cmd.Flags().StringVar(&before, "before", "", "absolute upper bound (YYYY-MM-DD or RFC3339)")
	cmd.Flags().StringVar(&session, "session", "", "filter by session id")
	cmd.Flags().IntVar(&limit, "limit", 0, "maximum events to return (0 means all)")
	cmd.Flags().StringVar(&format, "format", "json", "output format: json|text")
	return cmd
}

// parseTypeFilter trims and validates the requested --type values against the
// known event types, failing fast on an unknown type with a remediation hint
// listing the valid set. An empty request yields an empty (match-all) filter.
func parseTypeFilter(types []string) (map[string]struct{}, error) {
	filter := make(map[string]struct{}, len(types))
	known := make(map[string]struct{}, len(knownEventTypes))
	for _, t := range knownEventTypes {
		known[t] = struct{}{}
	}
	for _, raw := range types {
		t := strings.ToLower(strings.TrimSpace(raw))
		if t == "" {
			continue
		}
		if _, ok := known[t]; !ok {
			return nil, fmt.Errorf("invalid --type %q: must be one of %s", raw, strings.Join(knownEventTypes, ", "))
		}
		filter[t] = struct{}{}
	}
	return filter, nil
}

// summarizePayload renders a compact, human-scannable one-line description of an
// event's payload, decoding the known payload shapes. An undecodable or unknown
// payload yields an empty summary rather than an error: the reader never fails on
// a single malformed record.
func summarizePayload(e events.Event) string {
	switch e.Type {
	case events.TypeRuleCreated:
		var p events.RuleCreatedPayload
		if decodeEventPayload(e, &p) != nil {
			return ""
		}
		return fmt.Sprintf("rule %s [%s/%s]%s", p.RuleID, p.RuleType, p.Lifecycle, domainSuffix(p.Domain))
	case events.TypeRuleEdited:
		var p events.RuleEditedPayload
		if decodeEventPayload(e, &p) != nil {
			return ""
		}
		return fmt.Sprintf("rule %s v%d->v%d (%d delta(s))", p.RuleID, p.FromVersion, p.ToVersion, len(p.Deltas))
	case events.TypeRetrieval:
		var p events.RetrievalPayload
		if decodeEventPayload(e, &p) != nil {
			return ""
		}
		return fmt.Sprintf("intent=%q matched %d rule(s)%s", p.Intent, len(p.Items), domainSuffix(p.Domain))
	case events.TypeSelection:
		var p events.SelectionPayload
		if decodeEventPayload(e, &p) != nil {
			return ""
		}
		return fmt.Sprintf("selected %d rule(s)", len(p.Items))
	case events.TypeFeedback:
		var p events.FeedbackPayload
		if decodeEventPayload(e, &p) != nil {
			return ""
		}
		return fmt.Sprintf("outcome=%s ranked %d", p.Outcome, len(p.Rankings))
	case events.TypeObservation:
		var p events.ObservationPayload
		if decodeEventPayload(e, &p) != nil {
			return ""
		}
		return fmt.Sprintf("%s/%s %q (%d evidence)", p.Kind, p.Severity, p.Subject, len(p.Evidence))
	default:
		return ""
	}
}

// decodeEventPayload unmarshals an event's raw payload into dst. It is a thin
// wrapper kept local to the cli package so the reader does not reach into loop's
// unexported decode helper.
func decodeEventPayload(e events.Event, dst any) error {
	return json.Unmarshal(e.Payload, dst)
}

func domainSuffix(domain []string) string {
	if len(domain) == 0 {
		return ""
	}
	return " (" + strings.Join(domain, ",") + ")"
}

func printEventLine(cmd *cobra.Command, v *eventView) {
	session := v.SessionID
	if session == "" {
		session = "-"
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s %s %s [%s] %s\n", v.TS, v.Type, v.ID, session, v.Summary)
}
