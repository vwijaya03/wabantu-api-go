package llm

import (
	"regexp"
	"strings"
)

var (
	reDateSuffix = regexp.MustCompile(`-(\d{8}|latest)$`)
	reClaudeNew  = regexp.MustCompile(`^claude-(haiku|sonnet|opus)-(\d+)(?:-(\d{1,2}))?$`)
	reClaudeOld  = regexp.MustCompile(`^claude-(\d+)(?:-(\d{1,2}))?-(haiku|sonnet|opus)$`)
	reGemini     = regexp.MustCompile(`^(gemini-\d+(?:\.\d+)?-(?:flash-lite|flash|pro))`)
)

// ModelFamily maps a full model id to a stable alias for reports/usage aggregation;
// the full id should still be stored. Unknown ids are returned lower-cased unchanged.
//
//	claude-haiku-4-5-20251001 → haiku-4.5    claude-3-5-haiku-20241022 → haiku-3.5
//	claude-sonnet-4-5         → sonnet-4.5   gemini-2.5-flash-preview-05-20 → gemini-2.5-flash
func ModelFamily(id string) string {
	s := strings.ToLower(strings.TrimSpace(id))
	if s == "" {
		return ""
	}
	s = strings.TrimPrefix(s, "models/")
	base := reDateSuffix.ReplaceAllString(s, "")
	ver := func(maj, min string) string {
		if min == "" {
			return maj
		}
		return maj + "." + min
	}
	if m := reClaudeNew.FindStringSubmatch(base); m != nil {
		return m[1] + "-" + ver(m[2], m[3])
	}
	if m := reClaudeOld.FindStringSubmatch(base); m != nil {
		return m[3] + "-" + ver(m[1], m[2])
	}
	if m := reGemini.FindStringSubmatch(base); m != nil {
		return m[1]
	}
	return s
}
