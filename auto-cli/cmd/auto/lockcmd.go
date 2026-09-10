package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	sharedconfig "github.com/mistakenot/auto-shared/config"
	"github.com/mistakenot/auto-shared/lock"
	"github.com/spf13/cobra"
)

// newLockCmd is the parent for the serial-update lock surface (task 064):
// `auto lock init --project`, `take <group>`, `release [group]`, `status`,
// `clear <group>` and `doctor`. Output is JSON on stdout by default, a
// human-readable rendering with --text; errors go to stderr with a non-zero
// exit.
func newLockCmd() *cobra.Command {
	opts := &lockOpts{}
	cmd := &cobra.Command{
		Use:   "lock",
		Short: "Serial-update locks on groups of files (hook-enforced, opt-in per project)",
	}
	cmd.PersistentFlags().BoolVar(&opts.text, "text", false, "human-readable output instead of JSON")
	cmd.AddCommand(newLockInitCmd(opts))
	cmd.AddCommand(newLockTakeCmd(opts))
	cmd.AddCommand(newLockReleaseCmd(opts))
	cmd.AddCommand(newLockStatusCmd(opts))
	cmd.AddCommand(newLockClearCmd(opts))
	cmd.AddCommand(newLockDoctorCmd(opts))
	return cmd
}

// lockOpts holds the flag shared by every lock subcommand: --text switches
// the stdout payload from JSON (the default) to a human-readable rendering.
type lockOpts struct {
	text bool
}

// emit writes the command's result to stdout: v as JSON, or text's rendering
// when --text is set. stdout carries nothing else in either mode.
func (o *lockOpts) emit(cmd *cobra.Command, v any, text func(w io.Writer)) error {
	if o.text {
		text(cmd.OutOrStdout())
		return nil
	}
	return writeLockJSON(cmd.OutOrStdout(), v)
}

// The fire commands `auto hooks install` wires into each agent's hook config;
// doctor and init look for exactly these.
const (
	claudeFireCommand = "auto hooks fire --agent claude"
	codexFireCommand  = "auto hooks fire --agent codex"
)

// newGHChecker builds the PR checker `auto lock clear` verifies with, given a
// directory inside the repo. It is a package var so tests can script gh.
var newGHChecker = func(dir string) lock.GHChecker {
	return lock.GHCLI{Dir: dir}
}

// lockContext is what every lock subcommand resolves first: the repo the
// caller is in, its lock config (which must exist), and the caller's Worker.
type lockContext struct {
	repo   lock.Repo
	config *lock.Config
	worker lock.Worker
}

// resolveLockContext resolves cwd → repo, config and Worker, failing with a
// remediation hint when the project has not opted in or the Worker cannot be
// identified. as is the --as override (an explicit worker id that stands in
// for AUTO_LOCK_WORKER on this invocation only); "" defers to the env.
func resolveLockContext(as string) (lockContext, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return lockContext{}, err
	}
	repo, err := lock.ResolveRepo(cwd)
	if err != nil {
		return lockContext{}, err
	}
	cfg, err := loadLockConfig(repo.Root)
	if err != nil {
		return lockContext{}, err
	}
	worker, err := lock.ResolveWorkerAs(cwd, nil, cfg.Identity, as)
	if err != nil {
		return lockContext{}, err
	}
	return lockContext{repo: repo, config: cfg, worker: worker}, nil
}

// loadLockConfig loads the project's lock config for a command that needs it:
// a missing file is the "not opted in" error with the init remediation, and a
// config that fails validation is an error listing EVERY offending field (the
// library error carries them structurally but prints only the first).
func loadLockConfig(root string) (*lock.Config, error) {
	cfg, err := lock.LoadConfig(root)
	var verr *sharedconfig.ValidationErrorsError
	if errors.As(err, &verr) {
		return nil, fmt.Errorf("invalid lock config %s:\n  %s\n(fix the listed fields, then re-run; `auto lock doctor` checks the whole setup)",
			verr.Path, strings.Join(validationLines(verr.Errors), "\n  "))
	}
	if err != nil {
		return nil, err
	}
	if cfg == nil {
		return nil, fmt.Errorf("no lock config at %s: this project has not opted in to serial-update locks (run `auto lock init --project`)", lock.ConfigPath(root))
	}
	return cfg, nil
}

// validationLines renders each field error as "path: message (got value)".
func validationLines(errs []sharedconfig.ValidationError) []string {
	out := make([]string, 0, len(errs))
	for _, e := range errs {
		line := e.Path + ": " + e.Message
		if e.Value != nil {
			line += fmt.Sprintf(" (got %v)", e.Value)
		}
		out = append(out, line)
	}
	return out
}

// lockGroupArg turns a group argument into a configured Group name: it is
// normalized (trimmed, lowercased) and checked against the same slug rule the
// config enforces before it is looked up, so an unknown-group error is only
// ever reported for a well-formed name.
func lockGroupArg(cfg *lock.Config, root, raw string) (string, error) {
	name := lock.NormalizeGroupName(raw)
	if !lock.ValidGroupName(name) {
		return "", fmt.Errorf("invalid lock group name %q: group names match %s (see `auto lock status` for the configured groups)", raw, lock.GroupNamePattern)
	}
	if cfg.Group(name) == nil {
		return "", fmt.Errorf("unknown lock group %q in %s (see `auto lock status` for the configured groups)", name, lock.ConfigPath(root))
	}
	return name, nil
}

// addAsFlag registers --as, the CLI spelling of the AUTO_LOCK_WORKER override
// (D-1 step 1). take and release share it so a lock taken --as X is released
// --as X.
func addAsFlag(cmd *cobra.Command, as *string) {
	cmd.Flags().StringVar(as, "as", "", "act as this worker id instead of the resolved identity (same as "+lock.WorkerEnv+")")
}

// holderLabel is the short human-facing name of a Holder for text output: the
// branch for a worktree holder, otherwise its worker id.
func holderLabel(h lock.Holder) string {
	if h.Kind == lock.KindWorktree || (h.Kind == "" && h.Branch != "") {
		return h.Branch
	}
	return h.WorkerID
}

// newLockTakeCmd implements `auto lock take <group>`: acquire the named Group
// for the current Worker. Idempotent when already self-held; exits non-zero
// naming the holder when another Worker holds it.
func newLockTakeCmd(opts *lockOpts) *cobra.Command {
	var reason, as string
	cmd := &cobra.Command{
		Use:   "take <group>",
		Short: "Take the lock on a group for the current Worker",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			lc, err := resolveLockContext(as)
			if err != nil {
				return err
			}
			group, err := lockGroupArg(lc.config, lc.repo.Root, args[0])
			if err != nil {
				return err
			}
			store, err := lock.OpenDefault()
			if err != nil {
				return err
			}
			l, err := store.Take(lc.repo.Project, group, lc.worker, reason)
			var held *lock.HeldError
			if errors.As(err, &held) {
				if held.Lock.Holder.Kind == lock.KindWorktree {
					return fmt.Errorf("%w; wait for it to be released, or run `auto lock clear %s` once its PR is merged", err, group)
				}
				return fmt.Errorf("%w; that worker has no PR to verify — wait for it to run `auto lock release`, or `auto lock clear %s --force` once you have confirmed it is done", err, group)
			}
			if err != nil {
				return err
			}
			return opts.emit(cmd, l, func(w io.Writer) {
				fmt.Fprintf(w, "✓ took lock %q as %s (%s)\n", l.Group, holderLabel(l.Holder), l.Holder.Kind)
			})
		},
	}
	cmd.Flags().StringVar(&reason, "reason", "", "why this Worker is taking the lock (shown to blocked agents)")
	addAsFlag(cmd, &as)
	return cmd
}

// lockRelease is the `auto lock release` payload.
type lockRelease struct {
	Project  string      `json:"project"`
	Worker   lock.Holder `json:"worker"`
	Group    string      `json:"group,omitempty"`
	Released int         `json:"released"`
}

// newLockReleaseCmd implements `auto lock release [group]`: free every lock the
// current Worker holds on this project (run at merge time), or just the named
// group. Releasing nothing is a success with released 0.
func newLockReleaseCmd(opts *lockOpts) *cobra.Command {
	var as string
	cmd := &cobra.Command{
		Use:   "release [group]",
		Short: "Release the current Worker's locks on this project (all, or one group)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			lc, err := resolveLockContext(as)
			if err != nil {
				return err
			}
			var group string
			if len(args) == 1 {
				if group, err = lockGroupArg(lc.config, lc.repo.Root, args[0]); err != nil {
					return err
				}
			}
			store, err := lock.OpenDefault()
			if err != nil {
				return err
			}
			n, err := store.Release(lc.worker, group)
			if err != nil {
				return err
			}
			return opts.emit(cmd, lockRelease{
				Project:  lc.repo.Project,
				Worker:   lc.worker.Holder,
				Group:    group,
				Released: n,
			}, func(w io.Writer) {
				fmt.Fprintf(w, "✓ released %d lock%s\n", n, plural(n))
			})
		},
	}
	addAsFlag(cmd, &as)
	return cmd
}

// lockStatus is the `auto lock status` payload (resource-list shape): every
// configured Group with its current hold, plus the raw locks held on the
// project — the same information keyed by lock, which also surfaces a lock
// whose Group has since been removed from the config.
type lockStatus struct {
	Project string            `json:"project"`
	Worker  lock.Holder       `json:"worker"`
	Groups  []lockStatusGroup `json:"groups"`
	Locks   []lockStatusLock  `json:"locks"`
}

// lockStatusGroup is one configured Group and who, if anyone, holds it.
type lockStatusGroup struct {
	Name        string       `json:"name"`
	Description string       `json:"description"`
	Globs       []string     `json:"globs"`
	Held        bool         `json:"held"`
	Holder      *lock.Holder `json:"holder,omitempty"`
	HeldByYou   bool         `json:"held_by_you"`
	Reason      string       `json:"reason,omitempty"`
	PR          string       `json:"pr,omitempty"`
	TakenAt     string       `json:"taken_at,omitempty"`
}

type lockStatusLock struct {
	lock.Lock
	HeldByYou bool `json:"held_by_you"`
}

// newLockStatusCmd implements `auto lock status`: the configured groups
// (listed whether or not they are held) and every lock held on this project.
func newLockStatusCmd(opts *lockOpts) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show the configured lock groups and who holds what on this project",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			lc, err := resolveLockContext("")
			if err != nil {
				return err
			}
			store, err := lock.OpenDefault()
			if err != nil {
				return err
			}
			locks, err := store.List(lc.repo.Project)
			if err != nil {
				return err
			}
			held := map[string]lockStatusLock{}
			out := lockStatus{
				Project: lc.repo.Project,
				Worker:  lc.worker.Holder,
				Groups:  make([]lockStatusGroup, 0, len(lc.config.Groups)),
				Locks:   make([]lockStatusLock, 0, len(locks)),
			}
			for i := range locks {
				l := lockStatusLock{Lock: locks[i], HeldByYou: lc.worker.Matches(locks[i].Holder)}
				out.Locks = append(out.Locks, l)
				held[l.Group] = l
			}
			for _, g := range lc.config.Groups {
				sg := lockStatusGroup{Name: g.Name, Description: g.Description, Globs: g.Globs}
				if l, ok := held[g.Name]; ok {
					holder := l.Holder
					sg.Held, sg.Holder, sg.HeldByYou = true, &holder, l.HeldByYou
					sg.Reason, sg.PR, sg.TakenAt = l.Reason, l.PR, l.TakenAt
				}
				out.Groups = append(out.Groups, sg)
			}
			return opts.emit(cmd, out, func(w io.Writer) { writeLockStatusText(w, out) })
		},
	}
}

// writeLockStatusText renders status one line per group:
//
//	drizzle-schema  HELD by feat/orders (you)  since 14:02
//	api-routes      free
//
// A lock on a group no longer in the config follows, marked as such.
func writeLockStatusText(w io.Writer, st lockStatus) {
	width := 0
	for i := range st.Groups {
		width = max(width, len(st.Groups[i].Name))
	}
	configured := map[string]bool{}
	for i := range st.Groups {
		g := &st.Groups[i]
		configured[g.Name] = true
		if !g.Held {
			fmt.Fprintf(w, "%-*s  free\n", width, g.Name)
			continue
		}
		fmt.Fprintf(w, "%-*s  HELD by %s%s  since %s\n", width, g.Name, holderLabel(*g.Holder), youSuffix(g.HeldByYou), lockSince(g.TakenAt))
	}
	for i := range st.Locks {
		l := &st.Locks[i]
		if !configured[l.Group] {
			fmt.Fprintf(w, "%-*s  HELD by %s%s  since %s  (group not in config)\n", width, l.Group, holderLabel(l.Holder), youSuffix(l.HeldByYou), lockSince(l.TakenAt))
		}
	}
}

func youSuffix(you bool) string {
	if you {
		return " (you)"
	}
	return ""
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// lockSince renders an RFC 3339 taken_at for text output: the local clock
// time when it was today, otherwise date and time.
func lockSince(ts string) string {
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return ts
	}
	t = t.Local()
	if t.Format(time.DateOnly) == time.Now().Local().Format(time.DateOnly) {
		return t.Format("15:04")
	}
	return t.Format("2006-01-02 15:04")
}

// lockClear is the `auto lock clear` payload.
type lockClear struct {
	Cleared bool        `json:"cleared"`
	Project string      `json:"project"`
	Group   string      `json:"group"`
	Holder  lock.Holder `json:"holder"`
	Forced  bool        `json:"forced"`
	PR      string      `json:"pr,omitempty"`
	State   string      `json:"state,omitempty"`
}

// newLockClearCmd implements `auto lock clear <group> [--force]`: remove
// another Worker's lock once its PR is merged (verified via gh), or
// unconditionally with --force. A refusal is a non-zero exit with the PR
// state on stderr and the store untouched (D-3, AC-9). The clearing Worker is
// recorded in the audit; when it cannot be identified (bare main, D-6) the
// entry carries just this host, since clear needs no identity of its own.
func newLockClearCmd(opts *lockOpts) *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "clear <group>",
		Short: "Clear another Worker's lock on a group (refuses unless its PR is merged; --force overrides)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			repo, err := lock.ResolveRepo(cwd)
			if err != nil {
				return err
			}
			cfg, err := loadLockConfig(repo.Root)
			if err != nil {
				return err
			}
			group, err := lockGroupArg(cfg, repo.Root, args[0])
			if err != nil {
				return err
			}
			by := lock.Holder{Host: sharedconfig.HostIDQuietly()}
			if w, err := lock.ResolveWorker(cwd, nil, cfg.Identity); err == nil {
				by = w.Holder
			}
			store, err := lock.OpenDefault()
			if err != nil {
				return err
			}
			res, err := store.Clear(repo.Project, group, by, force, newGHChecker(repo.Root))
			if err != nil {
				return err
			}
			out := lockClear{
				Cleared: true,
				Project: repo.Project,
				Group:   group,
				Holder:  res.Lock.Holder,
				Forced:  res.Forced,
				PR:      res.PR,
				State:   res.State,
			}
			return opts.emit(cmd, out, func(w io.Writer) {
				how := "forced"
				if !out.Forced {
					how = "PR #" + strings.TrimPrefix(out.PR, "#") + " " + out.State
				}
				fmt.Fprintf(w, "✓ cleared lock %q held by %s (%s)\n", out.Group, lock.DescribeHolder(out.Holder), how)
			})
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "clear even if the holder's PR is not merged (audited)")
	return cmd
}

// lockInit is the `auto lock init --project` payload.
type lockInit struct {
	Path                string   `json:"path"`
	Created             bool     `json:"created"`
	Identity            string   `json:"identity,omitempty"`
	Groups              []string `json:"groups"`
	ClaudeHookInstalled bool     `json:"claude_hook_installed"`
	Hint                string   `json:"hint,omitempty"`
}

// lockInitScaffold is the config `auto lock init --project` writes: identity
// auto and one example group to edit or replace. It passes Validate, so the
// project is usable (and `auto lock status` lists the example) immediately.
var lockInitScaffold = lock.Config{
	Identity: lock.IdentityAuto,
	Groups: []lock.Group{{
		Name:        "example-group",
		Globs:       []string{"db/schema/**"},
		Description: "EXAMPLE — replace with your own group. Say why these files must be edited serially; blocked agents are shown this.",
	}},
}

// hookInstallHint is the init/doctor remediation when the Claude PreToolUse
// hook — the only thing that makes a lock enforceable (D-13) — is missing.
const hookInstallHint = "run auto hooks install"

// newLockInitCmd implements `auto lock init --project`: scaffold
// .auto/lock/settings.json (never overwriting one that exists) and report
// whether the Claude PreToolUse hook that enforces it is installed. --project
// is required because the lock config is project-local only.
func newLockInitCmd(opts *lockOpts) *cobra.Command {
	var project bool
	cmd := &cobra.Command{
		Use:   "init --project",
		Short: "Opt this project in: scaffold .auto/lock/settings.json and check the enforcing hook",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			repo, err := lock.ResolveRepo(cwd)
			if err != nil {
				return err
			}
			out := lockInit{Path: lock.ConfigPath(repo.Root), Groups: []string{}}
			errOut := cmd.ErrOrStderr()
			if _, err := os.Stat(out.Path); err == nil {
				fmt.Fprintf(errOut, "%s already exists; left untouched\n", out.Path)
				if cfg, err := lock.LoadConfig(repo.Root); err == nil && cfg != nil {
					out.Identity = cfg.Identity
					for _, g := range cfg.Groups {
						out.Groups = append(out.Groups, g.Name)
					}
				} else {
					fmt.Fprintf(errOut, "warning: existing config is invalid (%v); run `auto lock doctor`\n", err)
				}
			} else if errors.Is(err, os.ErrNotExist) {
				if err := sharedconfig.WriteJSONFileAtomic(out.Path, lockInitScaffold); err != nil {
					return err
				}
				out.Created = true
				out.Identity = lockInitScaffold.Identity
				for _, g := range lockInitScaffold.Groups {
					out.Groups = append(out.Groups, g.Name)
				}
			} else {
				return err
			}

			installed, err := hookInstalled(claudeSettingsPath(repo.Root), "PreToolUse", claudeFireCommand)
			if err != nil {
				return err
			}
			out.ClaudeHookInstalled = installed
			if !installed {
				out.Hint = hookInstallHint + " (the Claude PreToolUse hook is not installed, so locks are not enforced yet)"
				fmt.Fprintln(errOut, "warning: "+out.Hint)
			}
			return opts.emit(cmd, out, func(w io.Writer) {
				if out.Created {
					fmt.Fprintf(w, "✓ created %s (identity %s, example group %q)\n", out.Path, out.Identity, out.Groups[0])
				} else {
					fmt.Fprintf(w, "✓ %s already exists (%d group%s); left untouched\n", out.Path, len(out.Groups), plural(len(out.Groups)))
				}
				if installed {
					fmt.Fprintln(w, "✓ Claude PreToolUse hook installed: locks are enforced")
				} else {
					fmt.Fprintln(w, "✗ Claude PreToolUse hook not installed: "+hookInstallHint)
				}
			})
		},
	}
	cmd.Flags().BoolVar(&project, "project", false, "scaffold the project-local config (required: lock config is project-local only)")
	_ = cmd.MarkFlagRequired("project")
	return cmd
}

// lockDoctorCheck is the repo-standard doctor shape
// (docs/auto-package-patterns.md → doctor) plus a remediation hint.
type lockDoctorCheck struct {
	Check   string `json:"check"`
	Status  string `json:"status"`
	Message string `json:"message"`
	Hint    string `json:"hint,omitempty"`
}

const (
	doctorPass = "pass"
	doctorWarn = "warn"
	doctorFail = "fail"
)

// newLockDoctorCmd implements `auto lock doctor`: is this setup actually
// enforceable? Emits []DoctorCheck and exits non-zero if any check fails.
func newLockDoctorCmd(opts *lockOpts) *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check that serial-update locks are configured and enforceable here",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			checks := runLockDoctor(cwd)
			if err := opts.emit(cmd, checks, func(w io.Writer) { writeLockDoctorText(w, checks) }); err != nil {
				return err
			}
			failed := 0
			for _, c := range checks {
				if c.Status == doctorFail {
					failed++
				}
			}
			if failed > 0 {
				return fmt.Errorf("auto lock doctor: %d check%s failed (each carries a hint)", failed, plural(failed))
			}
			return nil
		},
	}
}

// runLockDoctor runs every check from cwd. The hook, config and identity
// checks need a repo; outside one they collapse into a single failing repo
// check, while the host-level store and gh checks still run.
func runLockDoctor(cwd string) []lockDoctorCheck {
	var checks []lockDoctorCheck
	repo, err := lock.ResolveRepo(cwd)
	if err != nil {
		checks = append(checks, lockDoctorCheck{
			Check: "repo", Status: doctorFail, Message: err.Error(),
			Hint: "run auto lock doctor from inside the git repository whose locks you want to check",
		})
	} else {
		checks = append(checks, checkClaudeHook(repo.Root), checkCodexHook(repo.Root))
		cfg, cfgCheck := checkLockConfig(repo.Root)
		checks = append(checks, cfgCheck, checkLockIdentity(cwd, cfg))
	}
	return append(checks, checkLockStore(), checkGH())
}

func claudeSettingsPath(root string) string {
	return filepath.Join(root, ".claude", "settings.json")
}

func codexHooksPath(root string) string {
	return filepath.Join(root, ".codex", "hooks.json")
}

// hookInstalled reports whether the hook config at path carries a command
// handler for command on event — the same test `auto hooks install` uses to
// decide a handler is already present. A missing file is simply "no".
func hookInstalled(path, event, command string) (bool, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read %s: %w", path, err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		return false, fmt.Errorf("parse %s: %w", path, err)
	}
	hooks, _ := doc["hooks"].(map[string]any)
	groups, _ := hooks[event].([]any)
	return handlerExists(groups, command), nil
}

// checkClaudeHook is the ONE check that can pass enforceability (D-13): the
// guard only runs when `auto hooks fire --agent claude` is wired onto
// PreToolUse in this project's .claude/settings.json.
func checkClaudeHook(root string) lockDoctorCheck {
	path := claudeSettingsPath(root)
	installed, err := hookInstalled(path, "PreToolUse", claudeFireCommand)
	switch {
	case err != nil:
		return lockDoctorCheck{Check: "claude-hook", Status: doctorFail, Message: err.Error(),
			Hint: "fix " + path + " so it parses, then " + hookInstallHint}
	case !installed:
		return lockDoctorCheck{Check: "claude-hook", Status: doctorFail,
			Message: fmt.Sprintf("%q is not installed on PreToolUse in %s: the guard never runs, so locks are not enforced", claudeFireCommand, path),
			Hint:    hookInstallHint}
	default:
		return lockDoctorCheck{Check: "claude-hook", Status: doctorPass,
			Message: fmt.Sprintf("%q is installed on PreToolUse in %s: Claude Code edits to locked files are denied", claudeFireCommand, path)}
	}
}

// checkCodexHook never passes (D-12/D-13): an untrusted Codex hook is inert
// and its trust state cannot be inspected, and v1 does not enforce on Codex
// anyway (apply_patch carries no file path to match).
func checkCodexHook(root string) lockDoctorCheck {
	path := codexHooksPath(root)
	installed, err := hookInstalled(path, "PreToolUse", codexFireCommand)
	const scope = "v1 does not enforce locks on Codex (D-12)"
	switch {
	case err != nil:
		return lockDoctorCheck{Check: "codex-hook", Status: doctorWarn, Message: err.Error() + "; " + scope,
			Hint: "fix " + path + " so it parses, then " + hookInstallHint}
	case !installed:
		return lockDoctorCheck{Check: "codex-hook", Status: doctorWarn,
			Message: fmt.Sprintf("%q is not installed in %s; %s, so this is informational", codexFireCommand, path, scope),
			Hint:    hookInstallHint + " to wire Codex hooks for hints and events"}
	default:
		return lockDoctorCheck{Check: "codex-hook", Status: doctorWarn,
			Message: fmt.Sprintf("%q is present in %s but not verifiable: hook trust state cannot be inspected (D-13), and %s", codexFireCommand, path, scope),
			Hint:    "trust the hook via /hooks in Codex; only the Claude PreToolUse hook enforces locks in v1"}
	}
}

// checkLockConfig validates .auto/lock/settings.json through the shared
// validator and returns the config (nil unless valid) for the identity check.
func checkLockConfig(root string) (*lock.Config, lockDoctorCheck) {
	path := lock.ConfigPath(root)
	cfg, err := lock.LoadConfig(root)
	var verr *sharedconfig.ValidationErrorsError
	switch {
	case errors.As(err, &verr):
		return nil, lockDoctorCheck{Check: "config", Status: doctorFail,
			Message: fmt.Sprintf("%s is invalid: %s", path, strings.Join(validationLines(verr.Errors), "; ")),
			Hint:    "fix the listed fields in " + path + " (group names match " + lock.GroupNamePattern + ")"}
	case err != nil:
		return nil, lockDoctorCheck{Check: "config", Status: doctorFail, Message: err.Error(),
			Hint: "fix " + path + " so it parses as JSON"}
	case cfg == nil:
		return nil, lockDoctorCheck{Check: "config", Status: doctorFail,
			Message: path + " is missing: this project has not opted in to serial-update locks",
			Hint:    "run auto lock init --project"}
	}
	names := make([]string, 0, len(cfg.Groups))
	for _, g := range cfg.Groups {
		names = append(names, g.Name)
	}
	return cfg, lockDoctorCheck{Check: "config", Status: doctorPass,
		Message: fmt.Sprintf("%s is valid: identity %s, %d group%s (%s)", path, cfg.Identity, len(cfg.Groups), plural(len(cfg.Groups)), strings.Join(names, ", "))}
}

// checkLockIdentity resolves the caller's Worker under the config's identity
// mode (auto when the config is absent or invalid). Bare (D-6) is a warning
// with the remediation: locks cannot be taken here, and the guard would deny
// edits to lockable files.
func checkLockIdentity(cwd string, cfg *lock.Config) lockDoctorCheck {
	identity := lock.IdentityAuto
	if cfg != nil {
		identity = cfg.Identity
	}
	w, err := lock.ResolveWorker(cwd, nil, identity)
	var bare *lock.BareError
	switch {
	case errors.As(err, &bare):
		return lockDoctorCheck{Check: "identity", Status: doctorWarn,
			Message: "this Worker cannot be identified (locks cannot be taken here, and edits to lockable files are denied): " + bare.Detail(),
			Hint:    bare.Remediation()}
	case err != nil:
		return lockDoctorCheck{Check: "identity", Status: doctorWarn, Message: err.Error(),
			Hint: "export " + lock.WorkerEnv + "=<name> to pin an explicit worker id"}
	}
	return lockDoctorCheck{Check: "identity", Status: doctorPass,
		Message: fmt.Sprintf("this Worker resolves as %s (kind %s) on host %s", lock.DescribeHolder(w.Holder), w.Kind, w.Host)}
}

// checkLockStore verifies ~/.auto/lock/locks.json is absent (fine: created on
// first take), or parseable and in a writable directory.
func checkLockStore() lockDoctorCheck {
	store, err := lock.OpenDefault()
	if err != nil {
		return lockDoctorCheck{Check: "store", Status: doctorFail, Message: err.Error(),
			Hint: "set HOME so ~/.auto is resolvable"}
	}
	path := store.Path()
	_, statErr := os.Stat(path)
	absent := errors.Is(statErr, os.ErrNotExist)
	locks, err := store.List("")
	if err != nil {
		return lockDoctorCheck{Check: "store", Status: doctorFail, Message: err.Error(),
			Hint: "fix the JSON in " + path + ", or delete the file (every lock on this host is forgotten), and make " + store.Dir + " writable"}
	}
	probe, err := os.CreateTemp(store.Dir, ".doctor-*")
	if err != nil {
		return lockDoctorCheck{Check: "store", Status: doctorFail,
			Message: store.Dir + " is not writable: locks cannot be taken or released on this host",
			Hint:    "make " + store.Dir + " writable by this user"}
	}
	_ = probe.Close()
	_ = os.Remove(probe.Name())
	if absent {
		return lockDoctorCheck{Check: "store", Status: doctorPass,
			Message: path + " does not exist yet; it will be created on the first take"}
	}
	return lockDoctorCheck{Check: "store", Status: doctorPass,
		Message: fmt.Sprintf("%s is readable and writable (%d lock%s held on this host)", path, len(locks), plural(len(locks)))}
}

// checkGH reports whether the GitHub CLI the merge-verified clear relies on
// is available; without it clear still works with --force.
func checkGH() lockDoctorCheck {
	path, err := exec.LookPath("gh")
	if err != nil {
		return lockDoctorCheck{Check: "gh", Status: doctorWarn,
			Message: "gh is not on PATH: merge-verified `auto lock clear` is unavailable; use `clear --force` once you have confirmed the PR is merged, or rely on liveness reclaim when the holder's worktree or pane is gone",
			Hint:    "install the GitHub CLI (https://cli.github.com) and run gh auth login"}
	}
	return lockDoctorCheck{Check: "gh", Status: doctorPass,
		Message: "gh found at " + path + ": `auto lock clear` can verify a holder's PR is merged"}
}

// writeLockDoctorText renders one line per check, passes first, then
// warnings, then failures, each failure or warning followed by its hint.
func writeLockDoctorText(w io.Writer, checks []lockDoctorCheck) {
	width := 0
	for _, c := range checks {
		width = max(width, len(c.Check))
	}
	for _, status := range []string{doctorPass, doctorWarn, doctorFail} {
		for _, c := range checks {
			if c.Status != status {
				continue
			}
			fmt.Fprintf(w, "%s %-*s  %s\n", doctorMark(status), width, c.Check, c.Message)
			if c.Hint != "" {
				fmt.Fprintf(w, "  %-*s  hint: %s\n", width, "", c.Hint)
			}
		}
	}
}

func doctorMark(status string) string {
	switch status {
	case doctorPass:
		return "✓"
	case doctorWarn:
		return "!"
	default:
		return "✗"
	}
}

// writeLockJSON writes v as 2-space-indented JSON followed by a newline.
func writeLockJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false) // hints quote <name>; keep them readable
	return enc.Encode(v)
}
