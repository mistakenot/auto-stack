package sync

import (
	"path/filepath"
	"time"

	"github.com/mistakenot/auto-shared/config"
	sharedgit "github.com/mistakenot/auto-shared/git"
	"github.com/mistakenot/auto-skill/internal/skill"
)

// Receipts is the machine-local record of what THIS machine wrote into each
// output target: target -> skill name -> rendered skill_version digest. It is
// the deletion authority T5 consumes — T4 only writes it (the orphan-prune pass
// that reads it lands in T5). Receipts live OUTSIDE the repo, under
// ~/.auto/skills/receipts/<project-id>.json, so a clone on another machine never
// inherits foreign ownership of a target directory.
type Receipts struct {
	Version   int                          `json:"version"`
	ProjectID string                       `json:"project_id"`
	Root      string                       `json:"root"`
	UpdatedAt string                       `json:"updated_at"`
	Targets   map[string]map[string]string `json:"targets"` // target -> skill -> digest
}

// newReceipts returns an empty receipts record stamped for this project.
func newReceipts(env skill.Env) *Receipts {
	return &Receipts{
		Version:   1,
		ProjectID: projectID(env),
		Root:      env.Root,
		Targets:   map[string]map[string]string{},
	}
}

// record registers (or refreshes) the digest this machine wrote for a skill in
// a target.
func (r *Receipts) record(target, name, digest string) {
	m := r.Targets[target]
	if m == nil {
		m = map[string]string{}
		r.Targets[target] = m
	}
	m[name] = digest
}

// receiptsDir is the machine-local receipts directory. It honors RootOverride
// (tests / isolated harnesses) exactly like Env.TrustPath / UpstreamCacheDir so
// a --root run never touches the real ~/.auto.
func receiptsDir(env skill.Env) string {
	if env.RootOverride {
		return filepath.Join(env.Root, ".auto", "skills", "receipts")
	}
	home, err := config.HomeDir()
	if err != nil {
		return filepath.Join(env.Root, ".auto", "skills", "receipts")
	}
	return filepath.Join(home, ".auto", "skills", "receipts")
}

// receiptsPath is the per-project receipts file.
func receiptsPath(env skill.Env) string {
	return filepath.Join(receiptsDir(env), projectID(env)+".json")
}

// projectID derives a deterministic, machine-local identifier for this project
// from its canonical absolute root path. A path hash is collision-free across
// distinct checkouts on one machine; the projects.json slug id (the repo's
// directory base name) is NOT — two repos named "foo" would collide and corrupt
// each other's deletion authority. Since receipts are a machine-local ownership
// ledger keyed per checkout, the path hash is authoritative here.
func projectID(env skill.Env) string {
	abs, err := filepath.Abs(env.Root)
	if err != nil {
		abs = env.Root
	}
	return sharedgit.ComputeRepoIDFromPath(filepath.Clean(abs))
}

// loadReceipts best-effort loads the existing receipts for this project,
// returning a fresh (empty) record on any miss/parse failure. Prior ownership is
// preserved so a partial sync never drops a target this machine still manages.
func loadReceipts(env skill.Env) *Receipts {
	r := newReceipts(env)
	if err := config.DecodeJSONFile(receiptsPath(env), r); err != nil {
		return newReceipts(env)
	}
	if r.Targets == nil {
		r.Targets = map[string]map[string]string{}
	}
	r.ProjectID = projectID(env)
	r.Root = env.Root
	return r
}

// buildReceipts merges the current install set's ownership (target -> skill ->
// skill_version) onto the existing receipts and stamps the write time.
func buildReceipts(env skill.Env, installs []Install) *Receipts {
	r := loadReceipts(env)
	for _, inst := range installs {
		r.record(inst.Target, inst.Skill, inst.Want)
	}
	r.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	return r
}
