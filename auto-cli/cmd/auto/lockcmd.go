package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/mistakenot/auto-shared/lock"
	"github.com/spf13/cobra"
)

// newLockCmd is the parent for the serial-update lock surface (task 064):
// `auto lock take <group>`, `auto lock release [group]` and `auto lock status`;
// clear, doctor and init follow in later phases. Output is JSON on stdout;
// errors go to stderr with a non-zero exit.
func newLockCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "lock",
		Short: "Serial-update locks on groups of files (hook-enforced, opt-in per project)",
	}
	cmd.AddCommand(newLockTakeCmd())
	cmd.AddCommand(newLockReleaseCmd())
	cmd.AddCommand(newLockStatusCmd())
	return cmd
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
	cfg, err := lock.LoadConfig(repo.Root)
	if err != nil {
		return lockContext{}, err
	}
	if cfg == nil {
		return lockContext{}, fmt.Errorf("no lock config at %s: this project has not opted in to serial-update locks", lock.ConfigPath(repo.Root))
	}
	worker, err := lock.ResolveWorkerAs(cwd, nil, cfg.Identity, as)
	if err != nil {
		return lockContext{}, err
	}
	return lockContext{repo: repo, config: cfg, worker: worker}, nil
}

// addAsFlag registers --as, the CLI spelling of the AUTO_LOCK_WORKER override
// (D-1 step 1). take and release share it so a lock taken --as X is released
// --as X.
func addAsFlag(cmd *cobra.Command, as *string) {
	cmd.Flags().StringVar(as, "as", "", "act as this worker id instead of the resolved identity (same as "+lock.WorkerEnv+")")
}

// newLockTakeCmd implements `auto lock take <group>`: acquire the named Group
// for the current Worker. Idempotent when already self-held; exits non-zero
// naming the holder when another Worker holds it.
func newLockTakeCmd() *cobra.Command {
	var reason, as string
	cmd := &cobra.Command{
		Use:   "take <group>",
		Short: "Take the lock on a group for the current Worker",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			group := args[0]
			lc, err := resolveLockContext(as)
			if err != nil {
				return err
			}
			if lc.config.Group(group) == nil {
				return fmt.Errorf("unknown lock group %q in %s (see `auto lock status` for the configured groups)", group, lock.ConfigPath(lc.repo.Root))
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
			return writeLockJSON(cmd.OutOrStdout(), l)
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
func newLockReleaseCmd() *cobra.Command {
	var as string
	cmd := &cobra.Command{
		Use:   "release [group]",
		Short: "Release the current Worker's locks on this project (all, or one group)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var group string
			if len(args) == 1 {
				group = args[0]
			}
			lc, err := resolveLockContext(as)
			if err != nil {
				return err
			}
			if group != "" && lc.config.Group(group) == nil {
				return fmt.Errorf("unknown lock group %q in %s (see `auto lock status` for the configured groups)", group, lock.ConfigPath(lc.repo.Root))
			}
			store, err := lock.OpenDefault()
			if err != nil {
				return err
			}
			n, err := store.Release(lc.worker, group)
			if err != nil {
				return err
			}
			return writeLockJSON(cmd.OutOrStdout(), lockRelease{
				Project:  lc.repo.Project,
				Worker:   lc.worker.Holder,
				Group:    group,
				Released: n,
			})
		},
	}
	addAsFlag(cmd, &as)
	return cmd
}

// lockStatus is the `auto lock status` payload: the project's configured
// groups and the locks currently held on it, each flagged when held by the
// caller.
type lockStatus struct {
	Project string           `json:"project"`
	Worker  lock.Holder      `json:"worker"`
	Groups  []lock.Group     `json:"groups"`
	Locks   []lockStatusLock `json:"locks"`
}

type lockStatusLock struct {
	lock.Lock
	HeldByYou bool `json:"held_by_you"`
}

// newLockStatusCmd implements `auto lock status`: the configured groups and
// every lock held on this project, JSON on stdout.
func newLockStatusCmd() *cobra.Command {
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
			out := lockStatus{
				Project: lc.repo.Project,
				Worker:  lc.worker.Holder,
				Groups:  lc.config.Groups,
				Locks:   make([]lockStatusLock, 0, len(locks)),
			}
			for i := range locks {
				out.Locks = append(out.Locks, lockStatusLock{Lock: locks[i], HeldByYou: lc.worker.Matches(locks[i].Holder)})
			}
			return writeLockJSON(cmd.OutOrStdout(), out)
		},
	}
}

// writeLockJSON writes v as 2-space-indented JSON followed by a newline.
func writeLockJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
