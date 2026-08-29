package main

import "encoding/json"

func debugBugsBatch2() []struct {
	title, bug, cause, fix, diff string
	broken, fixed                string
} {
	return []struct {
		title, bug, cause, fix, diff string
		broken, fixed                string
	}{
		{
			"Hero Effect Loop",
			"Browser freezes after Hero mounts.",
			"useEffect without a dependency array triggers setState on every render.",
			"Add the correct dependency array or move logic to an event handler.",
			"hard",
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
		},
		{
			"Hero Click Handler",
			"Clicking the button immediately increments the counter without interaction.",
			"onClick invokes the handler during render instead of passing a function reference.",
			"Pass a function reference: onClick={() => handler()} not onClick={handler()}.",
			"medium",
			`export function Hero({ title }) {
  const [n, setN] = useState(0);
  const bump = () => setN((x) => x + 1);
  return (
    <section data-testid="hero">
      <h1>{title}</h1>
      <button type="button" onClick={bump()}>+</button>
      <p>{n}</p>
    </section>
  );
}`,
			`export function Hero({ title }) {
  const [n, setN] = useState(0);
  const bump = () => setN((x) => x + 1);
  return (
    <section data-testid="hero">
      <h1>{title}</h1>
      <button type="button" onClick={bump}>+</button>
      <p>{n}</p>
    </section>
  );
}`,
		},
		{
			"Hero Conditional Hook",
			"Rules of Hooks error when showDetails is false.",
			"useState is called inside a conditional branch.",
			"Move all hooks to the top level of the component.",
			"hard",
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
		},
		{
			"Hero Missing Return",
			"UI is empty even though title prop is set.",
			"Arrow function implicit return is lost because of a block body without return.",
			"Add an explicit return or use parentheses around JSX.",
			"easy",
			`export function Hero({ title }) {
  const label = title.toUpperCase();
  <section data-testid="hero"><h1>{label}</h1></section>;
}`,
			`export function Hero({ title }) {
  const label = title.toUpperCase();
  return <section data-testid="hero"><h1>{label}</h1></section>;
}`,
		},
		{
			"Hero Uncontrolled Input",
			"Input does not update when parent sends a new value.",
			"defaultValue does not sync with subsequent value prop updates.",
			"Use a controlled input: value + onChange from props/state.",
			"medium",
			`export function Hero({ value, onChange }) {
  return (
    <section data-testid="hero">
      <input defaultValue={value} onChange={onChange} />
    </section>
  );
}`,
			`export function Hero({ value, onChange }) {
  return (
    <section data-testid="hero">
      <input value={value} onChange={onChange} />
    </section>
  );
}`,
		},
	}
}

func debugHintExtended(kind int) string {
	switch kind {
	case 0:
		return "Check whether setState is called during render instead of in an event handler."
	case 1:
		return "Check whether state from props updates when the parent sends a new prop."
	case 2:
		return "Check how you render the `items` list — React needs stable keys."
	case 3:
		return "Check the useEffect dependency array — empty vs missing behaves differently."
	case 4:
		return "Check how you pass handlers to onClick — do not invoke the function during render."
	case 5:
		return "Hooks must be called at the top level, not inside if/loop."
	case 6:
		return "Make sure JSX is returned from the function component."
	default:
		return "Check whether the input is controlled (`value`) or uncontrolled (`defaultValue`)."
	}
}

func debugAssertionsExtended(kind int) json.RawMessage {
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
	case 2:
		return mustAssertions(
			assertionCase{Check: "no_index_list_key"},
			assertionCase{Check: "stable_list_key"},
		)
	case 3:
		return mustAssertions(
			assertionCase{Check: "has_testid", ID: "hero"},
			assertionCase{Check: "use_effect_has_dependency_array"},
			assertionCase{Check: "no_broken_onclick_setstate"},
		)
	case 4:
		return mustAssertions(
			assertionCase{Check: "has_testid", ID: "hero"},
			assertionCase{Check: "onclick_handler_reference"},
		)
	case 5:
		return mustAssertions(
			assertionCase{Check: "has_testid", ID: "hero"},
			assertionCase{Check: "no_hooks_in_conditional"},
		)
	case 6:
		return mustAssertions(
			assertionCase{Check: "has_testid", ID: "hero"},
			assertionCase{Check: "has_explicit_return"},
			assertionCase{Check: "renders_prop", Prop: "title"},
		)
	default:
		return mustAssertions(
			assertionCase{Check: "has_testid", ID: "hero"},
		)
	}
}
