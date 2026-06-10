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
	"github.com/mistakenot/auto-reflect/internal/observations"
	"github.com/mistakenot/auto-reflect/internal/timefilter"
	"github.com/spf13/cobra"
)

func newObservationCmd(application *app.App) *cobra.Command {
	observationCmd := &cobra.Command{
		Use:   "observation",
		Short: "Capture and list working-memory observations (event-sourced)",
	}
	observationCmd.AddCommand(
		newObservationAddCmd(application),
		newObservationListCmd(application),
	)
	return observationCmd
}

func newObservationAddCmd(application *app.App) *cobra.Command {
	var (
		in       observations.Input
		quotes   []string
		messages []string
		domain   []string
		format   string
	)

	cmd := &cobra.Command{
		Use:   "add",
		Short: "Record an observation (appends an observation event)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			outputFormat, err := normalizeFormat(format)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			in.Quotes = quotes
			in.Messages = messages
			in.Domain = domain

			if validationErrs := in.Validate(); len(validationErrs) > 0 {
				writeValidationErrors(cmd.ErrOrStderr(), validationErrs)
				return &ExitError{Code: 1}
			}

			id := observations.NewObservationID()
			payload := in.Payload(id)

			stored, err := events.AppendEvent(application.CWD, events.TypeObservation, payload, events.AppendOptions{})
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			obs, err := observations.Project(stored)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			if outputFormat == "text" {
				printObservationText(cmd, "Recorded observation", &obs)
				return nil
			}
			if err := writeJSON(cmd.OutOrStdout(), map[string]any{"created": true, "scope": "repo", "observation": obs}); err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&in.Kind, "kind", "", "observation kind: correction|pattern|gap|incident")
	cmd.Flags().StringVar(&in.Subject, "subject", "", "what the observation is about")
	cmd.Flags().StringArrayVar(&in.Sessions, "evidence-session", nil, "evidence session id (repeatable; >=1 required)")
	cmd.Flags().StringArrayVar(&quotes, "evidence-quote", nil, "evidence quote, paired by position to --evidence-session (repeatable)")
	cmd.Flags().StringArrayVar(&messages, "evidence-message", nil, "evidence message id, paired by position to --evidence-session (repeatable)")
	cmd.Flags().StringVar(&in.Context, "context", "", "situational context for the observation")
	cmd.Flags().StringVar(&in.SuggestedGeneralization, "suggested-generalization", "", "a candidate rule this observation might generalize to")
	cmd.Flags().StringSliceVar(&domain, "domain", nil, "domain tag(s); repeatable or comma-separated")
	cmd.Flags().StringVar(&in.Severity, "severity", observations.SeverityNormal, "severity: normal|high")
	cmd.Flags().StringVar(&format, "format", "json", "output format: json|text")
	_ = cmd.MarkFlagRequired("kind")
	_ = cmd.MarkFlagRequired("subject")
	return cmd
}

func newObservationListCmd(application *app.App) *cobra.Command {
	var (
		kind           string
		domain         []string
		since          string
		after          string
		before         string
		unconsolidated bool
		limit          int
		format         string
	)

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List observations (newest-first), filtered by kind/domain/time/limit",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			outputFormat, err := normalizeFormat(format)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			if limit < 0 {
				return &ExitError{Code: 1, Err: fmt.Errorf("invalid --limit: use --limit <n> where n >= 0")}
			}

			window, err := timefilter.Parse(time.Now().UTC(), since, after, before)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			kindFilter := strings.ToLower(strings.TrimSpace(kind))
			domainFilter := normalizeFilterTags(domain)

			repo, err := gitutil.DetectRepoLenient(application.CWD)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			all, err := events.ReadAll(repo.Root)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			// consolidated indexes which observation ids a consolidation event
			// already references, so --unconsolidated can drop them. Populated from
			// consolidation events; observations never referenced by one remain
			// unconsolidated.
			consolidated := consolidatedObservationIDs(all)

			matches := make([]observations.Observation, 0)
			for i := range all {
				e := all[i]
				if e.Type != events.TypeObservation {
					continue
				}
				obs, projErr := observations.Project(e)
				if projErr != nil {
					return &ExitError{Code: 1, Err: projErr}
				}
				if kindFilter != "" && obs.Kind != kindFilter {
					continue
				}
				if len(domainFilter) > 0 && !hasAnyDomain(obs.Domain, domainFilter) {
					continue
				}
				if !withinWindow(obs.TS, window) {
					continue
				}
				if unconsolidated {
					if _, done := consolidated[obs.ObservationID]; done {
						continue
					}
				}
				matches = append(matches, obs)
			}

			// Newest-first: descending by ts, then by event id for a stable tie-break.
			sort.SliceStable(matches, func(i, j int) bool {
				if matches[i].TS != matches[j].TS {
					return matches[i].TS > matches[j].TS
				}
				return matches[i].ID > matches[j].ID
			})

			if limit > 0 && len(matches) > limit {
				matches = matches[:limit]
			}

			if outputFormat == "text" {
				for i := range matches {
					printObservationLine(cmd, &matches[i])
				}
				return nil
			}
			if err := writeJSON(cmd.OutOrStdout(), map[string]any{"scope": "repo", "observations": matches}); err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&kind, "kind", "", "filter by kind: correction|pattern|gap|incident")
	cmd.Flags().StringSliceVar(&domain, "domain", nil, "filter by domain tag(s); ANY-of, repeatable or comma-separated")
	cmd.Flags().StringVar(&since, "since", "", "relative window, e.g. 5m, 12h, 7d, 1w")
	cmd.Flags().StringVar(&after, "after", "", "absolute lower bound (YYYY-MM-DD or RFC3339)")
	cmd.Flags().StringVar(&before, "before", "", "absolute upper bound (YYYY-MM-DD or RFC3339)")
	cmd.Flags().BoolVar(&unconsolidated, "unconsolidated", false, "only observations not yet referenced by a consolidation")
	cmd.Flags().IntVar(&limit, "limit", 0, "maximum observations to return (0 means all)")
	cmd.Flags().StringVar(&format, "format", "json", "output format: json|text")
	return cmd
}

// consolidatedObservationIDs returns the set of observation ids already
// referenced by a consolidation event. An observation becomes "consolidated" once
// any consolidation event (from `auto reflect consolidate`) lists its id; the
// --unconsolidated filter uses this to drop already-promoted observations.
func consolidatedObservationIDs(all []events.Event) map[string]struct{} {
	out := map[string]struct{}{}
	for i := range all {
		if all[i].Type != events.TypeConsolidation {
			continue
		}
		var p events.ConsolidationPayload
		if json.Unmarshal(all[i].Payload, &p) != nil {
			continue
		}
		for _, id := range p.ObservationIDs {
			out[id] = struct{}{}
		}
	}
	return out
}

// normalizeFilterTags trims, lowercases, and dedupes filter tags, dropping
// empties. Unlike stored-tag validation, malformed filter values are simply
// non-matching rather than errors.
func normalizeFilterTags(tags []string) []string {
	out := make([]string, 0, len(tags))
	seen := make(map[string]struct{}, len(tags))
	for _, t := range tags {
		n := strings.ToLower(strings.TrimSpace(t))
		if n == "" {
			continue
		}
		if _, ok := seen[n]; ok {
			continue
		}
		seen[n] = struct{}{}
		out = append(out, n)
	}
	return out
}

func hasAnyDomain(have, want []string) bool {
	for _, w := range want {
		for _, h := range have {
			if h == w {
				return true
			}
		}
	}
	return false
}

// withinWindow reports whether an RFC3339 timestamp falls inside the window.
// After is an inclusive lower bound; Before is an exclusive upper bound. An
// unparseable timestamp is treated as out of range.
func withinWindow(ts string, window timefilter.Window) bool {
	if window.After == nil && window.Before == nil {
		return true
	}
	parsed, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return false
	}
	parsed = parsed.UTC()
	if window.After != nil && parsed.Before(*window.After) {
		return false
	}
	if window.Before != nil && !parsed.Before(*window.Before) {
		return false
	}
	return true
}

func printObservationText(cmd *cobra.Command, header string, obs *observations.Observation) {
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "%s %s\n", header, obs.ObservationID)
	fmt.Fprintf(out, "Kind: %s  Severity: %s\n", obs.Kind, obs.Severity)
	if len(obs.Domain) > 0 {
		fmt.Fprintf(out, "Domain: %s\n", strings.Join(obs.Domain, ", "))
	}
	fmt.Fprintf(out, "Subject: %s\n", obs.Subject)
	for _, ev := range obs.Evidence {
		fmt.Fprintf(out, "Evidence: session=%s", ev.SessionID)
		if ev.MessageID != "" {
			fmt.Fprintf(out, " message=%s", ev.MessageID)
		}
		if ev.Quote != "" {
			fmt.Fprintf(out, " quote=%q", ev.Quote)
		}
		fmt.Fprintln(out)
	}
	if obs.Context != "" {
		fmt.Fprintf(out, "Context: %s\n", obs.Context)
	}
	if obs.SuggestedGeneralization != "" {
		fmt.Fprintf(out, "Suggested generalization: %s\n", obs.SuggestedGeneralization)
	}
}

func printObservationLine(cmd *cobra.Command, obs *observations.Observation) {
	fmt.Fprintf(cmd.OutOrStdout(), "%s [%s/%s] (%s) %s\n",
		obs.ObservationID, obs.Kind, obs.Severity, strings.Join(obs.Domain, ","), obs.Subject)
}
