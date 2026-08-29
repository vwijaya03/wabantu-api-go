package main

import (
	"encoding/json"
	"fmt"
)

func generateHardBuilds() []buildTask {
	specs := []struct {
		title, field, diff string
		validate           string
	}{
		{"PromoCodeForm", "promoCode", "hard", "promo code min 6 characters"},
		{"WaitlistConsentForm", "email", "hard", "email must contain @"},
		{"PasswordStrengthForm", "password", "hard", "password min 8 characters"},
		{"IntlPhoneForm", "phone", "hard", "phone min 10 digits"},
		{"CompanySiteForm", "website", "hard", "URL must contain https"},
		{"HeroCTAEmailForm", "email", "hard", "email must contain @"},
		{"LandingZipForm", "zipCode", "hard", "zip code min 5 digits"},
		{"SearchDebouncedForm", "query", "hard", "query min 3 characters"},
		{"NewsletterTaglineForm", "tagline", "hard", "tagline max 80 characters"},
		{"BetaReasonForm", "reason", "hard", "reason min 20 characters"},
	}
	var out []buildTask
	for _, s := range specs {
		cmp := s.title
		testID := toTestID(cmp)
		validation := buildValidationBlock(s.validate)
		out = append(out, buildTask{
			Title:        cmp,
			Family:       "form",
			SpecMarkdown: buildSpecMarkdown(cmp, s.field, s.validate),
			StarterCode: fmt.Sprintf(`import { useState } from "react";

export function %s({ onSubmit }) {
  // TODO: controlled %s + validation
  return (
    <form data-testid="%s">
      <input name="%s" placeholder="%s" />
      <button type="submit">Submit</button>
    </form>
  );
}
`, cmp, s.field, testID, s.field, s.field),
			SolutionCode: fmt.Sprintf(`import { useState } from "react";

export function %s({ onSubmit }) {
  const [value, setValue] = useState("");
  const [error, setError] = useState("");

  function handleSubmit(e) {
    e.preventDefault();
%s
    setError("");
    onSubmit?.({ %s: value });
  }

  return (
    <form data-testid="%s" onSubmit={handleSubmit}>
      <label htmlFor="%s">%s</label>
      <input
        id="%s"
        name="%s"
        value={value}
        onChange={(e) => setValue(e.target.value)}
      />
      {error && <p role="alert">{error}</p>}
      <button type="submit">Submit</button>
    </form>
  );
}
`, cmp, validation, s.field, testID, s.field, s.field, s.field, s.field),
			SolutionExplanation: "Controlled input, validation per instructions, error with role=alert, onSubmit when valid.",
			RubricJSON:          buildRubric,
			TestCases:           buildAssertions(s.field, s.validate, testID),
			BestPractices:       []string{"Controlled components", "Validate before submit", "Label htmlFor for a11y", "preventDefault on form submit"},
			CommonMistakes:      []string{"Forgot preventDefault", "Uncontrolled input", "No error message"},
			LearningObjective:   fmt.Sprintf("React form — %s (hard)", s.title),
			Difficulty:          s.diff,
			Points:              40,
		})
	}
	return out
}

func generateHardDebugs() []debugTask {
	specs := []struct {
		title, bug, cause, fix string
		broken, fixed          string
		kind                   int
	}{
		{
			"Hero Effect Loop",
			"Browser freezes after Hero mounts.",
			"useEffect without a dependency array triggers setState on every render.",
			"Add the correct dependency array or move logic to an event handler.",
			`export function Hero({ title }) {
  const [count, setCount] = useState(0);
  useEffect(() => {
    setCount((c) => c + 1);
  });
  return <section data-testid="hero"><h1>{title}</h1><p>{count}</p></section>;
}`,
			`export function Hero({ title }) {
  const [count, setCount] = useState(0);
  useEffect(() => {
    setCount(1);
  }, []);
  return <section data-testid="hero"><h1>{title}</h1><p>{count}</p></section>;
}`,
			3,
		},
		{
			"Hero Conditional Hook",
			"Rules of Hooks error when showDetails is false.",
			"useState is called inside a conditional branch.",
			"Move all hooks to the top level of the component.",
			`export function Hero({ title, showDetails }) {
  if (showDetails) {
    const [detail, setDetail] = useState("");
    return <section data-testid="hero"><h1>{title}</h1><input value={detail} onChange={(e) => setDetail(e.target.value)} /></section>;
  }
  return <section data-testid="hero"><h1>{title}</h1></section>;
}`,
			`export function Hero({ title, showDetails }) {
  const [detail, setDetail] = useState("");
  return (
    <section data-testid="hero">
      <h1>{title}</h1>
      {showDetails && <input value={detail} onChange={(e) => setDetail(e.target.value)} />}
    </section>
  );
}`,
			5,
		},
		{
			"Hero Infinite Render",
			"Page hangs when Hero mounts.",
			"setState is called directly in the render body.",
			"Remove setState from render; use an event handler or useEffect with correct deps.",
			`export function Hero({ title }) {
  const [n, setN] = useState(0);
  setN(n + 1);
  return <section data-testid="hero"><h1>{title}</h1><p>{n}</p></section>;
}`,
			`export function Hero({ title }) {
  const [n, setN] = useState(0);
  return (
    <section data-testid="hero">
      <h1>{title}</h1>
      <p>{n}</p>
      <button type="button" onClick={() => setN((x) => x + 1)}>+</button>
    </section>
  );
}`,
			0,
		},
		{
			"Hero Stale Props",
			"Subtitle does not update when the prop changes.",
			"State is initialized from props only once without syncing.",
			"Render directly from props or sync with useEffect when the prop changes.",
			`export function Hero({ subtitle }) {
  const [text, setText] = useState(subtitle);
  return <section data-testid="hero"><p>{text}</p></section>;
}`,
			`export function Hero({ subtitle }) {
  return <section data-testid="hero"><p>{subtitle}</p></section>;
}`,
			1,
		},
		{
			"Hero Missing Key",
			"CTA list order is wrong after filter.",
			"Using index as key causes the reconciler to incorrectly reuse DOM.",
			"Use a stable id from the data as the key.",
			`export function Hero({ items }) {
  return (
    <section data-testid="hero">
      {items.map((it, i) => <span key={i}>{it.label}</span>)}
    </section>
  );
}`,
			`export function Hero({ items }) {
  return (
    <section data-testid="hero">
      {items.map((it) => <span key={it.id}>{it.label}</span>)}
    </section>
  );
}`,
			2,
		},
	}
	var out []debugTask
	for i := 0; i < 15; i++ {
		b := specs[i%len(specs)]
		title := fmt.Sprintf("%s (Hard) #%02d", b.title, i+1)
		out = append(out, debugTask{
			Title:             title,
			Family:            "hero",
			BrokenCode:        prependImport(b.broken),
			SolutionCode:      prependImport(b.fixed),
			BugDescription:    debugSpecMarkdown(title, b.bug, debugHintExtended(b.kind)),
			RootCause:         b.cause,
			FixExplanation:    b.fix,
			TestCases:         debugAssertionsExtended(b.kind),
			BestPractices:     []string{"Rules of Hooks", "Stable list keys", "Never setState during render"},
			CommonMistakes:    []string{"Patching symptoms without understanding render cycle", "Removing state the UI still needs"},
			LearningObjective: fmt.Sprintf("Debug React Hero — Mindrift hard %02d", i+1),
			Difficulty:        "hard",
			Points:            35,
		})
	}
	return out
}

func generateHardBlueprints() []blueprintSeed {
	foci := []struct {
		title string
		tags  []string
	}{
		{"Hard 01 — React Effects & Cleanup", []string{"react", "javascript"}},
		{"Hard 02 — Stale Closures", []string{"react", "javascript"}},
		{"Hard 03 — Hydration & SSR", []string{"react", "html"}},
		{"Hard 04 — Performance & Memo", []string{"react", "javascript"}},
		{"Hard 05 — Context & Re-renders", []string{"react"}},
		{"Hard 06 — Lists & Keys", []string{"react"}},
		{"Hard 07 — Event Loop & Async", []string{"javascript"}},
		{"Hard 08 — Promises & Fetch", []string{"javascript"}},
		{"Hard 09 — Closures & Scope", []string{"javascript"}},
		{"Hard 10 — Type Coercion Traps", []string{"javascript"}},
		{"Hard 11 — CSS Flex Edge Cases", []string{"css"}},
		{"Hard 12 — Grid & Layout", []string{"css"}},
		{"Hard 13 — Stacking & z-index", []string{"css"}},
		{"Hard 14 — Responsive & Viewport", []string{"css", "html"}},
		{"Hard 15 — Specificity Wars", []string{"css"}},
		{"Hard 16 — Semantic HTML", []string{"html"}},
		{"Hard 17 — ARIA & Accessibility", []string{"html", "react"}},
		{"Hard 18 — Forms & Validation", []string{"html", "react"}},
		{"Hard 19 — Landing Page LCP/CLS", []string{"css", "html"}},
		{"Hard 20 — Waitlist Forms (Tendem)", []string{"react", "html"}},
		{"Hard 21 — AI Code Review (Mindrift)", []string{"react", "javascript", "css"}},
		{"Hard 22 — Hero Section Debug", []string{"react", "css"}},
		{"Hard 23 — Portals & Modals", []string{"react"}},
		{"Hard 24 — Controlled Inputs", []string{"react"}},
		{"Hard 25 — Error Handling UI", []string{"react", "javascript"}},
		{"Hard 26 — Debounce & Search UX", []string{"javascript", "react"}},
		{"Hard 27 — CORS & Credentials", []string{"javascript"}},
		{"Hard 28 — Tailwind/CSS Mental Model", []string{"css"}},
		{"Hard 29 — Conversion UX Patterns", []string{"html", "css", "react"}},
		{"Hard 30 — Full Frontend Hard Mix", []string{"react", "javascript", "css", "html"}},
	}
	var out []blueprintSeed
	for i, f := range foci {
		out = append(out, blueprintSeed{
			Slug:   fmt.Sprintf("tendem-hard-%02d", i+1),
			Title:  fmt.Sprintf("Tendem Frontend Developer — %s", f.title),
			Config: hardBlueprintConfigJSON(f.tags),
		})
	}
	return out
}

func hardBlueprintConfigJSON(tags []string) json.RawMessage {
	tagsJSON, _ := json.Marshal(tags)
	cfg := fmt.Sprintf(`{
  "sections": [
    {"type": "mcq", "count": 5, "timeLimitMinutes": 40, "tags": %s, "difficulty": "hard"},
    {"type": "react_build", "count": 1, "timeLimitMinutes": 35, "componentFamily": "form", "difficulty": "hard"},
    {"type": "react_debug", "count": 1, "timeLimitMinutes": 23, "componentFamily": "hero", "difficulty": "hard"}
  ],
  "totalTimeLimitMinutes": 98,
  "proctoring": {"maxBlurEvents": 3, "warnOnPaste": true, "blockPasteInEditor": true}
}`, string(tagsJSON))
	return json.RawMessage(cfg)
}
