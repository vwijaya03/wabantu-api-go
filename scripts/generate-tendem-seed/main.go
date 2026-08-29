// Generate Tendem-style codesim seed files (30 sample tests worth of bank content).
//
// Usage: go run scripts/generate-tendem-seed/main.go
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	root := filepath.Join("codesim", "seed")
	if err := os.MkdirAll(root, 0o755); err != nil {
		fatal(err)
	}
	mcq := generateMCQs()
	mcqBatch2 := generateMCQsBatch2()
	mcqHard := generateHardMCQs()
	build := append(append(generateBuilds(), generateBuildsBatch2()...), generateHardBuilds()...)
	debug := append(generateDebugs(), generateHardDebugs()...)
	blueprints := generateBlueprints()
	blueprintsHard := generateHardBlueprints()

	writeJSON(filepath.Join(root, "tendem_mcq.json"), mcq)
	writeJSON(filepath.Join(root, "tendem_mcq_batch2.json"), mcqBatch2)
	writeJSON(filepath.Join(root, "tendem_mcq_hard.json"), mcqHard)
	writeJSON(filepath.Join(root, "tendem_build.json"), build)
	writeJSON(filepath.Join(root, "tendem_debug.json"), debug)
	writeJSON(filepath.Join(root, "tendem_blueprints.json"), blueprints)
	writeJSON(filepath.Join(root, "tendem_blueprints_hard.json"), blueprintsHard)

	fmt.Printf("wrote %d + %d + %d MCQs, %d builds, %d debugs, %d + %d blueprints\n",
		len(mcq), len(mcqBatch2), len(mcqHard), len(build), len(debug), len(blueprints), len(blueprintsHard))
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}

func writeJSON(path string, v any) {
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		fatal(err)
	}
	fmt.Println("wrote", path)
}

type mcq struct {
	Question          string            `json:"question"`
	CodeSnippet       string            `json:"code_snippet,omitempty"`
	Choices           []choice          `json:"choices"`
	CorrectID         string            `json:"correct_id"`
	Explanation       string            `json:"explanation"`
	WrongExplanations map[string]string `json:"wrong_explanations"`
	BestPractices     []string          `json:"best_practices"`
	LearningObjective string            `json:"learning_objective"`
	Points            int               `json:"points"`
	Tags              []string          `json:"tags"`
	Difficulty        string            `json:"difficulty"`
	Topic             string            `json:"topic"`
}

type choice struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

func stdChoices(a, b, c, d, correct string) ([]choice, map[string]string, string) {
	choices := []choice{
		{ID: "a", Text: a},
		{ID: "b", Text: b},
		{ID: "c", Text: c},
		{ID: "d", Text: d},
	}
	wrong := map[string]string{}
	for _, ch := range choices {
		if ch.ID != correct {
			wrong[ch.ID] = fmt.Sprintf("Option %q is not the best answer for this question.", ch.Text)
		}
	}
	return choices, wrong, correct
}

func mcqItem(q, correct string, opts [4]string, expl string, tags []string, diff, topic string, code string) mcq {
	ch, wrong, cid := stdChoices(opts[0], opts[1], opts[2], opts[3], correct)
	return mcq{
		Question:          q,
		CodeSnippet:       code,
		Choices:           ch,
		CorrectID:         cid,
		Explanation:       expl,
		WrongExplanations: wrong,
		BestPractices:     []string{"Read the question and snippet carefully", "Eliminate clearly wrong options", "Connect concepts to real UI scenarios"},
		LearningObjective: topic,
		Points:            10,
		Tags:              tags,
		Difficulty:        diff,
		Topic:             topic,
	}
}

func generateMCQs() []mcq {
	var out []mcq

	reactEasy := []struct{ q, c string; o [4]string; e string }{
		{"What is React's main purpose?", "b", [4]string{"Database ORM", "Building component-based UI", "Package manager", "CSS preprocessor"}, "React focuses on the view layer with reusable components."},
		{"Which file extension is common for React components?", "a", [4]string{".jsx / .tsx", ".vue", ".svelte", ".angular"}, "JSX/TSX is the common syntax in the React ecosystem."},
		{"What does the `children` prop contain in React?", "c", [4]string{"Only strings", "Only arrays", "Nested content rendered inside the component", "Only img elements"}, "children is a slot for content rendered inside the component."},
		{"Correct way to render a list in JSX?", "b", [4]string{"items.forEach", "items.map + key", "items.filter only", "while loop in JSX"}, "map returns an array of elements; key helps the reconciler."},
		{"Default props behavior in modern function components?", "a", [4]string{"Default parameter / default value destructuring", "Class components only", "Not supported", "Redux only"}, "Default values can be set in destructured props parameters."},
	}
	for _, it := range reactEasy {
		out = append(out, mcqItem(it.q, it.c, it.o, it.e, []string{"react"}, "easy", "react-basics", ""))
	}

	jsEasy := []struct{ q, c string; o [4]string; e string }{
		{"Result of `[] == ![]` in JavaScript?", "b", [4]string{"true", "false", "undefined", "throw"}, "Coercion makes loose equality confusing — use ===."},
		{"`const` for an object means?", "c", [4]string{"Object is immutable", "Cannot reassign binding, properties can change", "Block scope in functions only", "Same as Object.freeze"}, "const prevents reassigning the variable, not deep freezing."},
		{"Spread operator `{...obj}` is used for?", "a", [4]string{"Shallow copy / merge object", "Automatic deep clone", "Arrays only", "Deleting keys"}, "Spread creates a shallow copy of enumerable properties."},
		{"`===` compared to `==`?", "d", [4]string{"Exactly the same", "== is stricter", "=== performs coercion", "=== without type coercion"}, "Strict equality does not perform type coercion."},
		{"What is hoisting in JS?", "b", [4]string{"CSS technique", "Declarations lifted to top of scope (var/function)", "React feature", "Bundler optimization"}, "var and function declarations are hoisted; let/const have TDZ."},
	}
	for _, it := range jsEasy {
		out = append(out, mcqItem(it.q, it.c, it.o, it.e, []string{"javascript"}, "easy", "js-basics", ""))
	}

	cssEasy := []struct{ q, c string; o [4]string; e string }{
		{"`display: flex` enables?", "a", [4]string{"Flexbox layout", "Grid layout", "Block formatting only", "Table layout"}, "Flexbox is for one-dimensional layout."},
		{"`box-sizing: border-box`?", "b", [4]string{"Padding outside width", "Width includes padding & border", "Margins only", "Removes border"}, "border-box makes sizing more predictable."},
		{"Unit relative to element font-size?", "c", [4]string{"px", "vh", "em", "cm"}, "em is relative to the element's font-size (or parent for font)."},
		{"Pseudo-class for hover?", "a", [4]string{":hover", "::hover", ":hover()", "@hover"}, ":hover is an interaction pseudo-class."},
		{"`position: absolute` is relative to?", "d", [4]string{"Always viewport", "Always body", "First parent", "Nearest positioned ancestor"}, "Absolute positioning is relative to a positioned ancestor."},
	}
	for _, it := range cssEasy {
		out = append(out, mcqItem(it.q, it.c, it.o, it.e, []string{"css"}, "easy", "css-basics", ""))
	}

	// Medium batch — mix react/js/css with code snippets
	medium := []mcq{
		mcqItem(
			"What does console.log output after clicking the button once?",
			"b",
			[4]string{"0", "1", "2", "undefined"},
			"Functional update `setCount(c => c+1)` is safe; here onClick increments once per click.",
			[]string{"react", "hooks"},
			"medium",
			"react-state",
			"function Counter() {\n  const [count, setCount] = useState(0);\n  return <button onClick={() => setCount(c => c + 1)}>{count}</button>;\n}",
		),
		mcqItem(
			"Main problem with this useEffect?",
			"c",
			[4]string{"No cleanup return", "Wrong dependency array — infinite loop risk", "useState is invalid", "JSX is invalid"},
			"Effect without a dependency array runs every render; setState inside can loop.",
			[]string{"react", "hooks"},
			"medium",
			"react-useeffect",
			"useEffect(() => {\n  setLoading(true);\n  fetchData();\n});",
		),
		mcqItem(
			"Promise chain: correct log order?",
			"a",
			[4]string{"start → microtask → end", "end → start → microtask", "microtask → end → start", "random"},
			"Microtask (.then) runs before the next macrotask after sync code.",
			[]string{"javascript"},
			"medium",
			"js-async",
			"console.log('start');\nPromise.resolve().then(() => console.log('microtask'));\nconsole.log('end');",
		),
		mcqItem(
			"Flex child `flex: 1` generally means?",
			"b",
			[4]string{"flex-grow: 0", "flex-grow: 1, shrink: 1, basis: 0%", "width 100% only", "display block"},
			"Shorthand flex:1 equals grow 1 and basis 0 — child fills remaining space.",
			[]string{"css"},
			"medium",
			"css-flexbox",
			".row { display: flex; }\n.item { flex: 1; }",
		),
		mcqItem(
			"a11y attribute linking label to input?",
			"a",
			[4]string{"htmlFor + id", "class + id", "role=label", "tabIndex only"},
			"htmlFor on label must match input id for screen readers.",
			[]string{"html", "react"},
			"medium",
			"html-a11y",
			"<label htmlFor=\"email\">Email</label>\n<input id=\"email\" type=\"email\" />",
		),
	}
	out = append(out, medium...)

	// Generate more medium/hard programmatically
	topics := []struct {
		tag, topic, diff string
		questions        []struct{ q, c string; o [4]string; e string }
	}{
		{"react", "react-memo", "medium", []struct{ q, c string; o [4]string; e string }{
			{"When does `React.memo` help?", "b", [4]string{"Always", "Child re-renders often with same props", "Class components only", "For fetching data"}, "memo skips render if props are shallow equal."},
			{"`useMemo` is used for?", "c", [4]string{"Side effects", "Caching expensive computation results", "DOM refs", "Routing"}, "useMemo caches function results, not side effects."},
			{"Context API is good for?", "a", [4]string{"Global data that rarely changes (theme, locale)", "All app state", "Always replaces Redux", "Fetch API"}, "Context is for prop drilling, not all state."},
			{"Error boundary catches?", "d", [4]string{"Event handler errors", "Async errors automatically", "All errors", "Render errors in subtree"}, "Error boundaries don't catch event handler / async without wrapper."},
			{"Fragment `<></>` is useful for?", "b", [4]string{"Styling", "Return multiple nodes without DOM wrapper", "Keys on lists", "Portals"}, "Fragment avoids extra div wrappers."},
		}},
		{"javascript", "js-closure", "medium", []struct{ q, c string; o [4]string; e string }{
			{"Closure is?", "a", [4]string{"Function accessing lexical scope variables", "CSS module", "React hook", "HTTP cache"}, "Closure = function + reference to outer scope."},
			{"`async/await` is equivalent to?", "c", [4]string{"Callback hell only", "setTimeout", "Promise syntax sugar", "Web Worker"}, "async function returns a Promise."},
			{"Destructuring `const {a} = obj`?", "b", [4]string{"Mutates object", "Extracts property a", "Deep clone", "JSON parse"}, "Destructuring assignment for objects/arrays."},
			{"`map` vs `forEach`?", "d", [4]string{"Same", "forEach returns new array", "map for side effects", "map returns new array, forEach does not"}, "map transforms; forEach for side effects."},
			{"Optional chaining `obj?.x`?", "a", [4]string{"Short-circuit if null/undefined", "Throw error", "Deep clone", "Type cast"}, "?. stops evaluation if nullish."},
		}},
		{"css", "css-responsive", "medium", []struct{ q, c string; o [4]string; e string }{
			{"Media query `@media (min-width: 768px)`?", "b", [4]string{"Mobile only", "Apply styles from 768px and up", "Print styles", "Dark mode"}, "min-width = breakpoint and up."},
			{"CSS Grid `grid-template-columns: 1fr 1fr`?", "c", [4]string{"One column", "Three columns", "Two equal-width columns", "Flexbox"}, "1fr 1fr = two fractional tracks."},
			{"`z-index` works on?", "a", [4]string{"Positioned elements / stacking context", "All static elements", "Flex only", "Grid only"}, "z-index needs stacking context (positioned, opacity, etc.)."},
			{"`rem` is relative to?", "d", [4]string{"Parent font", "Viewport width", "Root element font", "Line height"}, "rem = root em size."},
			{"`gap` in flex/grid?", "b", [4]string{"Margin collapse", "Space between tracks/items", "Padding inside cell", "Border radius"}, "gap controls spacing between items/tracks."},
		}},
		{"html", "html-forms", "easy", []struct{ q, c string; o [4]string; e string }{
			{"Input type for email?", "a", [4]string{"type=\"email\"", "type=\"text\" only", "type=\"mail\"", "role=email"}, "type email provides basic validation & mobile keyboard."},
			{"Submit button inside form?", "c", [4]string{"type=\"button\" default", "Does not submit", "type=\"submit\" submits form", "Enter only"}, "Default button in form can submit — set type explicitly."},
			{"`alt` on img is for?", "b", [4]string{"SEO only", "Description for screen reader / fallback", "Lazy load", "CDN"}, "alt is required for a11y unless decorative."},
			{"Landmark `<nav>`?", "d", [4]string{"Footer", "Navigation links section", "Article body", "Sidebar ads"}, "nav is for navigation blocks."},
			{"`required` on input?", "a", [4]string{"HTML5 constraint validation", "Server-only validation", "CSS pseudo", "React-specific prop"}, "required is built-in constraint validation."},
		}},
	}

	for _, block := range topics {
		for _, it := range block.questions {
			out = append(out, mcqItem(it.q, it.c, it.o, it.e, []string{block.tag}, block.diff, block.topic, ""))
		}
	}

	// Hard questions
	hard := []struct{ q, c string; o [4]string; e, code string; tags []string; topic string }{
		{
			"Most likely bug in this code?",
			"b",
			[4]string{"Key not needed", "Stale closure — count always 0", "useState illegal", "JSX error"},
			"onClick captures count from render when handler was created without functional update.",
			"function Counter() {\n  const [count, setCount] = useState(0);\n  const log = () => console.log(count);\n  useEffect(() => {\n    const id = setInterval(log, 1000);\n    return () => clearInterval(id);\n  }, []);\n}",
			[]string{"react", "hooks"},
			"react-stale-closure",
		},
		{
			"Log output order?",
			"c",
			[4]string{"A B C", "B A C", "A C B", "C B A"},
			"await waits for Promise; sync log A, microtask/settled B, then C after await.",
			"async function f() {\n  console.log('A');\n  await Promise.resolve();\n  console.log('C');\n}\nf();\nconsole.log('B');",
			[]string{"javascript"},
			"js-async-order",
		},
		{
			"Performance issue with large lists?",
			"a",
			[4]string{"Re-render entire list when one item changes without memo/virtualization", "Not using class", "Not using CSS", "Too many divs"},
			"For large lists consider virtualization, memo items, stable keys.",
			"{items.map(i => <Row key={i.id} data={i} onSelect={setSelected} />)}",
			[]string{"react"},
			"react-performance",
		},
		{
			"Which specificity wins?",
			"d",
			[4]string{".btn", "button", "#save.btn", "inline style"},
			"Inline style beats normal selectors unless !important.",
			"button.primary { color: blue; }\n#save { color: red; }",
			[]string{"css"},
			"css-specificity",
		},
		{
			"a11y issue in this markup?",
			"b",
			[4]string{"No issue", "Icon button without accessible name", "Too many divs", "Using section"},
			"Icon-only button needs aria-label or visually hidden text.",
			"<button><img src=\"close.svg\" /></button>",
			[]string{"html"},
			"html-a11y-hard",
		},
	}
	for _, it := range hard {
		out = append(out, mcqItem(it.q, it.c, it.o, it.e, it.tags, "hard", it.topic, it.code))
	}

	out = append(out, extraMCQs()...)
	return out
}

// extraMCQs adds hand-written questions to keep the bank large without duplicate filler text.
func extraMCQs() []mcq {
	raw := []mcq{
		fullMCQ(
			"When should you use `useReducer` instead of multiple `useState`?",
			"b",
			[4]string{"Always for performance", "Next state depends on previous state or logic branches", "Class components only", "When you need Context"},
			"useReducer fits complex state with defined transitions.",
			map[string]string{"a": "useReducer is not the default for performance.", "c": "Modern function components support both.", "d": "Context distributes data, not a reducer replacement."},
			[]string{"react", "hooks"}, "medium", "react-usereducer", "",
		),
		fullMCQ(
			"What is the most common purpose of `useRef` in React?",
			"c",
			[4]string{"Replace useState", "Store CSS classes", "Store mutable values / DOM refs without triggering re-render", "Fetch data"},
			"useRef keeps values across renders without re-rendering.",
			map[string]string{"a": "useRef does not replace UI state.", "b": "Not for styling.", "d": "Fetch data with useEffect or a data library."},
			[]string{"react", "hooks"}, "medium", "react-useref", "",
		),
		fullMCQ(
			"What is wrong with `if (loading) return <Spinner />` before other hooks?",
			"a",
			[4]string{"Violates Rules of Hooks — hooks must run in the same order every render", "No problem", "Production-only error", "Must use class component"},
			"Hooks cannot come after a conditional early return.",
			map[string]string{"b": "This violates the rules of hooks.", "c": "Error also in development.", "d": "Function components can use hooks with strict rules."},
			[]string{"react", "hooks"}, "hard", "react-rules-of-hooks", "",
		),
		fullMCQ(
			"Which is correct about `useCallback`?",
			"d",
			[4]string{"Always makes components faster", "Replaces useMemo", "Caches computation results", "Caches function definition across renders when deps are unchanged"},
			"useCallback returns the same function reference when deps don't change.",
			map[string]string{"a": "Not always faster — has its own cost.", "b": "useMemo for values, useCallback for functions.", "c": "That describes useMemo."},
			[]string{"react", "hooks"}, "medium", "react-usecallback", "",
		),
		fullMCQ(
			"Event delegation in the DOM means?",
			"b",
			[4]string{"Every child has its own listener", "Listener on parent handles child events via bubbling", "React only", "Replaces addEventListener"},
			"Delegation uses the bubbling phase for one handler on an ancestor.",
			map[string]string{"a": "That is the opposite of delegation.", "c": "Native DOM concept, usable in React too.", "d": "Still uses addEventListener/onClick."},
			[]string{"javascript", "html"}, "medium", "js-event-delegation", "",
		),
		fullMCQ(
			"Difference between `let` and `var` in a `for` loop?",
			"c",
			[4]string{"Exactly the same", "var is block-scoped", "let is block-scoped, var is function-scoped", "let is hoisted like var"},
			"let per iteration/block; var one binding for the whole function.",
			map[string]string{"a": "Closure behavior in loops differs.", "b": "var is function-scoped, not block.", "d": "let has TDZ, not hoisted like var."},
			[]string{"javascript"}, "medium", "js-let-var", "",
		),
		fullMCQ(
			"Result of `JSON.stringify({a: undefined, b: 1})`?",
			"a",
			[4]string{"`{\"b\":1}`", "`{\"a\":null,\"b\":1}`", "throw", "`{}`"},
			"undefined properties are omitted when stringifying an object.",
			map[string]string{"b": "undefined is not null in JSON.", "c": "Does not throw for undefined property.", "d": "Property b is still present."},
			[]string{"javascript"}, "easy", "js-json", "",
		),
		fullMCQ(
			"Which selector has the highest specificity?",
			"c",
			[4]string{"div.card", "button.primary", "#submit.btn", "[type=submit]"},
			"ID selector (#submit) beats class and element.",
			map[string]string{"a": "One element + one class.", "b": "Element + class without ID.", "d": "Attribute selector is lower than ID."},
			[]string{"css"}, "medium", "css-specificity-2", "",
		),
		fullMCQ(
			"`align-items: center` on a flex container controls?",
			"b",
			[4]string{"Distribution on main axis", "Alignment on cross axis", "Flex item order", "Line wrapping"},
			"align-items = cross axis; justify-content = main axis.",
			map[string]string{"a": "That is justify-content.", "c": "Order uses flex-direction/order.", "d": "Wrap uses flex-wrap."},
			[]string{"css"}, "easy", "css-flex-align", "",
		),
		fullMCQ(
			"Which CSS property makes an element sticky on scroll?",
			"d",
			[4]string{"position: fixed only", "overflow: scroll", "high z-index", "position: sticky + offset (top/bottom)"},
			"sticky needs positioned sticky and top/bottom threshold within scroll container.",
			map[string]string{"a": "fixed leaves flow; sticky is relative until threshold.", "b": "overflow does not enable sticky alone.", "c": "z-index does not activate sticky."},
			[]string{"css"}, "medium", "css-sticky", "",
		),
		fullMCQ(
			"Which HTML element is best for main page content?",
			"a",
			[4]string{"<main>", "<div id=\"main\">", "<section> only", "<article> for entire page"},
			"<main> is the landmark for primary content — one per page.",
			map[string]string{"b": "div without landmark is less accessible.", "c": "section for thematic sections, not entire main content.", "d": "article for standalone content (posts), not entire app."},
			[]string{"html"}, "easy", "html-main", "",
		),
		fullMCQ(
			"Attribute `aria-live` is used for?",
			"b",
			[4]string{"Focus styling", "Announcing dynamic content changes to screen readers", "Form validation", "Lazy loading images"},
			"aria-live announces content changes to assistive tech.",
			map[string]string{"a": "Focus uses :focus-visible/tabIndex.", "c": "Validation uses constraint API or JS.", "d": "Lazy load uses loading=lazy."},
			[]string{"html"}, "hard", "html-aria-live", "",
		),
		fullMCQ(
			"In React, why doesn't `setCount(count + 1)` twice in a row always add +2?",
			"c",
			[4]string{"React bug", "Strict mode only", "State updates batch — reads same count value", "useState is async to server"},
			"Batching uses the same count snapshot; use functional update for double increment.",
			map[string]string{"a": "This is documented behavior.", "b": "Not only strict mode.", "d": "Local state, not server roundtrip."},
			[]string{"react"}, "hard", "react-batching",
			"setCount(count + 1);\nsetCount(count + 1);",
		),
		fullMCQ(
			"What does `key` on a component outside a list do?",
			"d",
			[4]string{"Required on all components", "SEO", "Styling", "Force remount when key changes (reset internal state)"},
			"Changing key makes React remount the subtree — useful to reset state.",
			map[string]string{"a": "Key is only required on list siblings.", "b": "Not for SEO.", "c": "Not a className substitute."},
			[]string{"react"}, "medium", "react-key-remount", "",
		),
		fullMCQ(
			"`fetch().then(r => r.json())` — when can the response body be read?",
			"a",
			[4]string{"Once — stream consumed after read", "Unlimited times", "Node only", "Only if status 200"},
			"Response body is a one-time stream.",
			map[string]string{"b": "Need clone() to read again.", "c": "Browser and Node alike.", "d": "Body can be read even on 4xx/5xx."},
			[]string{"javascript"}, "medium", "js-fetch", "",
		),
		fullMCQ(
			"Output of `[1, 2, 3].map(parseInt)` in JavaScript?",
			"c",
			[4]string{"[1, 2, 3]", "[1, NaN, NaN]", "[1, 2, 2]", "[NaN, NaN, NaN]"},
			"parseInt takes (string, radix); map passes (value, index) as arguments.",
			map[string]string{"a": "parseInt('2', 1) is not 2.", "b": "Index 2 with radix 2 yields 2.", "d": "First element is still 1."},
			[]string{"javascript"}, "hard", "js-parseint-map", "",
		),
		fullMCQ(
			"CSS `min-height: 100vh` on mobile is sometimes too tall because?",
			"b",
			[4]string{"vh not supported", "Browser address bar changes viewport — consider dvh/svh", "Because of flexbox", "Because of rem"},
			"Classic vh does not always match visible viewport on mobile browsers.",
			map[string]string{"a": "vh is widely supported.", "c": "Not flexbox-specific.", "d": "rem relates to root font."},
			[]string{"css"}, "medium", "css-viewport-units", "",
		),
		fullMCQ(
			"Grid `grid-template-areas` is useful for?",
			"a",
			[4]string{"Naming layout areas and placing items into areas", "Animation", "Font sizing", "Z-index"},
			"Named areas make dashboard layouts easier to read.",
			map[string]string{"b": "Animation uses @keyframes.", "c": "Font uses font-size.", "d": "Z-index is separate."},
			[]string{"css"}, "medium", "css-grid-areas", "",
		),
		fullMCQ(
			"Form `novalidate` means?",
			"c",
			[4]string{"Form cannot be submitted", "Server validation only", "Disable built-in HTML5 browser validation", "Remove required"},
			"novalidate disables built-in constraint validation — JS validation still works.",
			map[string]string{"a": "Submit still works.", "b": "Not automatically server-only.", "d": "required remains in markup."},
			[]string{"html"}, "medium", "html-novalidate", "",
		),
		fullMCQ(
			"Which is a scalable React form validation pattern?",
			"d",
			[4]string{"One large useState object without structure", "Store everything on window", "alert() only", "Schema validation (zod/yup) + form library or structured reducer"},
			"Large forms need schema, per-field errors, and separation of concerns.",
			map[string]string{"a": "Hard to maintain and test.", "b": "Global state anti-pattern.", "c": "alert is not accessible/poor UX."},
			[]string{"react"}, "medium", "react-form-patterns", "",
		),
	}
	return raw
}

func fullMCQ(q, correct string, opts [4]string, expl string, wrong map[string]string, tags []string, diff, topic, code string) mcq {
	ch := []choice{
		{ID: "a", Text: opts[0]},
		{ID: "b", Text: opts[1]},
		{ID: "c", Text: opts[2]},
		{ID: "d", Text: opts[3]},
	}
	return mcq{
		Question:          q,
		CodeSnippet:       code,
		Choices:           ch,
		CorrectID:         correct,
		Explanation:       expl,
		WrongExplanations: wrong,
		BestPractices:     []string{"Understand core concepts before memorizing answers", "Relate questions to daily coding experience", "Read the explanation after submitting to learn"},
		LearningObjective: topic,
		Points:            10,
		Tags:              tags,
		Difficulty:        diff,
		Topic:             topic,
	}
}

type buildTask struct {
	Title               string          `json:"title"`
	Family              string          `json:"family"`
	SpecMarkdown        string          `json:"spec_markdown"`
	StarterCode         string          `json:"starter_code"`
	SolutionCode        string          `json:"solution_code"`
	SolutionExplanation string          `json:"solution_explanation"`
	RubricJSON          json.RawMessage `json:"rubric_json"`
	TestCases           json.RawMessage `json:"test_cases"`
	BestPractices       []string        `json:"best_practices"`
	CommonMistakes      []string        `json:"common_mistakes"`
	LearningObjective   string          `json:"learning_objective"`
	Difficulty          string          `json:"difficulty"`
	Points              int             `json:"points"`
}

var buildRubric = json.RawMessage(`{"criteria":[{"id":"tests_pass","label":"Functional criteria met","points":40,"auto":true},{"id":"controlled","label":"Controlled inputs","points":0,"auto":false},{"id":"validation","label":"Validation per instructions","points":0,"auto":false}]}`)

type assertionCase struct {
	Check     string `json:"check"`
	Field     string `json:"field,omitempty"`
	Max       int    `json:"max,omitempty"`
	Min       int    `json:"min,omitempty"`
	Substring string `json:"substring,omitempty"`
	ID        string `json:"id,omitempty"`
	Prop      string `json:"prop,omitempty"`
}

func mustAssertions(cases ...assertionCase) json.RawMessage {
	raw, err := json.Marshal(cases)
	if err != nil {
		panic(err)
	}
	return raw
}

func buildSpecMarkdown(title, field, validate string) string {
	return fmt.Sprintf(`## Goal
React form exercise: controlled input, validation, and submit via callback.

## Build component %s
- Use a **controlled input** for field %s (value + onChange)
- **Validation:** %s
- Include a **Submit** button
- If invalid, show an **error message** (element with role="alert")
- If valid, call the **onSubmit** prop with an object containing %s

## How to verify
Click **Run tests** — your solution does not need to match the reference exactly, as long as it meets the criteria above.`,
		fmt.Sprintf("`%s`", title), fmt.Sprintf("`%s`", field), validate, fmt.Sprintf("`%s`", field))
}

func buildAssertions(field, validate, testID string) json.RawMessage {
	checks := []assertionCase{
		{Check: "has_testid", ID: testID},
		{Check: "controlled_input", Field: field},
		{Check: "form_prevent_default"},
		{Check: "calls_on_submit", Field: field},
		{Check: "shows_error"},
	}
	switch {
	case strings.Contains(validate, "max 200"):
		checks = append(checks,
			assertionCase{Check: "validates_required", Field: field},
			assertionCase{Check: "validates_max_length", Field: field, Max: 200},
		)
	case strings.Contains(validate, "max 120"):
		checks = append(checks,
			assertionCase{Check: "validates_required", Field: field},
			assertionCase{Check: "validates_max_length", Field: field, Max: 120},
		)
	case strings.Contains(validate, "max 80"):
		checks = append(checks,
			assertionCase{Check: "validates_required", Field: field},
			assertionCase{Check: "validates_max_length", Field: field, Max: 80},
		)
	case strings.Contains(validate, "must contain https"), strings.Contains(validate, "mengandung https"):
		checks = append(checks, assertionCase{Check: "validates_includes", Field: field, Substring: "https"})
	case strings.Contains(validate, "must contain @"), strings.Contains(validate, "mengandung @"):
		checks = append(checks, assertionCase{Check: "validates_includes", Field: field, Substring: "@"})
	case strings.Contains(validate, "min 20"):
		checks = append(checks, assertionCase{Check: "validates_min_length", Field: field, Min: 20})
	case strings.Contains(validate, "min 10"):
		checks = append(checks, assertionCase{Check: "validates_min_length", Field: field, Min: 10})
	case strings.Contains(validate, "min 8"):
		checks = append(checks, assertionCase{Check: "validates_min_length", Field: field, Min: 8})
	case strings.Contains(validate, "min 6"), strings.Contains(validate, "minimal 6"):
		checks = append(checks, assertionCase{Check: "validates_min_length", Field: field, Min: 6})
	case strings.Contains(validate, "min 5"), strings.Contains(validate, "minimal 5"):
		checks = append(checks, assertionCase{Check: "validates_min_length", Field: field, Min: 5})
	case strings.Contains(validate, "min 4"), strings.Contains(validate, "minimal 4"):
		checks = append(checks, assertionCase{Check: "validates_min_length", Field: field, Min: 4})
	case strings.Contains(validate, "min 2"), strings.Contains(validate, "minimal 2"):
		checks = append(checks, assertionCase{Check: "validates_min_length", Field: field, Min: 2})
	case strings.Contains(validate, "min 3"), strings.Contains(validate, "minimal 3"):
		checks = append(checks, assertionCase{Check: "validates_min_length", Field: field, Min: 3})
	default:
		checks = append(checks, assertionCase{Check: "validates_required", Field: field})
	}
	return mustAssertions(checks...)
}

func buildValidationBlock(validate string) string {
	switch {
	case strings.Contains(validate, "max 200"):
		return `    if (value.length > 200) {
      setError("Maximum 200 characters");
      return;
    }`
	case strings.Contains(validate, "max 120"):
		return `    if (value.length > 120) {
      setError("Maximum 120 characters");
      return;
    }`
	case strings.Contains(validate, "max 80"):
		return `    if (value.length > 80) {
      setError("Maximum 80 characters");
      return;
    }`
	case strings.Contains(validate, "must contain https"), strings.Contains(validate, "mengandung https"):
		return `    if (!value.includes("https")) {
      setError("URL must contain https");
      return;
    }`
	case strings.Contains(validate, "must contain @"), strings.Contains(validate, "mengandung @"):
		return `    if (!value.includes("@")) {
      setError("Email must contain @");
      return;
    }`
	case strings.Contains(validate, "min 20"):
		return `    if (value.trim().length < 20) {
      setError("Minimum 20 characters");
      return;
    }`
	case strings.Contains(validate, "min 10"):
		return `    if (value.replace(/\D/g, "").length < 10) {
      setError("Minimum 10 digits");
      return;
    }`
	case strings.Contains(validate, "min 8"):
		return `    if (value.length < 8) {
      setError("Minimum 8 characters");
      return;
    }`
	case strings.Contains(validate, "min 6"), strings.Contains(validate, "minimal 6"):
		return `    if (value.length < 6) {
      setError("Minimum 6 characters");
      return;
    }`
	case strings.Contains(validate, "min 5"), strings.Contains(validate, "minimal 5"):
		return `    if (value.trim().length < 5) {
      setError("Minimum 5 characters");
      return;
    }`
	case strings.Contains(validate, "min 4"), strings.Contains(validate, "minimal 4"):
		return `    if (value.trim().length < 4) {
      setError("Minimum 4 characters");
      return;
    }`
	case strings.Contains(validate, "min 2"), strings.Contains(validate, "minimal 2"):
		return `    if (value.trim().length < 2) {
      setError("Minimum 2 characters");
      return;
    }`
	case strings.Contains(validate, "min 3"), strings.Contains(validate, "minimal 3"):
		return `    if (value.trim().length < 3) {
      setError("Minimum 3 characters");
      return;
    }`
	default:
		return `    if (!value.trim()) {
      setError("Field is required");
      return;
    }`
	}
}

func generateBuilds() []buildTask {
	specs := []struct {
		title, name, field, diff string
		validate                 string
	}{
		{"WaitlistForm", "email", "email", "medium", "email must contain @"},
		{"NewsletterSignup", "email", "email", "easy", "email required"},
		{"ContactForm", "name", "name", "medium", "name min 2 characters"},
		{"LoginForm", "password", "password", "medium", "password min 6 characters"},
		{"SearchBar", "query", "query", "easy", "show query below input"},
		{"FeedbackForm", "message", "message", "medium", "message must not be empty"},
		{"RegisterForm", "email", "email", "hard", "valid email + password match"},
		{"SubscribeForm", "email", "email", "easy", "consent checkbox must be checked"},
		{"ProfileForm", "displayName", "displayName", "medium", "displayName required"},
		{"BookingForm", "date", "date", "medium", "date input type=date"},
		{"CouponForm", "code", "code", "easy", "code min 3 characters"},
		{"AddressForm", "city", "city", "medium", "city required"},
		{"PhoneVerifyForm", "phone", "phone", "hard", "phone digits only"},
		{"RatingForm", "rating", "rating", "easy", "select rating 1-5"},
		{"CommentForm", "comment", "comment", "medium", "max 200 characters"},
		{"InviteForm", "inviteEmail", "inviteEmail", "medium", "friend email"},
		{"ResetPasswordForm", "newPassword", "newPassword", "hard", "confirm password must match"},
		{"JobApplyForm", "linkedin", "linkedin", "medium", "optional valid LinkedIn URL"},
		{"EventRSVPForm", "guests", "guests", "easy", "guest count number >= 1"},
		{"SupportTicketForm", "subject", "subject", "medium", "subject required"},
		{"CheckoutEmailForm", "checkoutEmail", "checkoutEmail", "medium", "email before continuing"},
		{"BetaAccessForm", "reason", "reason", "hard", "reason min 20 characters"},
		{"MailingListForm", "firstName", "firstName", "easy", "firstName required"},
		{"SurveyForm", "satisfaction", "satisfaction", "medium", "radio satisfaction"},
		{"DemoRequestForm", "company", "company", "medium", "company required"},
		{"PartnerForm", "website", "website", "hard", "website must contain https"},
		{"AlertSignupForm", "topic", "topic", "easy", "select topic from dropdown"},
		{"GiftCardForm", "amount", "amount", "medium", "amount number > 0"},
		{"ReferralForm", "referralCode", "referralCode", "medium", "referral code"},
		{"OnboardingForm", "role", "role", "easy", "select role developer/designer"},
	}

	var out []buildTask
	for _, s := range specs {
		cmp := s.title
		testID := toTestID(cmp)
		validation := buildValidationBlock(s.validate)
		out = append(out, buildTask{
			Title:  cmp,
			Family: "form",
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
			SolutionExplanation: "Controlled input, validation per instructions, error with role=alert, onSubmit called when valid.",
			RubricJSON:          buildRubric,
			TestCases:           buildAssertions(s.field, s.validate, testID),
			BestPractices:       []string{"Controlled components", "Validate before submit", "Label htmlFor for a11y", "preventDefault on form submit"},
			CommonMistakes:        []string{"Forgetting preventDefault", "Uncontrolled input", "Not showing errors"},
			LearningObjective:   fmt.Sprintf("React form — %s", s.title),
			Difficulty:          s.diff,
			Points:              40,
		})
	}
	return out
}

func toTestID(name string) string {
	var b []byte
	for i, r := range name {
		if r >= 'A' && r <= 'Z' {
			if i > 0 {
				b = append(b, '-')
			}
			b = append(b, byte(r+'a'-'A'))
		} else {
			b = append(b, byte(r))
		}
	}
	return string(b)
}

type debugTask struct {
	Title             string          `json:"title"`
	Family            string          `json:"family"`
	BrokenCode        string          `json:"broken_code"`
	SolutionCode      string          `json:"solution_code"`
	BugDescription    string          `json:"bug_description"`
	RootCause         string          `json:"root_cause"`
	FixExplanation    string          `json:"fix_explanation"`
	TestCases         json.RawMessage `json:"test_cases"`
	BestPractices     []string        `json:"best_practices"`
	CommonMistakes    []string        `json:"common_mistakes"`
	LearningObjective string          `json:"learning_objective"`
	Difficulty        string          `json:"difficulty"`
	Points            int             `json:"points"`
}

func coreDebugBugs() []struct {
	title, bug, cause, fix, diff string
	broken, fixed                string
} {
	return []struct {
		title, bug, cause, fix, diff string
		broken, fixed                string
	}{
		{
			"Hero Infinite Render",
			"Page hangs when Hero mounts.",
			"setState is called directly in the render body.",
			"Remove setState from render; use an event handler or useEffect with correct deps.",
			"medium",
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
		},
		{
			"Hero Stale Props",
			"Subtitle does not update when the prop changes.",
			"State is initialized from props only once without syncing.",
			"Render directly from props or sync with useEffect when the prop changes.",
			"medium",
			`export function Hero({ subtitle }) {
  const [text, setText] = useState(subtitle);
  return <section data-testid="hero"><p>{text}</p></section>;
}`,
			`export function Hero({ subtitle }) {
  return <section data-testid="hero"><p>{subtitle}</p></section>;
}`,
		},
		{
			"Hero Missing Key",
			"CTA list order is wrong after filter.",
			"Using index as key causes the reconciler to incorrectly reuse DOM.",
			"Use a stable id from the data as the key.",
			"easy",
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
		},
	}
}

func generateDebugs() []debugTask {
	bugs := append(coreDebugBugs(), debugBugsBatch2()...)

	var out []debugTask
	for i := 0; i < 45; i++ {
		b := bugs[i%len(bugs)]
		title := fmt.Sprintf("%s #%02d", b.title, i+1)
		kind := i % len(bugs)
		out = append(out, debugTask{
			Title:          title,
			Family:         "hero",
			BrokenCode:     prependImport(b.broken),
			SolutionCode:   prependImport(b.fixed),
			BugDescription: debugSpecMarkdown(title, b.bug, debugHintExtended(kind)),
			RootCause:      b.cause,
			FixExplanation: b.fix,
			TestCases:      debugAssertionsExtended(kind),
			BestPractices:     []string{"Never setState during render", "Stable keys on lists", "React DevTools Profiler"},
			CommonMistakes:      []string{"Adding if statements without understanding the render cycle", "Removing state the UI still needs"},
			LearningObjective: fmt.Sprintf("Debug React Hero — sample %02d", i+1),
			Difficulty:        b.diff,
			Points:            35,
		})
	}
	return out
}

func debugSpecMarkdown(title, symptom, hint string) string {
	base := strings.Split(title, " #")[0]
	heroNote := ""
	if strings.Contains(base, "Hero") {
		heroNote = "\n\n**Note:** `Hero` is a React **landing-page hero section** — the large top banner with headline/image/CTA (not a game character).\n"
	}
	return fmt.Sprintf(`## Symptom
%s

## Your task
Fix the **%s** component in the editor until the preview works correctly.
%s
## Hint
%s

## How to verify
Click **Run tests** — your code does not need to match the reference solution exactly.`,
		symptom, base, heroNote, hint)
}

func debugHint(kind int) string {
	switch kind {
	case 0:
		return "Check whether setState is called during render rather than in an event handler."
	case 1:
		return "Check whether state derived from props updates when the parent sends a new prop."
	default:
		return "Check how you render the `items` list — React needs stable keys."
	}
}

func debugAssertions(kind int) json.RawMessage {
	switch kind {
	case 0:
		return mustAssertions(
			assertionCase{Check: "no_setstate_in_render"},
			assertionCase{Check: "has_testid", ID: "hero"},
		)
	case 1:
		return mustAssertions(
			assertionCase{Check: "renders_prop", Prop: "subtitle"},
			assertionCase{Check: "has_testid", ID: "hero"},
		)
	default:
		return mustAssertions(
			assertionCase{Check: "no_index_list_key"},
			assertionCase{Check: "stable_list_key"},
		)
	}
}

func prependImport(body string) string {
	if len(body) == 0 || body[0] != 'e' {
		return body
	}
	imports := "useState"
	if strings.Contains(body, "useEffect") {
		imports = "useState, useEffect"
	}
	return fmt.Sprintf("import { %s } from \"react\";\n\n%s", imports, body)
}

type blueprintSeed struct {
	Slug   string          `json:"slug"`
	Title  string          `json:"title"`
	Config json.RawMessage `json:"config"`
}

func tendemConfigJSON(focus string) json.RawMessage {
	cfg := fmt.Sprintf(`{
  "sections": [
    {"type": "mcq", "count": 5, "timeLimitMinutes": 40, "tags": ["react", "javascript", "css", "html"]},
    {"type": "react_build", "count": 1, "timeLimitMinutes": 35, "componentFamily": "form"},
    {"type": "react_debug", "count": 1, "timeLimitMinutes": 23, "componentFamily": "hero"}
  ],
  "totalTimeLimitMinutes": 98,
  "proctoring": {"maxBlurEvents": 3, "warnOnPaste": true, "blockPasteInEditor": true}
}`)
	_ = focus
	return json.RawMessage(cfg)
}

func generateBlueprints() []blueprintSeed {
	foci := []string{
		"React Hooks", "JavaScript Core", "CSS Layout", "HTML Semantic", "Forms & Validation",
		"Performance", "State Management", "Async JS", "Flexbox", "Grid",
		"Accessibility", "Component Patterns", "Event Handling", "Lists & Keys", "useEffect",
		"Controlled Inputs", "Responsive UI", "Type Coercion", "DOM APIs", "React Memo",
		"Error Handling", "Routing Basics", "Testing Mindset", "CSS Specificity", "Semantic Forms",
		"Closures", "Promises", "React Context", "Debug Loops", "Interview Mix",
	}
	var out []blueprintSeed
	for i, focus := range foci {
		out = append(out, blueprintSeed{
			Slug:   fmt.Sprintf("tendem-fe-%02d", i+1),
			Title:  fmt.Sprintf("Tendem Frontend Developer — Sample %02d (%s)", i+1, focus),
			Config: tendemConfigJSON(focus),
		})
	}
	return out
}
