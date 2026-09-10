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
// `auto lock take <group>` and `auto lock status` in Phase 1; release, clear,
// doctor and init follow in later phases. Output is JSON on stdout; errors go
// to stderr with a non-zero exit.
func newLockCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "lock",
		Short: "Serial-update locks on groups of files (hook-enforced, opt-in per project)",
	}
	cmd.AddCommand(newLockTakeCmd())
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
// identified.
func resolveLockContext() (lockContext, error) {
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
	worker, err := lock.ResolveWorker(cwd, nil, cfg.Identity)
	if err != nil {
		return lockContext{}, err
	}
	return lockContext{repo: repo, config: cfg, worker: worker}, nil
}

// newLockTakeCmd implements `auto lock take <group>`: acquire the named Group
// for the current Worker. Idempotent when already self-held; exits non-zero
// naming the holder when another Worker holds it.
func newLockTakeCmd() *cobra.Command {
	var reason string
	cmd := &cobra.Command{
		Use:   "take <group>",
		Short: "Take the lock on a group for the current Worker",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			group := args[0]
			lc, err := resolveLockContext()
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
				return fmt.Errorf("%w; wait for it to be released, or run `auto lock clear %s` once its PR is merged", err, group)
			}
			if err != nil {
				return err
			}
			return writeLockJSON(cmd.OutOrStdout(), l)
		},
	}
	cmd.Flags().StringVar(&reason, "reason", "", "why this Worker is taking the lock (shown to blocked agents)")
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
			lc, err := resolveLockContext()
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
