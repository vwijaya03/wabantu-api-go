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
			"Browser freeze setelah Hero mount.",
			"useEffect tanpa dependency array memicu setState tiap render.",
			"Tambahkan dependency array yang benar atau pindahkan logic ke event handler.",
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
			"Klik tombol langsung menambah counter tanpa interaksi.",
			"onClick memanggil handler langsung saat render, bukan referensi function.",
			"Pass function reference: onClick={() => handler()} bukan onClick={handler()}.",
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
			"Error Rules of Hooks saat showDetails false.",
			"useState dipanggil di dalam conditional branch.",
			"Pindahkan semua hooks ke top level komponen.",
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
			"UI kosong meski props title ada.",
			"Arrow function implicit return hilang karena block body tanpa return.",
			"Tambahkan return eksplisit atau gunakan parentheses pada JSX.",
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
			"Input tidak ter-update saat parent mengirim value baru.",
			"defaultValue tidak sync dengan prop value berikutnya.",
			"Gunakan controlled input: value + onChange dari props/state.",
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
		return "Perhatikan apakah ada setState yang dipanggil saat render, bukan di event handler."
	case 1:
		return "Perhatikan apakah state dari props ikut berubah saat parent mengirim prop baru."
	case 2:
		return "Perhatikan cara me-render list `items` — React butuh key yang stabil."
	case 3:
		return "Perhatikan dependency array pada useEffect — kosong vs tidak ada beda perilaku."
	case 4:
		return "Perhatikan cara men-pass handler ke onClick — jangan invoke function saat render."
	case 5:
		return "Hooks harus dipanggil di top level, tidak di dalam if/loop."
	case 6:
		return "Pastikan JSX di-return dari function component."
	default:
		return "Perhatikan apakah input controlled (`value`) atau uncontrolled (`defaultValue`)."
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
			assertionCase{Check: "no_setstate_in_render"},
			assertionCase{Check: "has_testid", ID: "hero"},
		)
	case 4:
		return mustAssertions(
			assertionCase{Check: "no_setstate_in_render"},
			assertionCase{Check: "has_testid", ID: "hero"},
		)
	case 5:
		return mustAssertions(
			assertionCase{Check: "has_testid", ID: "hero"},
		)
	case 6:
		return mustAssertions(
			assertionCase{Check: "renders_prop", Prop: "title"},
			assertionCase{Check: "has_testid", ID: "hero"},
		)
	default:
		return mustAssertions(
			assertionCase{Check: "has_testid", ID: "hero"},
		)
	}
}
