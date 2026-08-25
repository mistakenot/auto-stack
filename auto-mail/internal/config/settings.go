// Package config resolves auto-mail's on-disk locations under ~/.auto/mail.
//
// The store filename carries the `alpha` marker (G10/D-2): it is an alpha
// artifact with no compatibility guarantee, and `auto mail reset` is a
// supported operation rather than a workaround.
package config

import (
	"path/filepath"

	sharedconfig "github.com/mistakenot/auto-shared/config"
)

const (
	mailDirName = "mail"
	// StoreFileName carries the alpha marker in the filename, not only in docs.
	StoreFileName = "alpha-store.db"
	// FlagsDirName holds the per-binding pending flags the hook stats (G8).
	FlagsDirName = "alpha-flags"
)

// ValidationError is an alias for the shared validation error type.
type ValidationError = sharedconfig.ValidationError

// ValidationErrorsError is an alias for the shared validation errors wrapper.
type ValidationErrorsError = sharedconfig.ValidationErrorsError

// MailDir returns the path to ~/.auto/mail.
func MailDir() (string, error) {
	autoDir, err := sharedconfig.AutoDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(autoDir, mailDirName), nil
}

// StorePath returns the path to ~/.auto/mail/alpha-store.db.
func StorePath() (string, error) {
	dir, err := MailDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, StoreFileName), nil
}

// FlagsDir returns the path to ~/.auto/mail/alpha-flags.
func FlagsDir() (string, error) {
	dir, err := MailDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, FlagsDirName), nil
}

// MailDirIn returns <home>/.auto/mail for an explicitly supplied home
// directory. The hook path needs the locations without consulting the
// environment, so every path helper has a home-relative twin.
func MailDirIn(home string) string {
	return filepath.Join(home, ".auto", mailDirName)
}

// StorePathIn returns <home>/.auto/mail/alpha-store.db.
func StorePathIn(home string) string {
	return filepath.Join(MailDirIn(home), StoreFileName)
}

// FlagsDirIn returns <home>/.auto/mail/alpha-flags.
func FlagsDirIn(home string) string {
	return filepath.Join(MailDirIn(home), FlagsDirName)
}
