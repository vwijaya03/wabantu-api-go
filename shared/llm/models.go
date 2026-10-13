package llm

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

// ModelAllowlist is the set of model ids accepted in config and by providers.
// "" (provider default) is always allowed. Cost policy: cheapest model first.
var ModelAllowlist = map[string][]string{
	"anthropic": {
		"claude-haiku-4-5", "claude-haiku-4-5-20251001",
		"claude-sonnet-4-5", "claude-sonnet-4-5-20250929", "claude-sonnet-4-6",
	},
	"gemini": {
		"gemini-2.5-flash-lite", "gemini-2.5-flash", "gemini-2.5-pro",
	},
	"openai": {
		"text-embedding-3-small",
	},
}

// FunctionProviders limits which providers may serve a function.
// Embedding stays OpenAI (vector dimension is bound to the model).
var FunctionProviders = map[Function][]string{
	FnChat:       {"anthropic", "gemini"},
	FnIntent:     {"anthropic", "gemini"},
	FnOCR:        {"anthropic", "gemini"},
	FnPaymentOCR: {"anthropic", "gemini"},
	FnVision:     {"anthropic", "gemini"},
	FnMemory:     {"anthropic", "gemini"},
	FnEmbedding:  {"openai"},
}

// AllFunctions in stable order.
func AllFunctions() []Function {
	out := make([]Function, 0, len(FunctionProviders))
	for f := range FunctionProviders {
		out = append(out, f)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

var reSafeModel = regexp.MustCompile(`^[a-z0-9][a-z0-9.\-]{0,63}$`)

// ValidateModel checks provider+model together against the allowlist.
func ValidateModel(provider, model string) error {
	list, ok := ModelAllowlist[provider]
	if !ok {
		return fmt.Errorf("llm: unknown provider %q", provider)
	}
	if model == "" {
		return nil
	}
	if !reSafeModel.MatchString(model) {
		return fmt.Errorf("llm: invalid model id %q", TrimForLog(model, 64))
	}
	for _, m := range list {
		if m == model {
			return nil
		}
	}
	return fmt.Errorf("llm: model %q is not allowed for provider %q", model, provider)
}

const (
	maxChainLinks = 3
	maxTimeoutMS  = 60000
	maxErrLog     = 300
	maxErrBodyLog = 200
	maxRetriesCap = 3
)

// ValidateFunctionConfig validates a full chain for a function (used by the admin API).
func ValidateFunctionConfig(fn Function, chain []ChainLink, maxRetries int) error {
	allowed, ok := FunctionProviders[fn]
	if !ok {
		return fmt.Errorf("llm: unknown function %q", fn)
	}
	if len(chain) == 0 || len(chain) > maxChainLinks {
		return fmt.Errorf("llm: chain must have 1..%d links", maxChainLinks)
	}
	if maxRetries < 0 || maxRetries > maxRetriesCap {
		return fmt.Errorf("llm: max_retries must be 0..%d", maxRetriesCap)
	}
	seen := map[string]bool{}
	for i, l := range chain {
		okp := false
		for _, p := range allowed {
			if p == l.Provider {
				okp = true
			}
		}
		if !okp {
			return fmt.Errorf("llm: link %d: provider %q not allowed for function %q", i, l.Provider, fn)
		}
		if err := ValidateModel(l.Provider, l.Model); err != nil {
			return fmt.Errorf("link %d: %w", i, err)
		}
		if l.TimeoutMS < 0 || l.TimeoutMS > maxTimeoutMS {
			return fmt.Errorf("llm: link %d: timeout_ms must be 0..%d", i, maxTimeoutMS)
		}
		key := l.Provider + "|" + l.Model
		if seen[key] {
			return fmt.Errorf("llm: link %d duplicates an earlier link", i)
		}
		seen[key] = true
	}
	return nil
}

// TrimForLog bounds a string for logs (UTF-8 safe) and flattens newlines.
func TrimForLog(s string, max int) string {
	s = strings.ReplaceAll(strings.ReplaceAll(s, "\r", " "), "\n", " ")
	if len(s) <= max {
		return s
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + fmt.Sprintf("…(+%d bytes)", len(s)-cut)
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
