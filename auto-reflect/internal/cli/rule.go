package cli

import (
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/mistakenot/auto-reflect/internal/app"
	"github.com/mistakenot/auto-reflect/internal/consolidate"
	"github.com/mistakenot/auto-reflect/internal/events"
	"github.com/mistakenot/auto-reflect/internal/gitutil"
	"github.com/mistakenot/auto-reflect/internal/rules"
	"github.com/mistakenot/auto-reflect/internal/store"
	"github.com/spf13/cobra"
)

func newRuleCmd(application *app.App) *cobra.Command {
	ruleCmd := &cobra.Command{
		Use:   "rule",
		Short: "Manage repository rules (event-sourced)",
	}
	ruleCmd.AddCommand(
		newRuleCreateCmd(application),
		newRuleEditCmd(application),
		newRuleListCmd(application),
		newRuleGetCmd(application),
		newRulePromoteCmd(application),
		newRuleRetireCmd(application),
	)
	return ruleCmd
}

func newRuleCreateCmd(application *app.App) *cobra.Command {
	var (
		useWhen    string
		content    string
		causalNote string
		domain     []string
		ruleType   string
		lifecycle  string
		format     string
	)

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a rule (appends a rule_created event and refolds the playbook)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			outputFormat, err := normalizeFormat(format)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			repo, err := gitutil.DetectRepoLenient(application.CWD)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			id := rules.NewRuleID()
			candidate := rules.Rule{
				ID:         id,
				Domain:     rules.NormalizeDomain(domain),
				UseWhen:    strings.TrimSpace(useWhen),
				Content:    strings.TrimSpace(content),
				CausalNote: strings.TrimSpace(causalNote),
				RuleType:   strings.ToLower(strings.TrimSpace(ruleType)),
				Lifecycle:  strings.ToLower(strings.TrimSpace(lifecycle)),
				Version:    1,
			}
			if validationErrs := rules.ValidateRule("", 0, &candidate); len(validationErrs) > 0 {
				writeValidationErrors(cmd.ErrOrStderr(), validationErrs)
				return &ExitError{Code: 1}
			}

			payload := events.RuleCreatedPayload{
				RuleID:     candidate.ID,
				Domain:     candidate.Domain,
				UseWhen:    candidate.UseWhen,
				Content:    candidate.Content,
				CausalNote: candidate.CausalNote,
				RuleType:   candidate.RuleType,
				Lifecycle:  candidate.Lifecycle,
			}
			if _, err := events.AppendEvent(application.CWD, events.TypeRuleCreated, payload, events.AppendOptions{}); err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			created, err := refoldAndGet(repo.Root, candidate.ID)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			if outputFormat == "text" {
				printRuleText(cmd, "Created rule", &created)
				return nil
			}
			if err := writeJSON(cmd.OutOrStdout(), map[string]any{"created": true, "scope": "repo", "rule": created}); err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&useWhen, "use-when", "", "the situation in which the rule applies")
	cmd.Flags().StringVar(&content, "content", "", "the rule guidance itself")
	cmd.Flags().StringVar(&causalNote, "causal-note", "", "why this rule exists (the failure it prevents)")
	cmd.Flags().StringSliceVar(&domain, "domain", nil, "domain tag(s); repeatable or comma-separated")
	cmd.Flags().StringVar(&ruleType, "type", "soft", "rule type: hard|soft")
	cmd.Flags().StringVar(&lifecycle, "lifecycle", "draft", "lifecycle: draft|confirmed|stale")
	cmd.Flags().StringVar(&format, "format", "json", "output format: json|text")
	_ = cmd.MarkFlagRequired("use-when")
	_ = cmd.MarkFlagRequired("content")
	_ = cmd.MarkFlagRequired("causal-note")
	return cmd
}

func newRuleEditCmd(application *app.App) *cobra.Command {
	var (
		useWhen    string
		content    string
		causalNote string
		domain     []string
		ruleType   string
		lifecycle  string
		format     string
	)

	cmd := &cobra.Command{
		Use:   "edit <r-id>",
		Short: "Edit a rule (appends ONE rule_edited event with all changed fields, one version bump)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			outputFormat, err := normalizeFormat(format)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			repo, err := gitutil.DetectRepoLenient(application.CWD)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			// Refold first so we edit against the current folded state.
			playbook, err := rules.Load(repo.Root, store.PlaybookPath(repo.Root))
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			current, ok := findRule(playbook, args[0])
			if !ok {
				return &ExitError{Code: 1, Err: fmt.Errorf("rule %q not found: run `auto reflect rule list` to see available ids", args[0])}
			}

			updated := current
			var deltas []events.FieldDelta
			flags := cmd.Flags()
			if flags.Changed("use-when") {
				newVal := strings.TrimSpace(useWhen)
				if newVal != current.UseWhen {
					deltas = append(deltas, events.FieldDelta{Field: rules.FieldUseWhen, Old: current.UseWhen, New: newVal})
					updated.UseWhen = newVal
				}
			}
			if flags.Changed("content") {
				newVal := strings.TrimSpace(content)
				if newVal != current.Content {
					deltas = append(deltas, events.FieldDelta{Field: rules.FieldContent, Old: current.Content, New: newVal})
					updated.Content = newVal
				}
			}
			if flags.Changed("causal-note") {
				newVal := strings.TrimSpace(causalNote)
				if newVal != current.CausalNote {
					deltas = append(deltas, events.FieldDelta{Field: rules.FieldCausalNote, Old: current.CausalNote, New: newVal})
					updated.CausalNote = newVal
				}
			}
			if flags.Changed("domain") {
				newVal := rules.NormalizeDomain(domain)
				if !reflect.DeepEqual(newVal, current.Domain) {
					deltas = append(deltas, events.FieldDelta{Field: rules.FieldDomain, Old: current.Domain, New: newVal})
					updated.Domain = newVal
				}
			}
			if flags.Changed("type") {
				newVal := strings.ToLower(strings.TrimSpace(ruleType))
				if newVal != current.RuleType {
					deltas = append(deltas, events.FieldDelta{Field: rules.FieldRuleType, Old: current.RuleType, New: newVal})
					updated.RuleType = newVal
				}
			}
			if flags.Changed("lifecycle") {
				newVal := strings.ToLower(strings.TrimSpace(lifecycle))
				if newVal != current.Lifecycle {
					deltas = append(deltas, events.FieldDelta{Field: rules.FieldLifecycle, Old: current.Lifecycle, New: newVal})
					updated.Lifecycle = newVal
				}
			}

			if len(deltas) == 0 {
				return &ExitError{Code: 1, Err: errors.New("no changes: pass at least one of --use-when --content --causal-note --domain --type --lifecycle with a new value")}
			}

			// Validate the post-edit rule so an edit can't violate invariants.
			if validationErrs := rules.ValidateRule("", 0, &updated); len(validationErrs) > 0 {
				writeValidationErrors(cmd.ErrOrStderr(), validationErrs)
				return &ExitError{Code: 1}
			}

			payload := events.RuleEditedPayload{
				RuleID:      current.ID,
				FromVersion: current.Version,
				ToVersion:   current.Version + 1,
				Deltas:      deltas,
			}
			if _, err := events.AppendEvent(application.CWD, events.TypeRuleEdited, payload, events.AppendOptions{}); err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			edited, err := refoldAndGet(repo.Root, current.ID)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			if outputFormat == "text" {
				printRuleText(cmd, "Edited rule", &edited)
				return nil
			}
			if err := writeJSON(cmd.OutOrStdout(), map[string]any{"edited": true, "scope": "repo", "rule": edited}); err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&useWhen, "use-when", "", "new use_when value")
	cmd.Flags().StringVar(&content, "content", "", "new content value")
	cmd.Flags().StringVar(&causalNote, "causal-note", "", "new causal_note value")
	cmd.Flags().StringSliceVar(&domain, "domain", nil, "new domain tag list (replaces existing)")
	cmd.Flags().StringVar(&ruleType, "type", "", "new rule type: hard|soft")
	cmd.Flags().StringVar(&lifecycle, "lifecycle", "", "new lifecycle: draft|confirmed|stale")
	cmd.Flags().StringVar(&format, "format", "json", "output format: json|text")
	return cmd
}

func newRuleListCmd(application *app.App) *cobra.Command {
	var (
		format    string
		lifecycle string
	)
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List rules (id, use_when, domain, type, lifecycle)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			outputFormat, err := normalizeFormat(format)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			lifecycleFilter := strings.ToLower(strings.TrimSpace(lifecycle))
			switch lifecycleFilter {
			case "", rules.LifecycleDraft, rules.LifecycleConfirmed, rules.LifecycleStale:
				// ok: empty means no filter (list-returns-all).
			default:
				return &ExitError{Code: 1, Err: fmt.Errorf("invalid --lifecycle %q: use one of %s|%s|%s", lifecycle, rules.LifecycleDraft, rules.LifecycleConfirmed, rules.LifecycleStale)}
			}
			repo, err := gitutil.DetectRepoLenient(application.CWD)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			playbook, err := rules.Load(repo.Root, store.PlaybookPath(repo.Root))
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			selected := make([]*rules.Rule, 0, len(playbook.Rules))
			for i := range playbook.Rules {
				if lifecycleFilter != "" && playbook.Rules[i].Lifecycle != lifecycleFilter {
					continue
				}
				selected = append(selected, &playbook.Rules[i])
			}

			if outputFormat == "text" {
				for _, r := range selected {
					line := fmt.Sprintf("%s [%s] (%s) %s  lifecycle=%s", r.ID, r.RuleType, strings.Join(r.Domain, ","), r.UseWhen, r.Lifecycle)
					if len(r.ObservationIDs) > 0 {
						line += fmt.Sprintf("  obs=%d", len(r.ObservationIDs))
					}
					fmt.Fprintln(cmd.OutOrStdout(), line)
				}
				return nil
			}

			items := make([]map[string]any, 0, len(selected))
			for _, r := range selected {
				items = append(items, map[string]any{
					"id":              r.ID,
					"use_when":        r.UseWhen,
					"domain":          r.Domain,
					"rule_type":       r.RuleType,
					"lifecycle":       r.Lifecycle,
					"observation_ids": r.ObservationIDs,
				})
			}
			if err := writeJSON(cmd.OutOrStdout(), map[string]any{"scope": "repo", "rules": items}); err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&lifecycle, "lifecycle", "", "filter by lifecycle: draft|confirmed|stale (default: all)")
	cmd.Flags().StringVar(&format, "format", "json", "output format: json|text")
	return cmd
}

func newRuleGetCmd(application *app.App) *cobra.Command {
	var format string
	cmd := &cobra.Command{
		Use:   "get <r-id>",
		Short: "Get the full rule by id",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			outputFormat, err := normalizeFormat(format)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			repo, err := gitutil.DetectRepoLenient(application.CWD)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			playbook, err := rules.Load(repo.Root, store.PlaybookPath(repo.Root))
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			rule, ok := findRule(playbook, args[0])
			if !ok {
				return &ExitError{Code: 1, Err: fmt.Errorf("rule %q not found: run `auto reflect rule list` to see available ids", args[0])}
			}

			if outputFormat == "text" {
				printRuleText(cmd, "Rule", &rule)
				return nil
			}
			if err := writeJSON(cmd.OutOrStdout(), rule); err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&format, "format", "json", "output format: json|text")
	return cmd
}

func newRulePromoteCmd(application *app.App) *cobra.Command {
	var (
		force  bool
		format string
	)
	cmd := &cobra.Command{
		Use:   "promote <r-id>",
		Short: "Promote a draft rule to confirmed (gated on >=2 distinct evidence sessions)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			outputFormat, err := normalizeFormat(format)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			repo, err := gitutil.DetectRepoLenient(application.CWD)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			playbook, err := rules.Load(repo.Root, store.PlaybookPath(repo.Root))
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			current, ok := findRule(playbook, args[0])
			if !ok {
				return &ExitError{Code: 1, Err: fmt.Errorf("rule %q not found: run `auto reflect rule list` to see available ids", args[0])}
			}
			if current.Lifecycle == rules.LifecycleConfirmed {
				return &ExitError{Code: 1, Err: fmt.Errorf("rule %s is already confirmed", current.ID)}
			}

			if !force {
				all, rerr := events.ReadAll(repo.Root)
				if rerr != nil {
					return &ExitError{Code: 1, Err: rerr}
				}
				cov := consolidate.NewObservationIndex(all).Coverage(current.ObservationIDs)
				if len(cov.Sessions) < consolidate.EvidenceMinSessions {
					return &ExitError{Code: 1, Err: fmt.Errorf("rule %s provenance covers %d distinct session(s); promotion needs >=%d (attach more evidence with `auto reflect consolidate`, or pass --force)", current.ID, len(cov.Sessions), consolidate.EvidenceMinSessions)}
				}
			}

			updated, err := changeLifecycle(application.CWD, repo.Root, &current, rules.LifecycleConfirmed)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			return writeRuleResult(cmd, outputFormat, "Promoted rule", "promoted", &updated)
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "promote even when provenance covers fewer than two distinct sessions")
	cmd.Flags().StringVar(&format, "format", "json", "output format: json|text")
	return cmd
}

func newRuleRetireCmd(application *app.App) *cobra.Command {
	var format string
	cmd := &cobra.Command{
		Use:   "retire <r-id>",
		Short: "Retire a rule to stale (always allowed; stale rules never surface in retrieve)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			outputFormat, err := normalizeFormat(format)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			repo, err := gitutil.DetectRepoLenient(application.CWD)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			playbook, err := rules.Load(repo.Root, store.PlaybookPath(repo.Root))
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			current, ok := findRule(playbook, args[0])
			if !ok {
				return &ExitError{Code: 1, Err: fmt.Errorf("rule %q not found: run `auto reflect rule list` to see available ids", args[0])}
			}
			if current.Lifecycle == rules.LifecycleStale {
				return &ExitError{Code: 1, Err: fmt.Errorf("rule %s is already stale", current.ID)}
			}
			updated, err := changeLifecycle(application.CWD, repo.Root, &current, rules.LifecycleStale)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			return writeRuleResult(cmd, outputFormat, "Retired rule", "retired", &updated)
		},
	}
	cmd.Flags().StringVar(&format, "format", "json", "output format: json|text")
	return cmd
}

// changeLifecycle appends one rule_edited event carrying a single lifecycle delta
// (so promote/retire are one versioned event, reusing the edit/fold path) and
// returns the refolded rule.
func changeLifecycle(cwd, repoRoot string, current *rules.Rule, lifecycle string) (rules.Rule, error) {
	payload := events.RuleEditedPayload{
		RuleID:      current.ID,
		FromVersion: current.Version,
		ToVersion:   current.Version + 1,
		Deltas:      []events.FieldDelta{{Field: rules.FieldLifecycle, Old: current.Lifecycle, New: lifecycle}},
	}
	if _, err := events.AppendEvent(cwd, events.TypeRuleEdited, payload, events.AppendOptions{}); err != nil {
		return rules.Rule{}, err
	}
	return refoldAndGet(repoRoot, current.ID)
}

// writeRuleResult emits a mutated rule in the chosen format, mirroring the
// create/edit response envelope.
func writeRuleResult(cmd *cobra.Command, outputFormat, textHeader, jsonVerb string, r *rules.Rule) error {
	if outputFormat == "text" {
		printRuleText(cmd, textHeader, r)
		return nil
	}
	if err := writeJSON(cmd.OutOrStdout(), map[string]any{jsonVerb: true, "scope": "repo", "rule": r}); err != nil {
		return &ExitError{Code: 1, Err: err}
	}
	return nil
}

func findRule(playbook rules.Playbook, id string) (rules.Rule, bool) {
	for i := range playbook.Rules {
		if playbook.Rules[i].ID == id {
			return playbook.Rules[i], true
		}
	}
	return rules.Rule{}, false
}

func refoldAndGet(repoRoot, id string) (rules.Rule, error) {
	playbook, _, err := rules.Rebuild(repoRoot, store.PlaybookPath(repoRoot))
	if err != nil {
		return rules.Rule{}, err
	}
	rule, ok := findRule(playbook, id)
	if !ok {
		return rules.Rule{}, fmt.Errorf("rule %q not found after refold", id)
	}
	return rule, nil
}

func printRuleText(cmd *cobra.Command, header string, r *rules.Rule) {
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "%s %s (v%d)\n", header, r.ID, r.Version)
	fmt.Fprintf(out, "Type: %s  Lifecycle: %s\n", r.RuleType, r.Lifecycle)
	if len(r.Domain) > 0 {
		fmt.Fprintf(out, "Domain: %s\n", strings.Join(r.Domain, ", "))
	}
	fmt.Fprintf(out, "Use when: %s\n", r.UseWhen)
	fmt.Fprintf(out, "Content: %s\n", r.Content)
	fmt.Fprintf(out, "Causal note: %s\n", r.CausalNote)
	if len(r.ObservationIDs) > 0 {
		fmt.Fprintf(out, "Observations: %s\n", strings.Join(r.ObservationIDs, ", "))
	}
}
