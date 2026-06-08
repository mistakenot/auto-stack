package rules

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/mistakenot/auto-reflect/internal/store"
)

type Service struct {
	Now func() time.Time
}

func NewService() *Service {
	return &Service{Now: func() time.Time { return time.Now().UTC() }}
}

func (s *Service) Create(playbookPath string, in CreateInput) (CreateResult, []ValidationError, error) {
	normalized, inputErrs := normalizeCreateInput(in)
	if len(inputErrs) > 0 {
		return CreateResult{}, inputErrs, nil
	}

	playbook, _, err := loadPlaybook(playbookPath)
	if err != nil {
		return CreateResult{}, nil, err
	}

	existingIDs := make(map[string]struct{}, len(playbook.Rules))
	for _, existing := range playbook.Rules {
		existingIDs[existing.ID] = struct{}{}
		if existing.Content == normalized.Content && existing.Category == normalized.Category {
			return CreateResult{}, nil, errors.New("duplicate rule: Run `auto reflect lookup` first or wait for a future rule update command")
		}
	}

	timestamp := s.Now().UTC().Format(time.RFC3339)
	id := newRuleID()
	for {
		if _, exists := existingIDs[id]; !exists {
			break
		}
		id = newRuleID()
	}

	rule := Rule{
		ID:        id,
		Content:   normalized.Content,
		Category:  normalized.Category,
		Tags:      normalized.Tags,
		CreatedAt: timestamp,
		UpdatedAt: timestamp,
	}

	playbook.Rules = append(playbook.Rules, rule)
	if err := store.WriteJSONFile(playbookPath, playbook); err != nil {
		return CreateResult{}, nil, fmt.Errorf("write playbook: %w", err)
	}

	return CreateResult{Path: playbookPath, Created: rule}, nil, nil
}

func (s *Service) Lookup(playbookPath, query string, limit int) (LookupResult, []ValidationError, error) {
	keywords := normalizeKeywords(query)
	if len(keywords) == 0 {
		return LookupResult{}, nil, errors.New("query is required")
	}

	playbook, validationErrs, err := loadPlaybookForLookup(playbookPath)
	if err != nil {
		return LookupResult{}, nil, err
	}

	matches := rankMatches(playbook.Rules, keywords)
	if limit > 0 && len(matches) > limit {
		matches = matches[:limit]
	}

	result := LookupResult{
		Query:    strings.TrimSpace(query),
		Keywords: keywords,
		Rules:    matches,
	}
	return result, validationErrs, nil
}

func loadPlaybook(path string) (Playbook, []ValidationError, error) {
	bytes, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Playbook{SchemaVersion: 1, Rules: []Rule{}}, nil, nil
		}
		return Playbook{}, nil, fmt.Errorf("read playbook: %w", err)
	}

	var playbook Playbook
	if err := json.Unmarshal(bytes, &playbook); err != nil {
		return Playbook{}, nil, errors.New("playbook parse error: Fix `.auto/reflect/playbook.json` or recreate it")
	}

	errs := validatePlaybook(path, playbook)
	if len(errs) > 0 {
		return Playbook{}, errs, errors.New("invalid playbook: fix `.auto/reflect/playbook.json` before writing new rules")
	}
	return playbook, nil, nil
}

func loadPlaybookForLookup(path string) (Playbook, []ValidationError, error) {
	bytes, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Playbook{SchemaVersion: 1, Rules: []Rule{}}, nil, nil
		}
		return Playbook{}, nil, fmt.Errorf("read playbook: %w", err)
	}

	var playbook Playbook
	if err := json.Unmarshal(bytes, &playbook); err != nil {
		return Playbook{}, nil, errors.New("playbook parse error: Fix `.auto/reflect/playbook.json` or recreate it")
	}

	allErrs := validatePlaybook(path, playbook)
	if len(allErrs) == 0 {
		return playbook, nil, nil
	}

	filtered := Playbook{SchemaVersion: playbook.SchemaVersion, Rules: make([]Rule, 0, len(playbook.Rules))}
	for i := range playbook.Rules {
		ruleErrs := validateRule(path, i, &playbook.Rules[i])
		if len(ruleErrs) == 0 {
			filtered.Rules = append(filtered.Rules, playbook.Rules[i])
		}
	}
	return filtered, allErrs, nil
}

func newRuleID() string {
	var bytes [4]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return fmt.Sprintf("r-%08x", time.Now().UnixNano())
	}
	return fmt.Sprintf("r-%02x%02x%02x%02x", bytes[0], bytes[1], bytes[2], bytes[3])
}
