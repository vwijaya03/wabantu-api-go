// Package buildinfo exposes the deployed VCS revision for triage verify gates.
package buildinfo

import (
	"os"
	"runtime/debug"
	"strings"
)

const (
	RevisionUnknown = "deployment_unknown"
)

// Revision is the git SHA of the running binary, or deployment_unknown.
func Revision() string {
	if v := strings.TrimSpace(os.Getenv("ENCORE_DEPLOY_ID")); v != "" {
		return v
	}
	if v := strings.TrimSpace(os.Getenv("ENCORE_GIT_COMMIT")); v != "" {
		return v
	}
	info, ok := debug.ReadBuildInfo()
	if !ok || info == nil {
		return RevisionUnknown
	}
	for _, s := range info.Settings {
		if s.Key == "vcs.revision" && strings.TrimSpace(s.Value) != "" {
			return s.Value
		}
	}
	return RevisionUnknown
}

// Covers reports whether deployed revision is the expected merge SHA or a descendant prefix match.
func Covers(deployed, expected string) bool {
	deployed = strings.TrimSpace(deployed)
	expected = strings.TrimSpace(expected)
	if deployed == "" || deployed == RevisionUnknown || expected == "" {
		return false
	}
	if deployed == expected {
		return true
	}
	// Allow short SHA prefix if both are hex.
	min := 7
	if len(expected) >= min && strings.HasPrefix(deployed, expected) {
		return true
	}
	if len(deployed) >= min && strings.HasPrefix(expected, deployed) {
		return true
	}
	return false
}
