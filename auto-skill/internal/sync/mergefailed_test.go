package sync

import (
	"testing"

	"github.com/mistakenot/auto-skill/internal/skill"
)

// TestMergeFailedRendersCarriesForwardIntended verifies the transactional-manifest
// fix (#4 / foreign-target cluster): a skill that was previously managed but did
// NOT stage this run keeps its prior manifest ownership when it is still intended,
// and is dropped when it is not (a genuine removal).
func TestMergeFailedRendersCarriesForwardIntended(t *testing.T) {
	old := &skill.Manifest{
		Version: 1,
		Skills: map[string]skill.ManifestSkill{
			"kept":    {TemplateHash: "aa", SkillVersion: "aa", RenderVersion: "1"},
			"removed": {TemplateHash: "bb", SkillVersion: "bb", RenderVersion: "1"},
		},
		Targets: map[string]skill.ManifestTarget{
			"claude": {ManagedSkills: map[string]string{"kept": "aa", "removed": "bb"}},
		},
	}
	fresh := &skill.Manifest{
		Version: 1,
		Skills:  map[string]skill.ManifestSkill{}, // nothing staged this run
		Targets: map[string]skill.ManifestTarget{
			"claude": {ManagedSkills: map[string]string{}},
		},
	}
	// "kept" is still declared (intended); "removed" was dropped from the lock.
	intended := map[string]bool{"kept": true}

	got := mergeFailedRenders(old, fresh, intended)

	if _, ok := got.Skills["kept"]; !ok {
		t.Error("intended-but-unstaged skill 'kept' should be carried forward")
	}
	if _, ok := got.Skills["removed"]; ok {
		t.Error("removed skill should NOT be carried forward (must prune)")
	}
	managed := got.Targets["claude"].ManagedSkills
	if _, ok := managed["kept"]; !ok {
		t.Error("target ownership of 'kept' should be carried forward")
	}
	if _, ok := managed["removed"]; ok {
		t.Error("target ownership of 'removed' should be dropped")
	}
}

func TestMergeFailedRendersPrefersFresh(t *testing.T) {
	old := &skill.Manifest{
		Skills:  map[string]skill.ManifestSkill{"a": {TemplateHash: "old", SkillVersion: "old", RenderVersion: "1"}},
		Targets: map[string]skill.ManifestTarget{"claude": {ManagedSkills: map[string]string{"a": "old"}}},
	}
	fresh := &skill.Manifest{
		Skills:  map[string]skill.ManifestSkill{"a": {TemplateHash: "new", SkillVersion: "new", RenderVersion: "1"}},
		Targets: map[string]skill.ManifestTarget{"claude": {ManagedSkills: map[string]string{"a": "new"}}},
	}
	got := mergeFailedRenders(old, fresh, map[string]bool{"a": true})
	if got.Skills["a"].SkillVersion != "new" {
		t.Errorf("a staged this run must keep its fresh entry, got %q", got.Skills["a"].SkillVersion)
	}
}
