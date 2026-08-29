package main

import "fmt"

func generateMCQsBatch2() []mcq {
	var out []mcq
	n := 0
	add := func(q, correct string, opts [4]string, expl, diff, tag, topic string) {
		n++
		out = append(out, mcqItem(q, correct, opts, expl, []string{tag}, diff, fmt.Sprintf("%s-%02d", topic, n), ""))
	}

	react := []struct {
		q, c string
		o    [4]string
		e, d string
	}{
		{"`useLayoutEffect` differs from `useEffect` because?", "b", [4]string{"Server-only", "Runs synchronously after DOM mutations, before paint", "Cannot cleanup", "Class components only"}, "useLayoutEffect is good for measuring DOM before the user sees flicker.", "medium"},
		{"React Portal (`createPortal`) is useful for?", "a", [4]string{"Global styling", "Rendering a subtree into a DOM node outside the parent (modal)", "Replacing the router", "Memoization"}, "Portal breaks parent overflow/z-index for overlays.", "medium"},
		{"When is `forwardRef` used?", "c", [4]string{"All components", "Class components only", "Parent needs access to the child's DOM ref", "Replacing props"}, "forwardRef passes ref to an element inside the component.", "medium"},
		{"Strict Mode in dev helps find?", "d", [4]string{"CSS bugs", "Network errors", "SEO issues", "Unsafe side effects / double invoke"}, "React 18 Strict Mode double-invokes effects in dev.", "medium"},
		{"Controlled vs uncontrolled input?", "b", [4]string{"Same thing", "Controlled: value from React state", "Uncontrolled is safer", "HTML forms only"}, "Controlled = single source of truth in React state.", "easy"},
		{"When should `key` on a list be stable?", "a", [4]string{"When the list can reorder/filter", "Never", "Production only", "Strings only"}, "Stable keys preserve item identity across renders.", "easy"},
		{"`children` as a function (render prop) is for?", "c", [4]string{"SEO", "Styling", "Delegating rendering to the parent", "Database"}, "Render props pattern shares UI logic.", "hard"},
		{"Errors in event handlers are handled with?", "b", [4]string{"Automatic error boundary", "try/catch or local error state", "Suspense", "Portal"}, "Error boundaries do not catch event handler errors.", "medium"},
		{"`lazy()` + `Suspense` is for?", "d", [4]string{"Global state", "CSS", "Form validation", "Async component code-splitting"}, "lazy loads dynamic components; Suspense shows loading fallback.", "medium"},
		{"React reconciliation refers to?", "a", [4]string{"Virtual DOM diff process", "HTTP cache", "CSS layout", "npm install"}, "The reconciler compares old vs new tree.", "hard"},
		{"`useId` (React 18) is useful for?", "c", [4]string{"Random list keys", "Database UUIDs", "Stable SSR/hydration IDs for a11y", "Routing"}, "useId avoids server/client id mismatch.", "medium"},
		{"Anti-pattern: `useEffect` fetch without cleanup abort?", "b", [4]string{"Best practice", "Race condition on unmount/fast re-fetch", "Required", "Mobile only"}, "AbortController prevents setState on unmounted component.", "hard"},
		{"`memo` + `useCallback` makes most sense when?", "d", [4]string{"Always", "Small files", "Without a profiler", "Expensive child re-renders and stable props"}, "Premature optimization can add complexity.", "hard"},
		{"Fragment with key is written as?", "a", [4]string{"`<React.Fragment key={id}>`", "`<key>`", "`#fragment`", "Not possible"}, "Keyed fragment for list of fragments.", "medium"},
		{"Hydration mismatch is often caused by?", "c", [4]string{"CSS", "useMemo", "Different HTML on server vs client", "CSS imports"}, "Example: Date.now() or random in initial render.", "hard"},
		{"`defaultValue` on uncontrolled input?", "b", [4]string{"Syncs every render", "Initial value only", "Same as value", "Required in TS"}, "defaultValue does not control subsequent updates.", "easy"},
		{"Context value object inline `value={{a,b}}` problem?", "d", [4]string{"None", "Faster", "SEO", "New reference each render triggers consumer re-render"}, "Memoize value object or split context.", "medium"},
		{"`useRef` for storing interval ID because?", "a", [4]string{"Mutable without re-render", "Faster than state", "Hooks rules require it", "SSR"}, "Ref is good for values that don't need to trigger UI update.", "medium"},
	}
	for _, it := range react {
		add(it.q, it.c, it.o, it.e, it.d, "react", "react-v2")
	}

	js := []struct {
		q, c string
		o    [4]string
		e, d string
	}{
		{"`Array.prototype.find` returns?", "b", [4]string{"Index", "First matching element or undefined", "New array", "Boolean"}, "find stops at the first element that passes the predicate.", "easy"},
		{"`Object.freeze` does?", "c", [4]string{"Deep immutable", "Arrays only", "Shallow freeze of properties", "Clone"}, "freeze is shallow — nested objects can still be changed.", "medium"},
		{"`??` (nullish coalescing) vs `||`?", "a", [4]string{"?? only null/undefined", "Same", "|| is stricter", "?? for empty strings"}, "|| treats '' and 0 as falsy; ?? does not.", "medium"},
		{"`Promise.allSettled` is useful when?", "d", [4]string{"One failure cancels all", "Sync loop", "DOM", "You need all promise results even if some reject"}, "allSettled does not short-circuit on rejection.", "medium"},
		{"TDZ (Temporal Dead Zone) relates to?", "b", [4]string{"var", "let/const before declaration", "function", "import"}, "Accessing let/const before the declaration line throws.", "hard"},
		{"`structuredClone` is for?", "c", [4]string{"JSON only", "Shallow copy", "Built-in deep clone (limited)", "Immutable.js"}, "structuredClone deep clones in modern browsers.", "medium"},
		{"Event loop: setTimeout(0) vs Promise.then?", "a", [4]string{".then microtask runs first", "setTimeout always first", "Random", "Parallel"}, "Microtask queue before next macrotask.", "hard"},
		{"`in` operator on object checks?", "b", [4]string{"Value", "Key existence (including prototype chain)", "Type", "Length"}, "Object.hasOwn is safer for own property.", "medium"},
		{"`fetch` credentials 'include'?", "d", [4]string{"No cookies", "POST only", "CORS not needed", "Send cookies cross-origin if server allows"}, "Credentials mode for session cookies.", "hard"},
		{"Debounce vs throttle?", "c", [4]string{"Same", "Throttle waits for idle", "Debounce waits for pause; throttle limits rate", "UI only"}, "Debounce: search input; throttle: scroll.", "medium"},
		{"`Map` vs plain object for keys?", "a", [4]string{"Map allows non-string keys", "Object is always faster", "Map is not iterable", "Same"}, "Map preserves insertion order and arbitrary keys.", "medium"},
		{"`Array.sort` default compares?", "b", [4]string{"Numbers numerically", "Unicode strings", "Random", "Length"}, "Default sort is string — pass compareFn for numbers.", "easy"},
		{"IIFE `(function(){})()` is used for?", "d", [4]string{"Import", "Class", "Hook", "Isolated scope / avoid polluting global"}, "Legacy pattern before modules.", "easy"},
		{"`Symbol` in JS is for?", "c", [4]string{"Math", "CSS", "Unique property keys", "Async"}, "Symbol.uniq for keys that don't collide.", "medium"},
		{"`WeakMap` keys must be?", "a", [4]string{"Objects (garbage collectible)", "Strings", "Numbers", "Symbols only"}, "WeakMap does not prevent GC of key objects.", "hard"},
		{"`try/finally` without catch?", "b", [4]string{"Illegal", "Legal — finally still runs", "Async only", "TS only"}, "finally runs even if return in try.", "medium"},
		{"Template literal tagged function?", "d", [4]string{"CSS only", "SQL injection", "JSON", "Custom processing of string parts"}, "Tagged templates for i18n/styled patterns.", "hard"},
		{"`Object.entries` returns?", "c", [4]string{"Keys only", "Values only", "Array of [key,value] pairs", "Map"}, "entries for iterating enumerable object properties.", "easy"},
	}
	for _, it := range js {
		add(it.q, it.c, it.o, it.e, it.d, "javascript", "js-v2")
	}

	css := []struct {
		q, c string
		o    [4]string
		e, d string
	}{
		{"`display: grid` vs `flex`?", "a", [4]string{"Grid is two-dimensional, flex is one main dimension", "Same", "Flex for tables", "Grid is not responsive"}, "Choose flex for single row/column; grid for 2D layout.", "easy"},
		{"`fr` unit in CSS Grid?", "b", [4]string{"Font relative", "Fraction of remaining track space", "Frame rate", "Rem"}, "1fr = proportional share of available space.", "medium"},
		{"`position: sticky` needs?", "c", [4]string{"z-index only", "fixed parent", "Offset + scroll ancestor + no overflow:hidden parent", "display flex"}, "Parent overflow:hidden can disable sticky.", "medium"},
		{"`clamp(min, pref, max)` is for?", "d", [4]string{"Animation", "Grid", "Print", "Bounded fluid typography/spacing"}, "clamp is responsive with min/max bounds.", "medium"},
		{"Specificity: inline style vs #id?", "b", [4]string{"#id wins", "Inline wins unless !important id", "Same", "class wins"}, "Inline 1,0,0,0 — id 0,1,0,0.", "medium"},
		{"`aspect-ratio` property?", "a", [4]string{"Maintains width/height ratio", "Font size", "Grid gap", "Flex order"}, "Useful for responsive video/cards.", "easy"},
		{"`@layer` in CSS is for?", "c", [4]string{"Animation", "Font", "Controlling cascade layer order", "Media query"}, "Layers control precedence without specificity wars.", "hard"},
		{"`contain: layout` helps?", "d", [4]string{"SEO", "Font", "Color", "Layout isolation for subtree performance"}, "Contain limits reflow to the subtree.", "hard"},
		{"`logical` properties (`margin-inline`)?", "b", [4]string{"Print only", "Follow writing mode LTR/RTL", "Grid only", "Deprecated"}, "Logical props for i18n layout.", "medium"},
		{"`prefers-reduced-motion`?", "a", [4]string{"A11y media query to reduce animation", "Dark mode", "Print", "Hover"}, "Respect user motion sensitivity preferences.", "easy"},
		{"Flex `gap` vs margin on items?", "c", [4]string{"Margin is more modern", "Same", "gap spaces items without margin collapse", "gap is grid only"}, "gap is cleaner for flex/grid spacing.", "easy"},
		{"`object-fit: cover` on img?", "b", [4]string{"Stretch distort", "Crop content to box while keeping ratio", "Contain blur", "SVG only"}, "cover fills container with center crop.", "easy"},
		{"`::before` pseudo-element default `display`?", "d", [4]string{"Always block", "Always inline", "none", "inline — needs content & often set to block"}, "Needs content property to appear.", "medium"},
		{"`minmax(200px, 1fr)` in grid?", "a", [4]string{"Track min 200px max remaining", "Fixed 200", "Max only", "Flex only"}, "minmax for flexible track sizing.", "medium"},
		{"`will-change` anti-pattern when?", "c", [4]string{"Small hover", "Transform animation", "Applied permanently to many elements", "One-time GPU layer"}, "Overuse of will-change wastes memory.", "hard"},
		{"`color-scheme` on root?", "b", [4]string{"Font color", "Hints browser dark/light UI chrome", "Grid", "Flex"}, "Affects scrollbar/form native theming.", "medium"},
		{"`subgrid` (CSS Grid)?", "d", [4]string{"Flex feature", "Deprecated", "Table only", "Child grid inherits parent tracks"}, "subgrid aligns nested grid to parent tracks.", "hard"},
	}
	for _, it := range css {
		add(it.q, it.c, it.o, it.e, it.d, "css", "css-v2")
	}

	html := []struct {
		q, c string
		o    [4]string
		e, d string
	}{
		{"`<button>` inside `<form>` default type?", "b", [4]string{"button", "submit", "reset", "menu"}, "Without an explicit type, button = submit.", "easy"},
		{"`<label for>` must match?", "a", [4]string{"Related input id", "name", "class", "type"}, "for/id connects label to control.", "easy"},
		{"Native `<dialog>` element?", "c", [4]string{"Does not exist", "Safari only", "HTML modal dialog with showModal()", "React only"}, "dialog + ::backdrop for native modal.", "medium"},
		{"`loading=\"lazy\"` on img?", "b", [4]string{"SEO", "Defer load until near viewport", "CDN", "Blur"}, "Native image lazy loading.", "easy"},
		{"Landmark `<main>` should?", "d", [4]string{"Many per page", "In footer", "In nav", "One per page for main content"}, "One main landmark per document.", "easy"},
		{"`aria-expanded` on accordion?", "a", [4]string{"Open/closed state for AT", "Styling", "Focus", "Tab order"}, "Screen reader knows panel is open.", "medium"},
		{"`<input type=\"number\">` caveat?", "c", [4]string{"Always integer", "No step", "Spinner & locale quirks — extra validation needed", "Not on mobile"}, "number input is not a substitute for business validation.", "medium"},
		{"`<details>/<summary>` for?", "b", [4]string{"Modal", "Disclosure widget without JS", "Table", "Form"}, "Native expand/collapse content.", "easy"},
		{"`tabindex=\"0\"`?", "d", [4]string{"Remove from tab order", "Mouse only", "Negative focus trap", "Enter natural tab order"}, "tabindex -1 programmatic focus only.", "medium"},
		{"`role=\"alert\"`?", "a", [4]string{"Urgent live region for errors", "Button", "Link", "Heading"}, "Form error messages should use role alert.", "easy"},
		{"`<meta viewport>` mobile?", "b", [4]string{"SEO", "width=device-width initial-scale=1", "HTTPS", "PWA"}, "Viewport meta for responsive mobile.", "easy"},
		{"`<picture>` + `<source>` for?", "c", [4]string{"Video", "Font", "Art direction / responsive image formats", "SVG"}, "picture for responsive images art direction.", "medium"},
		{"Skip link accessibility?", "d", [4]string{"SEO", "CSS", "Analytics", "Link to main content for keyboard users"}, "Skip nav to main content.", "medium"},
		{"`autocomplete` attribute on form?", "a", [4]string{"Help browser refill fields correctly", "Validation", "CSRF", "Routing"}, "autocomplete=\"email\" etc. for UX.", "easy"},
		{"`<fieldset>` + `<legend>`?", "b", [4]string{"Table", "Accessible form group with title", "Modal", "Grid"}, "fieldset groups radio/checkbox.", "easy"},
		{"`hidden` attribute vs `display:none` CSS?", "c", [4]string{"Always same", "hidden is not semantic", "hidden must not be shown & not relevant to AT", "hidden for SEO"}, "hidden=until-found in newer HTML for find-in-page.", "medium"},
		{"`<template>` content?", "d", [4]string{"Renders immediately", "Main SEO", "SSR only", "Inert until cloned into DOM via JS"}, "template for inactive fragments.", "hard"},
	}
	for _, it := range html {
		add(it.q, it.c, it.o, it.e, it.d, "html", "html-v2")
	}

	return out
}

func generateBuildsBatch2() []buildTask {
	specs := []struct {
		title, field, diff string
		validate           string
	}{
		{"UsernameForm", "username", "easy", "username min 3 characters"},
		{"OTPForm", "otpCode", "medium", "OTP code min 6 digits"},
		{"PinForm", "pin", "medium", "PIN min 4 digits"},
		{"BioForm", "bio", "easy", "bio max 120 characters"},
		{"WebsiteForm", "siteUrl", "medium", "URL must contain https"},
		{"AgeForm", "age", "easy", "age required"},
		{"ZipCodeForm", "zipCode", "medium", "zip code min 5 digits"},
		{"TeamNameForm", "teamName", "easy", "team name required"},
		{"TaglineForm", "tagline", "medium", "tagline max 80 characters"},
		{"BudgetForm", "budget", "hard", "budget required"},
		{"DeadlineForm", "dueDate", "medium", "date required"},
		{"PriorityForm", "priority", "easy", "priority required"},
		{"ChannelForm", "channel", "medium", "channel required"},
		{"LocaleForm", "locale", "easy", "locale required"},
		{"NicknameForm", "nickname", "medium", "nickname min 3 characters"},
	}

	var out []buildTask
	for _, s := range specs {
		cmp := s.title
		testID := toTestID(cmp)
		validation := buildValidationBlock(s.validate)
		out = append(out, buildTask{
			Title:               cmp,
			Family:              "form",
			SpecMarkdown:        buildSpecMarkdown(cmp, s.field, s.validate),
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
			SolutionExplanation: "Controlled input, validation per instructions, error with role=alert, onSubmit called when valid.",
			RubricJSON:          buildRubric,
			TestCases:           buildAssertions(s.field, s.validate, testID),
			BestPractices:       []string{"Controlled components", "Validate before submit", "Label htmlFor for a11y", "preventDefault on form submit"},
			CommonMistakes:      []string{"Forgot preventDefault", "Uncontrolled input", "Error not displayed"},
			LearningObjective:   fmt.Sprintf("React Form — %s", s.title),
			Difficulty:          s.diff,
			Points:              40,
		})
	}
	return out
}
