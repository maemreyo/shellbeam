package config

import (
	"fmt"
	"unicode"
	"unicode/utf8"

	workspace "github.com/maemreyo/shellbeam/internal/core/workspace"
)

const maxGitIdentityProfileNameBytes = 128

func (c Config) ValidateGitIdentityProfiles() error {
	if len(c.GitProfiles) > 32 || len(c.GitRepositoryProfiles) > 256 || len(c.GitWorkspaceProfiles) > 256 {
		return fmt.Errorf("too many Git identity profiles or bindings")
	}
	for name, profile := range c.GitProfiles {
		if !validGitIdentityProfileName(name) {
			return fmt.Errorf("invalid git profile name")
		}
		if err := profile.Validate(); err != nil {
			return fmt.Errorf("git profile %q: %w", name, err)
		}
	}
	for key, profileName := range c.GitRepositoryProfiles {
		if _, err := workspace.ParseRepositoryID(key); err != nil || !validGitIdentityProfileName(profileName) {
			return fmt.Errorf("invalid repository profile binding")
		}
		if _, ok := c.GitProfiles[profileName]; !ok {
			return fmt.Errorf("unknown git profile %q", profileName)
		}
	}
	for key, profileName := range c.GitWorkspaceProfiles {
		if _, err := workspace.ParseWorkspaceID(key); err != nil || !validGitIdentityProfileName(profileName) {
			return fmt.Errorf("invalid workspace profile binding")
		}
		if _, ok := c.GitProfiles[profileName]; !ok {
			return fmt.Errorf("unknown git profile %q", profileName)
		}
	}
	return nil
}

func validGitIdentityProfileName(value string) bool {
	if value == "" || len(value) > maxGitIdentityProfileNameBytes || !utf8.ValidString(value) {
		return false
	}
	for _, r := range value {
		if r == 0 || unicode.IsControl(r) {
			return false
		}
	}
	return true
}
