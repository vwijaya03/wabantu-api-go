package main

import "fmt"

// generateHardMCQs adds Mindrift/Tendem-style advanced frontend MCQs (English, hard).
func generateHardMCQs() []mcq {
	var out []mcq
	n := 0
	add := func(q, correct string, opts [4]string, expl string, tags []string, topic string, code string) {
		n++
		out = append(out, mcqItem(q, correct, opts, expl, tags, "hard", fmt.Sprintf("mindrift-hard-%03d", n), code))
	}

	react := []struct {
		q, c, e string
		o       [4]string
		tags    []string
		code    string
	}{
		{
			"A `useEffect` fetches data but updates state after unmount. Best fix?",
			"c",
			"AbortController in effect cleanup prevents setState on unmounted components.",
			[4]string{"Ignore the warning", "Use useLayoutEffect instead", "Abort fetch + guard/cleanup in useEffect", "Move fetch to render"},
			[]string{"react", "hooks"},
			`useEffect(() => {
  fetch("/api").then(r => r.json()).then(setData);
}, []);`,
		},
		{
			"Why can `useEffect(() => { setCount(count + 1); }, [count])` cause problems when incrementing once per mount?",
			"b",
			"Effect runs after count changes, potentially looping if logic is wrong; functional updates are safer.",
			[4]string{"Illegal syntax", "Effect re-runs when count changes — easy to create loops", "useEffect cannot set state", "Strict Mode only issue"},
			[]string{"react", "hooks"},
			"",
		},
		{
			"Hydration mismatch in Next.js often comes from?",
			"d",
			"Server HTML must match first client render — Date.now/random in render breaks hydration.",
			[4]string{"CSS modules", "Using TypeScript", "importing images", "Non-deterministic render output (Date, random, locale)"},
			[]string{"react"},
			"",
		},
		{
			"`useMemo(() => compute(a), [a])` — when is it NOT worth it?",
			"a",
			"Cheap computations cost more to memoize than to recompute.",
			[4]string{"compute is trivially cheap", "compute runs every render without memo", "Child is memoized", "a changes often"},
			[]string{"react", "performance"},
			"",
		},
		{
			"Error boundaries do NOT catch errors in?",
			"d",
			"Event handlers and async code need try/catch; boundaries catch render/lifecycle errors.",
			[4]string{"Child render", "Lifecycle methods", "Constructors of children", "Event handlers and async callbacks"},
			[]string{"react"},
			"",
		},
		{
			"Why use `createPortal` for a modal?",
			"b",
			"Portals escape parent overflow/z-index while keeping React event bubbling.",
			[4]string{"Faster renders", "Render outside parent DOM hierarchy", "Required for SSR", "Replaces state"},
			[]string{"react"},
			"",
		},
		{
			"Uncontrolled file input switched to controlled later causes?",
			"c",
			"File inputs are often uncontrolled; value cannot be set for security reasons.",
			[4]string{"Better performance", "Automatic upload", "Cannot set file input value from state", "Hydration always works"},
			[]string{"react", "html"},
			"",
		},
		{
			"`startTransition` in React 18 marks updates as?",
			"a",
			"Transitions are interruptible lower-priority updates (e.g. filtering a list).",
			[4]string{"Non-urgent / interruptible", "Synchronous blocking", "Server-only", "CSS animations"},
			[]string{"react"},
			"",
		},
		{
			"List item state resets when filtering — likely cause?",
			"b",
			"Index keys cause React to reuse wrong component instances after reorder/filter.",
			[4]string{"Too many divs", "Unstable list keys (index)", "Missing useMemo", "CSS flex"},
			[]string{"react"},
			`items.map((item, i) => <Row key={i} item={item} />)`,
		},
		{
			"Context value `value={{ user, theme }}` recreated each render causes?",
			"d",
			"New object reference triggers all consumers to re-render.",
			[4]string{"Nothing", "SEO issue", "Hydration only", "Unnecessary consumer re-renders"},
			[]string{"react", "performance"},
			"",
		},
	}
	for _, it := range react {
		add(it.q, it.c, it.o, it.e, it.tags, it.tags[0], it.code)
	}

	js := []struct {
		q, c, e string
		o       [4]string
		code    string
	}{
		{
			"Log order: `console.log('A'); Promise.resolve().then(() => console.log('B')); console.log('C');`",
			"b",
			"Sync A, sync C, then microtask B.",
			[4]string{"A B C", "A C B", "B A C", "C B A"},
			"",
		},
		{
			"`async function f(){ await null; console.log(1); } f(); console.log(2);` order?",
			"c",
			"f starts, hits await, yields; sync 2 runs; then microtask 1.",
			[4]string{"1 2", "2 1 only", "2 then 1", "random"},
			"",
		},
		{
			"Classic closure bug: `for (var i=0;i<3;i++) setTimeout(() => console.log(i), 0)` prints?",
			"d",
			"var is function-scoped — all timeouts see i=3.",
			[4]string{"0 1 2", "3 3 3 only once", "undefined", "3 3 3"},
			"",
		},
		{
			"`Object.is(NaN, NaN)` vs `NaN === NaN`?",
			"a",
			"Object.is treats NaN as equal to NaN; === does not.",
			[4]string{"Object.is true, === false", "Both true", "Both false", "=== true"},
			"",
		},
		{
			"Debouncing search input primarily helps with?",
			"c",
			"Waits for pause in typing before firing expensive handler.",
			[4]string{"SEO", "CSS layout", "Reducing API calls while typing", "Memory leaks"},
			"",
		},
		{
			"`fetch` with `credentials: 'include'` requires?",
			"b",
			"Server must respond with proper CORS credentials headers.",
			[4]string{"No CORS", "CORS Access-Control-Allow-Credentials", "GET only", "JSON body"},
			"",
		},
		{
			"Shallow compare in `React.memo` fails when?",
			"d",
			"New object/array props each render break shallow equality.",
			[4]string{"Primitives change", "Strings interned", "Numbers equal", "Props are new object references each render"},
			"",
		},
		{
			"`structuredClone` cannot clone?",
			"a",
			"Functions, symbols, and some DOM nodes are not cloneable.",
			[4]string{"Functions / some built-ins", "Arrays", "Plain objects", "Dates"},
			"",
		},
		{
			"Module scope vs block scope — `let` in `if` block?",
			"c",
			"let is block-scoped to the if block.",
			[4]string{"Global", "Function only", "Block-scoped to if", "Hoisted like var"},
			"",
		},
		{
			"Why prefer `===` over `==` in application code?",
			"b",
			"Avoids coercion surprises (`'' == 0`, `null == undefined`).",
			[4]string{"Faster only", "Avoids type coercion", "Required by TypeScript", "== is deprecated"},
			"",
		},
	}
	for _, it := range js {
		add(it.q, it.c, it.o, it.e, []string{"javascript"}, "javascript", it.code)
	}

	css := []struct {
		q, c, e string
		o       [4]string
	}{
		{
			"Flex child with long text overflows container. Common fix?",
			"b",
			"min-width: 0 on flex child allows shrinking below content intrinsic width. Flex items default min-width: auto preventing shrink.",
			[4]string{"display: block", "min-width: 0 on flex item", "float: left", "position: fixed"},
		},
		{
			"`position: sticky` not working — common cause?",
			"c",
			"Ancestor with overflow hidden/auto can break sticky. Sticky needs scroll container without clipping overflow.",
			[4]string{"Missing z-index only", "Needs display grid only", "Overflow hidden on ancestor", "Requires JavaScript"},
		},
		{
			"Stacking context is created by?",
			"d",
			"positioned + z-index, opacity < 1, transform, filter, etc. New stacking context isolates z-index comparisons.",
			[4]string{"color: red", "margin only", "font-size", "opacity, transform, positioned z-index"},
		},
		{
			"`100vh` on mobile browsers can cause?",
			"a",
			"Mobile URL bar changes visible viewport — dvh/svh are safer. Classic vh larger than visible area on mobile.",
			[4]string{"Content hidden behind browser chrome", "No issue ever", "Grid break only", "Hydration errors"},
		},
		{
			"CSS Grid `minmax(0, 1fr)` vs `1fr` for text truncation?",
			"b",
			"minmax(0,1fr) allows track to shrink below content min size. Default min size is auto (content-based).",
			[4]string{"Same always", "minmax(0,1fr) allows shrinking for ellipsis", "1fr is always smaller", "Only for flex"},
		},
		{
			"`:focus-visible` differs from `:focus` because?",
			"c",
			"focus-visible targets keyboard focus, not every mouse click. Better UX for keyboard vs pointer users.",
			[4]string{"Deprecated", "Only Safari", "Keyboard-like focus without mouse click ring", "Same"},
		},
		{
			"Container queries (`@container`) enable?",
			"d",
			"Style based on parent container size, not viewport. Useful for cards in variable-width sidebars.",
			[4]string{"Server components", "Print only", "SVG only", "Component-level responsive layout"},
		},
		{
			"Specificity: which wins? `.nav .btn.active` vs `#header .btn`",
			"b",
			"#header .btn has ID — higher specificity than two classes. ID selector outweighs multiple classes.",
			[4]string{"First wins", "#header .btn (ID beats classes)", "Second always loses", "Equal — last rule wins only if equal"},
		},
		{
			"`object-fit: cover` on hero image ensures?",
			"a",
			"Image fills box, cropping to preserve aspect ratio. Common for landing hero sections (Tendem-style pages).",
			[4]string{"Fill area, crop excess", "Distort to fit", "Blur background", "Lazy load"},
		},
		{
			"Logical property `margin-inline` helps with?",
			"c",
			"RTL/LTR layouts without separate left/right rules. i18n-friendly layout maintenance.",
			[4]string{"Animation", "Fonts", "RTL/LTR spacing", "Grid only"},
		},
	}
	for _, it := range css {
		add(it.q, it.c, it.o, it.e, []string{"css"}, "css", "")
	}

	html := []struct {
		q, c, e string
		o       [4]string
	}{
		{
			"Icon-only close button needs?",
			"b",
			"Accessible name via aria-label or visually hidden text. Screen readers need name for icon buttons.",
			[4]string{"title only", "aria-label or visible text", "role=button only", "tabindex=-1"},
		},
		{
			"Multiple `<main>` landmarks on one page?",
			"d",
			"Only one main per document for accessibility. Landmark confusion for assistive tech.",
			[4]string{"Best practice", "Required for SEO", "OK if hidden", "Invalid — one main per page"},
		},
		{
			"`aria-live=\"polite\"` is for?",
			"a",
			"Announces dynamic updates without interrupting. Toast/status regions use live regions.",
			[4]string{"Non-interrupting dynamic updates", "Modal focus trap", "Form submit", "Images"},
		},
		{
			"Associating `<label>` with `<input>` best practice?",
			"c",
			"htmlFor/id link increases hit area and screen reader support. Do not rely on placeholder as label.",
			[4]string{"Wrap only, never for", "class name match", "htmlFor + matching id", "placeholder replaces label"},
		},
		{
			"`loading=\"lazy\"` on above-the-fold hero image?",
			"b",
			"Can delay LCP — eager load critical hero images. Mindrift landing pages care about LCP.",
			[4]string{"Always recommended", "Can hurt LCP for hero", "Blocks SEO", "Required"},
		},
		{
			"`<button type=\"button\">` inside form prevents?",
			"a",
			"Default button type in form is submit. Explicit type for non-submit actions.",
			[4]string{"Accidental form submit", "CSS issues", "Hydration", "Caching"},
		},
		{
			"Skip navigation link targets?",
			"d",
			"Keyboard users jump to main content. WCAG bypass block recommendation.",
			[4]string{"Footer only", "Analytics", "CSS file", "Main content landmark"},
		},
		{
			"`autocomplete=\"email\"` on waitlist form helps?",
			"c",
			"Browser autofill improves conversion and UX. Tendem waitlist forms benefit from autofill.",
			[4]string{"Server validation", "CSRF", "Browser autofill", "Routing"},
		},
		{
			"`<dialog>` + `showModal()` provides?",
			"b",
			"Native modal with ::backdrop and focus management basics. Modern alternative to div overlays.",
			[4]string{"React portal only", "Native modal + backdrop", "SEO boost", "Grid layout"},
		},
		{
			"Heading levels should?",
			"a",
			"Logical h1-h6 hierarchy without skipping levels arbitrarily. Document outline for screen readers.",
			[4]string{"Not skip levels logically", "Always use h1 only", "Match font size only", "Be avoided"},
		},
	}
	for _, it := range html {
		add(it.q, it.c, it.o, it.e, []string{"html"}, "html", "")
	}

	// Mindrift / Tendem practical review scenarios
	practical := []struct {
		q, c, e string
		o       [4]string
		tags    []string
	}{
		{
			"AI-generated landing page uses `<div onClick>` for navigation. Main a11y issue?",
			"b",
			"Div is not keyboard-focusable or semantically a link — use `<a href>` or button with keyboard support.",
			[4]string{"Color contrast only", "Not keyboard accessible / wrong semantics", "Missing Tailwind", "Too many images"},
			[]string{"html", "react"},
		},
		{
			"Reviewing AI React code: inline `style={{ marginTop: 20 }}` everywhere. Maintainability issue?",
			"c",
			"Hard to theme/responsive — prefer design tokens, CSS classes, or Tailwind utilities.",
			[4]string{"Faster runtime", "SEO", "Hard to maintain consistent design system", "Illegal in React"},
			[]string{"css", "react"},
		},
		{
			"Waitlist form submits empty email — missing?",
			"a",
			"Client validation + required attribute + accessible error message.",
			[4]string{"Validation and error feedback", "More images", "useEffect", "Portal"},
			[]string{"react", "html"},
		},
		{
			"Hero CTA button works on desktop but not mobile. Likely CSS cause?",
			"d",
			"Overlapping invisible element or pointer-events blocking tap target.",
			[4]string{"Wrong font", "Missing meta viewport only", "useState bug always", "Overlay blocking pointer events / z-index"},
			[]string{"css", "html"},
		},
		{
			"Responsive promo page: fixed width 1200px container. Problem?",
			"b",
			"Horizontal scroll on mobile — use max-width and fluid layout.",
			[4]string{"SEO", "Horizontal overflow on small screens", "React error", "None"},
			[]string{"css"},
		},
		{
			"AI output uses `useEffect` to sync every prop to state. Better pattern?",
			"c",
			"Derive during render or use key to reset — avoid redundant state.",
			[4]string{"More useEffects", "Global variable", "Derive from props or reset with key", "class component"},
			[]string{"react"},
		},
		{
			"Image hero without width/height attributes causes?",
			"a",
			"Cumulative Layout Shift (CLS) when image loads.",
			[4]string{"CLS / layout shift", "Hydration only", "CORS", "Memory leak"},
			[]string{"html", "css"},
		},
		{
			"Form `onSubmit` without `preventDefault`?",
			"d",
			"Full page reload in traditional form post behavior.",
			[4]string{"JSON parse error", "React warning only", "CORS", "Browser full page reload"},
			[]string{"react", "javascript"},
		},
		{
			"Evaluating two AI implementations: one uses index keys, one uses id keys. Pick?",
			"b",
			"Stable id keys preserve state and DOM correctly when list changes.",
			[4]string{"Index — simpler", "Id keys — correct reconciliation", "Random keys", "No keys"},
			[]string{"react"},
		},
		{
			"Mindrift task: refine AI markup. First check for production landing page?",
			"c",
			"Semantic structure, responsive layout, accessible forms, and valid HTML.",
			[4]string{"Bundle size only", "Dark mode", "Semantics, responsive, a11y, valid HTML", "Number of useState calls"},
			[]string{"html", "css", "react"},
		},
	}
	for _, it := range practical {
		add(it.q, it.c, it.o, it.e, it.tags, "mindrift-practical", "")
	}

	return out
}
