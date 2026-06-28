package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/mistakenot/auto-reflect/internal/app"
	"github.com/mistakenot/auto-reflect/internal/consolidate"
	"github.com/mistakenot/auto-reflect/internal/events"
	"github.com/mistakenot/auto-reflect/internal/gitutil"
	"github.com/mistakenot/auto-reflect/internal/rules"
	"github.com/mistakenot/auto-reflect/internal/store"
	"github.com/spf13/cobra"
)

// applied is one delta that was (or, under --dry-run, would be) committed.
type applied struct {
	Op             string      `json:"op"`
	RuleID         string      `json:"rule_id"`
	ObservationIDs []string    `json:"observation_ids,omitempty"`
	Note           string      `json:"note,omitempty"`
	Rule           *rules.Rule `json:"rule,omitempty"`
}

// skipped is one delta a gate refused, with the reason a human/agent can act on.
type skipped struct {
	Delta  consolidate.Delta `json:"delta"`
	Reason string            `json:"reason"`
}

func newConsolidateCmd(application *app.App) *cobra.Command {
	var (
		force  bool
		dryRun bool
		format string
	)

	cmd := &cobra.Command{
		Use:   "consolidate <json|->",
		Short: "Turn clustered observations into playbook changes (gated, deterministic)",
		Long: "Apply a consolidation delta document (positional JSON or '-' for stdin). " +
			"The CLI validates, gates create-draft deltas on >=2 distinct evidence sessions, " +
			"dedupes against live rules, flags possible conflicts, and persists rule + " +
			"consolidation events. Use --dry-run to preview without writing.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			outputFormat, err := normalizeFormat(format)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}

			raw, err := readConsolidateInput(cmd, args[0])
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			doc, err := consolidate.ParseDocument(raw)
			if err != nil {
				var de *consolidate.DocumentError
				if errors.As(err, &de) {
					writeValidationErrors(cmd.ErrOrStderr(), de.Errors)
					return &ExitError{Code: 1}
				}
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
			all, err := events.ReadAll(repo.Root)
			if err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			obIndex := consolidate.NewObservationIndex(all)

			proc := &consolidator{
				cwd:      application.CWD,
				force:    force,
				dryRun:   dryRun,
				rules:    playbook.Rules,
				obIndex:  obIndex,
				touched:  map[string]struct{}{},
				appliedT: []applied{},
				skippedT: []skipped{},
			}
			for i := range doc.Deltas {
				proc.process(&doc.Deltas[i])
			}

			// One refold after all writes so applied rules reflect the new state.
			if !dryRun && proc.wrote {
				refolded, _, rerr := rules.Rebuild(repo.Root, store.PlaybookPath(repo.Root))
				if rerr != nil {
					return &ExitError{Code: 1, Err: rerr}
				}
				proc.attachFoldedRules(refolded)
			}

			result := map[string]any{
				"applied":   proc.appliedT,
				"skipped":   proc.skippedT,
				"conflicts": proc.conflicts,
				"dry_run":   dryRun,
			}
			if outputFormat == "text" {
				printConsolidateText(cmd, proc, dryRun)
				return nil
			}
			if err := writeJSON(cmd.OutOrStdout(), result); err != nil {
				return &ExitError{Code: 1, Err: err}
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "bypass the >=2-distinct-session evidence threshold on create-draft")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "compute and report gates/conflicts without writing any events")
	cmd.Flags().StringVar(&format, "format", "json", "output format: json|text")
	return cmd
}

// consolidator carries the per-invocation state threaded across deltas: the
// start-of-batch rule set the gates check against, the observation index, the set
// of rules already mutated this batch (to keep version bumps deterministic), and
// the accumulating output.
type consolidator struct {
	cwd     string
	force   bool
	dryRun  bool
	rules   []rules.Rule
	obIndex consolidate.ObservationIndex

	touched   map[string]struct{} // rule ids already mutated this batch
	wrote     bool
	appliedT  []applied
	skippedT  []skipped
	conflicts []consolidate.Conflict
}

func (c *consolidator) skip(d *consolidate.Delta, reason string) {
	c.skippedT = append(c.skippedT, skipped{Delta: *d, Reason: reason})
}

func (c *consolidator) findRule(id string) (rules.Rule, bool) {
	for i := range c.rules {
		if c.rules[i].ID == id {
			return c.rules[i], true
		}
	}
	return rules.Rule{}, false
}

func (c *consolidator) process(d *consolidate.Delta) {
	switch strings.TrimSpace(d.Op) {
	case consolidate.OpCreateDraft:
		c.createDraft(d)
	case consolidate.OpAttachEvidence:
		c.attachEvidence(d)
	case consolidate.OpMerge:
		c.merge(d)
	case consolidate.OpDeprecate:
		c.deprecate(d)
	case consolidate.OpSplit:
		c.split(d)
	case "":
		c.skip(d, "missing op: each delta needs op (create-draft|attach-evidence|merge|deprecate)")
	default:
		c.skip(d, fmt.Sprintf("unknown op %q: use one of create-draft, attach-evidence, merge, deprecate", d.Op))
	}
}

func (c *consolidator) createDraft(d *consolidate.Delta) {
	cov := c.obIndex.Coverage(d.ObservationIDs)
	if ok, reason := consolidate.EvidenceGate(cov, c.force); !ok {
		c.skip(d, reason)
		return
	}

	ruleType := strings.ToLower(strings.TrimSpace(d.Type))
	if ruleType == "" {
		ruleType = rules.RuleTypeSoft
	}
	domain := rules.NormalizeDomain(d.Domain)
	useWhen := strings.TrimSpace(d.UseWhen)

	if dup, isDup := consolidate.DetectDuplicate(c.rules, useWhen, domain); isDup {
		c.skip(d, fmt.Sprintf("duplicates existing rule %s (score %.2f); use attach-evidence to add provenance instead of a new draft", dup.RuleID, dup.Score))
		return
	}
	c.conflicts = append(c.conflicts, consolidate.DetectConflicts(c.rules, domain, strings.TrimSpace(d.Content))...)

	prov := consolidate.UnionObservationIDs(nil, d.ObservationIDs)
	candidate := rules.Rule{
		ID:             rules.NewRuleID(),
		Domain:         domain,
		UseWhen:        useWhen,
		Content:        strings.TrimSpace(d.Content),
		CausalNote:     strings.TrimSpace(d.CausalNote),
		RuleType:       ruleType,
		Lifecycle:      rules.LifecycleDraft,
		Version:        1,
		ObservationIDs: prov,
	}
	if errs := rules.ValidateRule("", 0, &candidate); len(errs) > 0 {
		c.skip(d, "invalid draft rule: "+joinValidation(errs))
		return
	}

	entry := applied{Op: consolidate.OpCreateDraft, RuleID: candidate.ID, ObservationIDs: prov, Note: "draft created"}
	if c.dryRun {
		ruleCopy := candidate
		entry.Rule = &ruleCopy
		c.appliedT = append(c.appliedT, entry)
		return
	}

	createdPayload := events.RuleCreatedPayload{
		RuleID:         candidate.ID,
		Domain:         candidate.Domain,
		UseWhen:        candidate.UseWhen,
		Content:        candidate.Content,
		CausalNote:     candidate.CausalNote,
		RuleType:       candidate.RuleType,
		Lifecycle:      candidate.Lifecycle,
		ObservationIDs: prov,
	}
	if _, err := events.AppendEvent(c.cwd, events.TypeRuleCreated, createdPayload, events.AppendOptions{}); err != nil {
		c.skip(d, "write rule_created failed: "+err.Error())
		return
	}
	if err := c.appendConsolidation(candidate.ID, prov, consolidate.OpCreateDraft); err != nil {
		c.skip(d, "write consolidation failed: "+err.Error())
		return
	}
	c.wrote = true
	c.touched[candidate.ID] = struct{}{}
	c.appliedT = append(c.appliedT, entry)
}

func (c *consolidator) attachEvidence(d *consolidate.Delta) {
	if strings.TrimSpace(d.RuleID) == "" {
		c.skip(d, "attach-evidence requires rule_id")
		return
	}
	if len(d.ObservationIDs) == 0 {
		c.skip(d, "attach-evidence requires observation_ids")
		return
	}
	current, ok := c.findRule(d.RuleID)
	if !ok {
		c.skip(d, fmt.Sprintf("rule %q not found: run `auto reflect rule list`", d.RuleID))
		return
	}
	if _, dup := c.touched[d.RuleID]; dup {
		c.skip(d, fmt.Sprintf("rule %s was already modified earlier in this document; submit as a separate consolidate", d.RuleID))
		return
	}
	cov := c.obIndex.Coverage(d.ObservationIDs)
	if len(cov.Missing) > 0 {
		c.skip(d, "references unknown observation(s): "+strings.Join(cov.Missing, ", "))
		return
	}
	newProv := consolidate.UnionObservationIDs(current.ObservationIDs, d.ObservationIDs)
	if len(newProv) == len(current.ObservationIDs) {
		c.skip(d, "no new evidence: all observation_ids already attached to this rule")
		return
	}

	entry := applied{Op: consolidate.OpAttachEvidence, RuleID: current.ID, ObservationIDs: newProv, Note: "evidence attached"}
	if c.dryRun {
		c.appliedT = append(c.appliedT, entry)
		return
	}
	deltas := []events.FieldDelta{{Field: rules.FieldObservationIDs, Old: current.ObservationIDs, New: newProv}}
	if err := c.appendRuleEdited(&current, deltas); err != nil {
		c.skip(d, "write rule_edited failed: "+err.Error())
		return
	}
	if err := c.appendConsolidation(current.ID, consolidate.UnionObservationIDs(nil, d.ObservationIDs), consolidate.OpAttachEvidence); err != nil {
		c.skip(d, "write consolidation failed: "+err.Error())
		return
	}
	c.wrote = true
	c.touched[current.ID] = struct{}{}
	c.appliedT = append(c.appliedT, entry)
}

func (c *consolidator) merge(d *consolidate.Delta) {
	if len(d.RuleIDs) < 2 {
		c.skip(d, "merge requires at least two rule_ids")
		return
	}
	if strings.TrimSpace(d.IntoUseWhen) == "" {
		c.skip(d, "merge requires into_use_when (the combined use_when)")
		return
	}
	resolved := make([]rules.Rule, 0, len(d.RuleIDs))
	inMerge := map[string]struct{}{}
	for _, id := range d.RuleIDs {
		r, ok := c.findRule(id)
		if !ok {
			c.skip(d, fmt.Sprintf("rule %q not found: run `auto reflect rule list`", id))
			return
		}
		if _, dup := c.touched[id]; dup {
			c.skip(d, fmt.Sprintf("rule %s was already modified earlier in this document; submit as a separate consolidate", id))
			return
		}
		resolved = append(resolved, r)
		inMerge[id] = struct{}{}
	}

	survivor := resolved[0]
	others := resolved[1:]
	newUseWhen := strings.TrimSpace(d.IntoUseWhen)

	// Validate any extra observation_ids against the index, mirroring attach-evidence,
	// so a typo'd/fabricated id can't silently bloat the survivor's provenance.
	if cov := c.obIndex.Coverage(d.ObservationIDs); len(cov.Missing) > 0 {
		c.skip(d, "references unknown observation(s): "+strings.Join(cov.Missing, ", "))
		return
	}

	// Provenance unions the survivor, every retired rule, and the delta's ids, so no
	// evidence is stranded on a rule merge retires to stale (which would also understate
	// the survivor's session coverage at promotion time).
	newProv := consolidate.UnionObservationIDs(survivor.ObservationIDs, d.ObservationIDs)
	for i := range others {
		newProv = consolidate.UnionObservationIDs(newProv, others[i].ObservationIDs)
	}

	// Dedupe gate: refuse if the combined use_when strongly matches a live rule OUTSIDE
	// the merge set, mirroring create-draft. The merged rules themselves are excluded so
	// they can't trivially match against their own (soon-to-be-retired) predicates.
	candidates := make([]rules.Rule, 0, len(c.rules))
	for i := range c.rules {
		if _, inSet := inMerge[c.rules[i].ID]; inSet {
			continue
		}
		candidates = append(candidates, c.rules[i])
	}
	if dup, isDup := consolidate.DetectDuplicate(candidates, newUseWhen, survivor.Domain); isDup {
		c.skip(d, fmt.Sprintf("merged use_when duplicates existing rule %s (score %.2f); use attach-evidence or a more specific into_use_when", dup.RuleID, dup.Score))
		return
	}

	// Conflicts: existing live rules outside the merge set that oppose the survivor.
	for _, cf := range consolidate.DetectConflicts(c.rules, survivor.Domain, survivor.Content) {
		if _, isMerged := inMerge[cf.RuleID]; !isMerged {
			c.conflicts = append(c.conflicts, cf)
		}
	}

	retired := make([]string, 0, len(others))
	for i := range others {
		retired = append(retired, others[i].ID)
	}
	entry := applied{Op: consolidate.OpMerge, RuleID: survivor.ID, ObservationIDs: newProv, Note: "merged; retired " + strings.Join(retired, ", ")}
	if c.dryRun {
		c.appliedT = append(c.appliedT, entry)
		return
	}

	var deltas []events.FieldDelta
	if newUseWhen != survivor.UseWhen {
		deltas = append(deltas, events.FieldDelta{Field: rules.FieldUseWhen, Old: survivor.UseWhen, New: newUseWhen})
	}
	if len(newProv) != len(survivor.ObservationIDs) {
		deltas = append(deltas, events.FieldDelta{Field: rules.FieldObservationIDs, Old: survivor.ObservationIDs, New: newProv})
	}
	if len(deltas) > 0 {
		if err := c.appendRuleEdited(&survivor, deltas); err != nil {
			c.skip(d, "write rule_edited (survivor) failed: "+err.Error())
			return
		}
	}
	for i := range others {
		o := &others[i]
		if o.Lifecycle == rules.LifecycleStale {
			continue
		}
		ld := []events.FieldDelta{{Field: rules.FieldLifecycle, Old: o.Lifecycle, New: rules.LifecycleStale}}
		if err := c.appendRuleEdited(o, ld); err != nil {
			c.skip(d, "write rule_edited (retire) failed: "+err.Error())
			return
		}
		c.touched[o.ID] = struct{}{}
	}
	if err := c.appendConsolidation(survivor.ID, consolidate.UnionObservationIDs(nil, d.ObservationIDs), consolidate.OpMerge); err != nil {
		c.skip(d, "write consolidation failed: "+err.Error())
		return
	}
	c.wrote = true
	c.touched[survivor.ID] = struct{}{}
	c.appliedT = append(c.appliedT, entry)
}

func (c *consolidator) deprecate(d *consolidate.Delta) {
	if strings.TrimSpace(d.RuleID) == "" {
		c.skip(d, "deprecate requires rule_id")
		return
	}
	current, ok := c.findRule(d.RuleID)
	if !ok {
		c.skip(d, fmt.Sprintf("rule %q not found: run `auto reflect rule list`", d.RuleID))
		return
	}
	if current.Lifecycle == rules.LifecycleStale {
		c.skip(d, fmt.Sprintf("rule %s is already stale", d.RuleID))
		return
	}
	if _, dup := c.touched[d.RuleID]; dup {
		c.skip(d, fmt.Sprintf("rule %s was already modified earlier in this document; submit as a separate consolidate", d.RuleID))
		return
	}
	note := "deprecated -> stale"
	if r := strings.TrimSpace(d.Reason); r != "" {
		note += " (" + r + ")"
	}
	entry := applied{Op: consolidate.OpDeprecate, RuleID: current.ID, Note: note}
	if c.dryRun {
		c.appliedT = append(c.appliedT, entry)
		return
	}
	deltas := []events.FieldDelta{{Field: rules.FieldLifecycle, Old: current.Lifecycle, New: rules.LifecycleStale}}
	if err := c.appendRuleEdited(&current, deltas); err != nil {
		c.skip(d, "write rule_edited failed: "+err.Error())
		return
	}
	c.wrote = true
	c.touched[current.ID] = struct{}{}
	c.appliedT = append(c.appliedT, entry)
}

// split retires one rule into >=2 narrower drafts, wiring lineage both ways: the
// parent folds to stale with successor_ids pointing at the children, and each
// child is a draft carrying predecessor_ids=[parent] and the parent's inherited
// provenance. Mirrors merge(): all gate/dedupe failures are exit-0 skips.
func (c *consolidator) split(d *consolidate.Delta) {
	if strings.TrimSpace(d.RuleID) == "" {
		c.skip(d, "split requires rule_id")
		return
	}
	if len(d.Into) < 2 {
		c.skip(d, "split requires at least two children in `into`")
		return
	}
	current, ok := c.findRule(d.RuleID)
	if !ok {
		c.skip(d, fmt.Sprintf("rule %q not found: run `auto reflect rule list`", d.RuleID))
		return
	}
	if _, dup := c.touched[d.RuleID]; dup {
		c.skip(d, fmt.Sprintf("rule %s was already modified earlier in this document; submit as a separate consolidate", d.RuleID))
		return
	}
	if current.Lifecycle == rules.LifecycleStale {
		c.skip(d, fmt.Sprintf("rule %s is already stale", d.RuleID))
		return
	}

	// Children inherit the parent's provenance, so no evidence is stranded when the
	// parent retires to stale.
	inheritedObs := consolidate.UnionObservationIDs(nil, current.ObservationIDs)

	// Mint each child up front so successor_ids on the parent edit is correct, then
	// validate it like a create-draft.
	children := make([]rules.Rule, 0, len(d.Into))
	childIDs := make([]string, 0, len(d.Into))
	for i := range d.Into {
		spec := &d.Into[i]
		ruleType := strings.ToLower(strings.TrimSpace(spec.Type))
		if ruleType == "" {
			ruleType = rules.RuleTypeSoft
		}
		child := rules.Rule{
			ID:             rules.NewRuleID(),
			Domain:         rules.NormalizeDomain(spec.Domain),
			UseWhen:        strings.TrimSpace(spec.UseWhen),
			Content:        strings.TrimSpace(spec.Content),
			CausalNote:     strings.TrimSpace(spec.CausalNote),
			RuleType:       ruleType,
			Lifecycle:      rules.LifecycleDraft,
			Version:        1,
			ObservationIDs: inheritedObs,
			PredecessorIDs: []string{current.ID},
		}
		if errs := rules.ValidateRule("", 0, &child); len(errs) > 0 {
			c.skip(d, fmt.Sprintf("invalid split child %d: %s", i, joinValidation(errs)))
			return
		}
		children = append(children, child)
		childIDs = append(childIDs, child.ID)
	}

	// Dedupe gate: each child's use_when must not strongly match a live rule OUTSIDE
	// the parent (mirrors merge's candidate exclusion so the soon-to-be-stale parent
	// can't trivially match its children).
	candidates := make([]rules.Rule, 0, len(c.rules))
	for i := range c.rules {
		if c.rules[i].ID == current.ID {
			continue
		}
		candidates = append(candidates, c.rules[i])
	}
	for i := range children {
		if dup, isDup := consolidate.DetectDuplicate(candidates, children[i].UseWhen, children[i].Domain); isDup {
			c.skip(d, fmt.Sprintf("split child %d duplicates existing rule %s (score %.2f); narrow the use_when or use attach-evidence", i, dup.RuleID, dup.Score))
			return
		}
	}

	entry := applied{Op: consolidate.OpSplit, RuleID: current.ID, ObservationIDs: inheritedObs, Note: "split into " + strings.Join(childIDs, ", ")}
	if c.dryRun {
		c.appliedT = append(c.appliedT, entry)
		return
	}

	// Parent: one rule_edited folding it to stale and recording the forward lineage.
	parentDeltas := []events.FieldDelta{
		{Field: rules.FieldLifecycle, Old: current.Lifecycle, New: rules.LifecycleStale},
		{Field: rules.FieldSuccessorIDs, Old: current.SuccessorIDs, New: childIDs},
	}
	if err := c.appendRuleEdited(&current, parentDeltas); err != nil {
		c.skip(d, "write rule_edited (parent) failed: "+err.Error())
		return
	}
	for i := range children {
		child := &children[i]
		createdPayload := events.RuleCreatedPayload{
			RuleID:         child.ID,
			Domain:         child.Domain,
			UseWhen:        child.UseWhen,
			Content:        child.Content,
			CausalNote:     child.CausalNote,
			RuleType:       child.RuleType,
			Lifecycle:      child.Lifecycle,
			ObservationIDs: inheritedObs,
			PredecessorIDs: []string{current.ID},
		}
		if _, err := events.AppendEvent(c.cwd, events.TypeRuleCreated, createdPayload, events.AppendOptions{}); err != nil {
			c.skip(d, "write rule_created (child) failed: "+err.Error())
			return
		}
		if err := c.appendConsolidation(child.ID, inheritedObs, consolidate.OpSplit); err != nil {
			c.skip(d, "write consolidation failed: "+err.Error())
			return
		}
		c.touched[child.ID] = struct{}{}
	}
	c.wrote = true
	c.touched[current.ID] = struct{}{}
	c.appliedT = append(c.appliedT, entry)
}

func (c *consolidator) appendRuleEdited(current *rules.Rule, deltas []events.FieldDelta) error {
	payload := events.RuleEditedPayload{
		RuleID:      current.ID,
		FromVersion: current.Version,
		ToVersion:   current.Version + 1,
		Deltas:      deltas,
	}
	_, err := events.AppendEvent(c.cwd, events.TypeRuleEdited, payload, events.AppendOptions{})
	return err
}

func (c *consolidator) appendConsolidation(ruleID string, obIDs []string, op string) error {
	payload := events.ConsolidationPayload{RuleID: ruleID, ObservationIDs: obIDs, Op: op}
	_, err := events.AppendEvent(c.cwd, events.TypeConsolidation, payload, events.AppendOptions{})
	return err
}

// attachFoldedRules fills each applied entry's Rule from the refolded playbook so
// the output shows the persisted (versioned, provenance-bearing) rule.
func (c *consolidator) attachFoldedRules(pb rules.Playbook) {
	byID := make(map[string]rules.Rule, len(pb.Rules))
	for i := range pb.Rules {
		byID[pb.Rules[i].ID] = pb.Rules[i]
	}
	for i := range c.appliedT {
		if r, ok := byID[c.appliedT[i].RuleID]; ok {
			rc := r
			c.appliedT[i].Rule = &rc
		}
	}
}

func printConsolidateText(cmd *cobra.Command, proc *consolidator, dryRun bool) {
	out := cmd.OutOrStdout()
	if dryRun {
		fmt.Fprintln(out, "DRY RUN — no events written")
	}
	for i := range proc.appliedT {
		a := &proc.appliedT[i]
		fmt.Fprintf(out, "applied %s %s %s\n", a.Op, a.RuleID, a.Note)
	}
	for i := range proc.skippedT {
		s := &proc.skippedT[i]
		fmt.Fprintf(out, "skipped %s: %s\n", s.Delta.Op, s.Reason)
	}
	for i := range proc.conflicts {
		cf := &proc.conflicts[i]
		fmt.Fprintf(out, "conflict with %s: %s\n", cf.RuleID, cf.Reason)
	}
}

func joinValidation(errs []rules.ValidationError) string {
	parts := make([]string, 0, len(errs))
	for _, e := range errs {
		parts = append(parts, fmt.Sprintf("%s: %s", e.Field, e.Message))
	}
	return strings.Join(parts, "; ")
}

// readConsolidateInput returns the raw delta-document JSON, reading stdin when the
// argument is "-".
func readConsolidateInput(cmd *cobra.Command, arg string) ([]byte, error) {
	if arg == "-" {
		in := cmd.InOrStdin()
		if in == nil {
			in = os.Stdin
		}
		data, err := io.ReadAll(in)
		if err != nil {
			return nil, fmt.Errorf("read consolidation document from stdin: %w", err)
		}
		if strings.TrimSpace(string(data)) == "" {
			return nil, errors.New("empty document on stdin: pipe a JSON delta document or pass it as a positional argument")
		}
		return data, nil
	}
	if strings.TrimSpace(arg) == "" {
		return nil, errors.New("empty consolidation document: pass a JSON payload or '-' to read from stdin")
	}
	return []byte(arg), nil
}
