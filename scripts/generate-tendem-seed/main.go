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
	build := append(generateBuilds(), generateBuildsBatch2()...)
	debug := generateDebugs()
	blueprints := generateBlueprints()

	writeJSON(filepath.Join(root, "tendem_mcq.json"), mcq)
	writeJSON(filepath.Join(root, "tendem_mcq_batch2.json"), mcqBatch2)
	writeJSON(filepath.Join(root, "tendem_build.json"), build)
	writeJSON(filepath.Join(root, "tendem_debug.json"), debug)
	writeJSON(filepath.Join(root, "tendem_blueprints.json"), blueprints)

	fmt.Printf("wrote %d + %d MCQs, %d builds, %d debugs, %d blueprints\n",
		len(mcq), len(mcqBatch2), len(build), len(debug), len(blueprints))
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
			wrong[ch.ID] = fmt.Sprintf("Opsi %q bukan jawaban terbaik untuk soal ini.", ch.Text)
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
		BestPractices:     []string{"Baca soal dan snippet dengan teliti", "Eliminasi opsi yang jelas salah", "Hubungkan konsep dengan kasus nyata di UI"},
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
		{"Apa fungsi utama React?", "b", [4]string{"Database ORM", "Membangun UI berbasis komponen", "Package manager", "CSS preprocessor"}, "React fokus pada view layer dengan komponen reusable."},
		{"File ekstensi apa yang umum untuk komponen React?", "a", [4]string{".jsx / .tsx", ".vue", ".svelte", ".angular"}, "JSX/TSX adalah sintaks umum di ekosistem React."},
		{"Prop `children` di React berisi apa?", "c", [4]string{"Hanya string", "Hanya array", "Konten nested di dalam komponen", "Hanya elemen img"}, "children adalah slot untuk konten yang di-render di dalam komponen."},
		{"Cara benar render list di JSX?", "b", [4]string{"items.forEach", "items.map + key", "items.filter saja", "while loop di JSX"}, "map menghasilkan array elemen; key membantu reconciler."},
		{"Default props behavior di function component modern?", "a", [4]string{"Default parameter / default value destructuring", "Hanya class component", "Tidak didukung", "Hanya dengan Redux"}, "Default values bisa di parameter destructuring props."},
	}
	for _, it := range reactEasy {
		out = append(out, mcqItem(it.q, it.c, it.o, it.e, []string{"react"}, "easy", "react-basics", ""))
	}

	jsEasy := []struct{ q, c string; o [4]string; e string }{
		{"Hasil `[] == ![]` di JavaScript?", "b", [4]string{"true", "false", "undefined", "throw"}, "Coercion membuat perbandingan loose equality membingungkan — pakai ===."},
		{"`const` untuk object berarti?", "c", [4]string{"Object immutable", "Tidak bisa reassign binding, properti bisa berubah", "Hanya di block scope function", "Sama dengan Object.freeze"}, "const mencegah reassign variabel, bukan deep freeze."},
		{"Spread operator `{...obj}` digunakan untuk?", "a", [4]string{"Shallow copy / merge object", "Deep clone otomatis", "Hanya array", "Menghapus key"}, "Spread membuat salinan dangkal properti enumerable."},
		{"`===` dibanding `==`?", "d", [4]string{"Sama persis", "== lebih ketat", "=== melakukan coercion", "=== tanpa type coercion"}, "Strict equality tidak melakukan type coercion."},
		{"Apa itu hoisting di JS?", "b", [4]string{"CSS technique", "Deklarasi diangkat ke atas scope (var/function)", "React feature", "Bundler optimization"}, "var dan function declaration dihoist; let/const TDZ."},
	}
	for _, it := range jsEasy {
		out = append(out, mcqItem(it.q, it.c, it.o, it.e, []string{"javascript"}, "easy", "js-basics", ""))
	}

	cssEasy := []struct{ q, c string; o [4]string; e string }{
		{"`display: flex` mengaktifkan?", "a", [4]string{"Flexbox layout", "Grid layout", "Block formatting saja", "Table layout"}, "Flexbox untuk layout satu dimensi."},
		{"`box-sizing: border-box`?", "b", [4]string{"Padding di luar width", "Width termasuk padding & border", "Hanya untuk margin", "Menghapus border"}, "border-box membuat ukuran lebih predictable."},
		{"Unit relatif terhadap font-size elemen?", "c", [4]string{"px", "vh", "em", "cm"}, "em relatif terhadap font-size elemen (atau parent untuk font)."},
		{"Pseudo-class untuk hover?", "a", [4]string{":hover", "::hover", ":hover()", "@hover"}, ":hover adalah pseudo-class interaksi."},
		{"`position: absolute` relatif terhadap?", "d", [4]string{"Selalu viewport", "Selalu body", "Parent pertama", "Nearest positioned ancestor"}, "Absolute positioning relatif ke ancestor yang positioned."},
	}
	for _, it := range cssEasy {
		out = append(out, mcqItem(it.q, it.c, it.o, it.e, []string{"css"}, "easy", "css-basics", ""))
	}

	// Medium batch — mix react/js/css with code snippets
	medium := []mcq{
		mcqItem(
			"Apa output console.log setelah klik tombol sekali?",
			"b",
			[4]string{"0", "1", "2", "undefined"},
			"Functional update `setCount(c => c+1)` aman; di sini onClick increment sekali per klik.",
			[]string{"react", "hooks"},
			"medium",
			"react-state",
			"function Counter() {\n  const [count, setCount] = useState(0);\n  return <button onClick={() => setCount(c => c + 1)}>{count}</button>;\n}",
		),
		mcqItem(
			"Masalah utama pada useEffect berikut?",
			"c",
			[4]string{"Tidak ada return cleanup", "Dependency array salah — infinite loop risk", "useState tidak valid", "JSX tidak valid"},
			"Effect tanpa dependency array jalan tiap render; jika setState di dalamnya bisa loop.",
			[]string{"react", "hooks"},
			"medium",
			"react-useeffect",
			"useEffect(() => {\n  setLoading(true);\n  fetchData();\n});",
		),
		mcqItem(
			"Promise chain: urutan log yang benar?",
			"a",
			[4]string{"start → microtask → end", "end → start → microtask", "microtask → end → start", "random"},
			"Microtask (.then) jalan sebelum macrotask berikutnya setelah sync code.",
			[]string{"javascript"},
			"medium",
			"js-async",
			"console.log('start');\nPromise.resolve().then(() => console.log('microtask'));\nconsole.log('end');",
		),
		mcqItem(
			"Flex child `flex: 1` umumnya berarti?",
			"b",
			[4]string{"flex-grow: 0", "flex-grow: 1, shrink: 1, basis: 0%", "Hanya width 100%", "display block"},
			"Shorthand flex:1 setara grow 1 dan basis 0 — child mengisi ruang tersisa.",
			[]string{"css"},
			"medium",
			"css-flexbox",
			".row { display: flex; }\n.item { flex: 1; }",
		),
		mcqItem(
			"Atribut a11y yang menghubungkan label ke input?",
			"a",
			[4]string{"htmlFor + id", "class + id", "role=label", "tabIndex saja"},
			"htmlFor pada label harus match id input agar screen reader bekerja.",
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
			{"Kapan `React.memo` membantu?", "b", [4]string{"Selalu", "Child re-render sering dengan props sama", "Hanya class component", "Untuk fetch data"}, "memo skip render jika props shallow equal."},
			{"`useMemo` digunakan untuk?", "c", [4]string{"Side effect", "Menyimpan nilai hasil komputasi mahal", "DOM ref", "Routing"}, "useMemo cache hasil fungsi, bukan side effect."},
			{"Context API cocok untuk?", "a", [4]string{"Data global yang jarang berubah (theme, locale)", "Semua state aplikasi", "Ganti Redux selalu", "Fetch API"}, "Context untuk prop drilling, bukan semua state."},
			{"Error boundary menangkap?", "d", [4]string{"Event handler errors", "Async errors otomatis", "Semua error", "Render errors di subtree"}, "Error boundary tidak tangkap event handler / async tanpa wrapper."},
			{"Fragment `<></>` berguna untuk?", "b", [4]string{"Styling", "Return multiple nodes tanpa DOM wrapper", "Key pada list", "Portal"}, "Fragment menghindari div tambahan."},
		}},
		{"javascript", "js-closure", "medium", []struct{ q, c string; o [4]string; e string }{
			{"Closure adalah?", "a", [4]string{"Fungsi akses variabel lexical scope", "CSS module", "React hook", "HTTP cache"}, "Closure = fungsi + referensi ke scope luar."},
			{"`async/await` setara dengan?", "c", [4]string{"Callback hell saja", "setTimeout", "Promise syntax sugar", "Web Worker"}, "async function mengembalikan Promise."},
			{"Destructuring `const {a} = obj`?", "b", [4]string{"Mutasi obj", "Ekstrak properti a", "Deep clone", "JSON parse"}, "Destructuring assignment untuk objek/array."},
			{"`map` vs `forEach`?", "d", [4]string{"Sama", "forEach return array baru", "map untuk side effect", "map return array baru, forEach tidak"}, "map transformasi; forEach side effect."},
			{"Optional chaining `obj?.x`?", "a", [4]string{"Short-circuit jika null/undefined", "Throw error", "Deep clone", "Type cast"}, "?. menghentikan evaluasi jika nullish."},
		}},
		{"css", "css-responsive", "medium", []struct{ q, c string; o [4]string; e string }{
			{"Media query `@media (min-width: 768px)`?", "b", [4]string{"Mobile only", "Apply styles dari 768px ke atas", "Print styles", "Dark mode"}, "min-width = breakpoint ke atas."},
			{"CSS Grid `grid-template-columns: 1fr 1fr`?", "c", [4]string{"Satu kolom", "Tiga kolom", "Dua kolom sama lebar", "Flexbox"}, "1fr 1fr = dua track fractional."},
			{"`z-index` bekerja pada?", "a", [4]string{"Positioned elements / stacking context", "Semua elemen static", "Hanya flex", "Hanya grid"}, "z-index butuh stacking context (positioned, opacity, dll)."},
			{"`rem` relatif terhadap?", "d", [4]string{"Parent font", "Viewport width", "Root element font", "Line height"}, "rem = root em size."},
			{"`gap` di flex/grid?", "b", [4]string{"Margin collapse", "Jarak antar track/items", "Padding dalam cell", "Border radius"}, "gap mengatur spacing antar items/tracks."},
		}},
		{"html", "html-forms", "easy", []struct{ q, c string; o [4]string; e string }{
			{"Input type untuk email?", "a", [4]string{"type=\"email\"", "type=\"text\" saja", "type=\"mail\"", "role=email"}, "type email memberi validasi dasar & keyboard mobile."},
			{"Button submit di dalam form?", "c", [4]string{"type=\"button\" default", "Tidak submit", "type=\"submit\" submit form", "Hanya Enter"}, "Default button di form bisa submit — set type eksplisit."},
			{"`alt` pada img untuk?", "b", [4]string{"SEO saja", "Deskripsi untuk screen reader / fallback", "Lazy load", "CDN"}, "alt wajib untuk a11y kecuali decorative."},
			{"Landmark `<nav>`?", "d", [4]string{"Footer", "Navigation links section", "Article body", "Sidebar ads"}, "nav untuk blok navigasi."},
			{"`required` pada input?", "a", [4]string{"HTML5 constraint validation", "Server-only validation", "CSS pseudo", "React prop khusus"}, "required adalah built-in constraint validation."},
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
			"Bug paling mungkin pada kode ini?",
			"b",
			[4]string{"Key tidak perlu", "Stale closure — count selalu 0", "useState ilegal", "JSX error"},
			"onClick menangkap count dari render saat handler dibuat jika tidak functional update.",
			"function Counter() {\n  const [count, setCount] = useState(0);\n  const log = () => console.log(count);\n  useEffect(() => {\n    const id = setInterval(log, 1000);\n    return () => clearInterval(id);\n  }, []);\n}",
			[]string{"react", "hooks"},
			"react-stale-closure",
		},
		{
			"Output urutan log?",
			"c",
			[4]string{"A B C", "B A C", "A C B", "C B A"},
			"await menunggu Promise; sync log A, microtask/settled B, lalu C setelah await.",
			"async function f() {\n  console.log('A');\n  await Promise.resolve();\n  console.log('C');\n}\nf();\nconsole.log('B');",
			[]string{"javascript"},
			"js-async-order",
		},
		{
			"Masalah performa pada list besar?",
			"a",
			[4]string{"Re-render seluruh list saat satu item berubah tanpa memo/virtualisasi", "Tidak pakai class", "Tidak pakai CSS", "Terlalu banyak div"},
			"Untuk list besar pertimbangkan virtualisasi, memo item, stable keys.",
			"{items.map(i => <Row key={i.id} data={i} onSelect={setSelected} />)}",
			[]string{"react"},
			"react-performance",
		},
		{
			"Specificity menang?",
			"d",
			[4]string{".btn", "button", "#save.btn", "inline style"},
			"Inline style mengalahkan selector biasa kecuali !important.",
			"button.primary { color: blue; }\n#save { color: red; }",
			[]string{"css"},
			"css-specificity",
		},
		{
			"Masalah a11y pada markup?",
			"b",
			[4]string{"Tidak ada masalah", "Icon button tanpa accessible name", "Terlalu banyak div", "Pakai section"},
			"Button icon-only butuh aria-label atau teks visually hidden.",
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
			"Kapan sebaiknya memakai `useReducer` dibanding beberapa `useState`?",
			"b",
			[4]string{"Selalu untuk performa", "State berikutnya bergantung pada state sebelumnya atau logic bercabang", "Hanya di class component", "Saat butuh Context"},
			"useReducer cocok untuk state kompleks dengan transisi terdefinisi.",
			map[string]string{"a": "useReducer bukan default untuk performa.", "c": "Function component modern mendukung keduanya.", "d": "Context untuk distribusi data, bukan pengganti reducer."},
			[]string{"react", "hooks"}, "medium", "react-usereducer", "",
		),
		fullMCQ(
			"Apa tujuan `useRef` yang paling umum di React?",
			"c",
			[4]string{"Mengganti useState", "Menyimpan CSS class", "Menyimpan nilai mutable / referensi DOM tanpa memicu re-render", "Fetch data"},
			"useRef mempertahankan nilai antar render tanpa re-render.",
			map[string]string{"a": "useRef tidak menggantikan state UI.", "b": "Bukan untuk styling.", "d": "Fetch data pakai useEffect atau library data."},
			[]string{"react", "hooks"}, "medium", "react-useref", "",
		),
		fullMCQ(
			"Apa yang salah jika menulis `if (loading) return <Spinner />` sebelum hooks lain?",
			"a",
			[4]string{"Melanggar Rules of Hooks — hooks harus dipanggil urutan sama tiap render", "Tidak ada masalah", "Hanya error di production", "Wajib pakai class component"},
			"Hooks tidak boleh setelah early return kondisional.",
			map[string]string{"b": "Ini pelanggaran rules of hooks.", "c": "Error juga di development.", "d": "Function component bisa pakai hooks dengan aturan ketat."},
			[]string{"react", "hooks"}, "hard", "react-rules-of-hooks", "",
		),
		fullMCQ(
			"Manakah yang benar tentang `useCallback`?",
			"d",
			[4]string{"Membuat komponen lebih cepat selalu", "Mengganti useMemo", "Menyimpan hasil komputasi", "Meng-cache definisi fungsi antar render jika dependency sama"},
			"useCallback mengembalikan fungsi yang sama referensinya saat deps tidak berubah.",
			map[string]string{"a": "Tidak selalu mempercepat — ada cost sendiri.", "b": "useMemo untuk nilai, useCallback untuk fungsi.", "c": "Itu deskripsi useMemo."},
			[]string{"react", "hooks"}, "medium", "react-usecallback", "",
		),
		fullMCQ(
			"Event delegation di DOM berarti?",
			"b",
			[4]string{"Setiap child punya listener sendiri", "Listener di parent menangani event dari child via bubbling", "Hanya untuk React", "Mengganti addEventListener"},
			"Delegation memanfaatkan fase bubbling untuk satu handler di ancestor.",
			map[string]string{"a": "Itu kebalikan delegation.", "c": "Konsep DOM native, bisa dipakai di React juga.", "d": "Tetap memakai addEventListener/onClick."},
			[]string{"javascript", "html"}, "medium", "js-event-delegation", "",
		),
		fullMCQ(
			"Apa beda `let` dan `var` dalam loop `for`?",
			"c",
			[4]string{"Sama persis", "var block-scoped", "let block-scoped, var function-scoped", "let dihoist seperti var"},
			"let per iterasi/block; var satu binding untuk seluruh function.",
			map[string]string{"a": "Perilaku closure di loop berbeda.", "b": "var function-scoped, bukan block.", "d": "let ada di TDZ, tidak dihoist seperti var."},
			[]string{"javascript"}, "medium", "js-let-var", "",
		),
		fullMCQ(
			"Apa hasil `JSON.stringify({a: undefined, b: 1})`?",
			"a",
			[4]string{"`{\"b\":1}`", "`{\"a\":null,\"b\":1}`", "throw", "`{}`"},
			"Properti undefined dihilangkan saat stringify object.",
			map[string]string{"b": "undefined bukan null di JSON.", "c": "Tidak throw untuk undefined property.", "d": "Property b tetap ada."},
			[]string{"javascript"}, "easy", "js-json", "",
		),
		fullMCQ(
			"Manakah selector dengan specificity tertinggi?",
			"c",
			[4]string{"div.card", "button.primary", "#submit.btn", "[type=submit]"},
			"ID selector (#submit) mengalahkan class dan element.",
			map[string]string{"a": "Satu element + satu class.", "b": "Element + class tanpa ID.", "d": "Attribute selector lebih rendah dari ID."},
			[]string{"css"}, "medium", "css-specificity-2", "",
		),
		fullMCQ(
			"`align-items: center` di flex container mengatur?",
			"b",
			[4]string{"Distribusi pada main axis", "Perataan pada cross axis", "Urutan flex item", "Wrap baris"},
			"align-items = cross axis; justify-content = main axis.",
			map[string]string{"a": "Itu justify-content.", "c": "Urutan pakai flex-direction/order.", "d": "Wrap pakai flex-wrap."},
			[]string{"css"}, "easy", "css-flex-align", "",
		),
		fullMCQ(
			"Properti CSS apa yang membuat elemen sticky saat scroll?",
			"d",
			[4]string{"position: fixed saja", "overflow: scroll", "z-index tinggi", "position: sticky + offset (top/bottom)"},
			"sticky butuh positioned sticky dan threshold top/bottom dalam container scroll.",
			map[string]string{"a": "fixed lepas dari flow; sticky relatif sampai threshold.", "b": "overflow tidak membuat sticky sendiri.", "c": "z-index tidak mengaktifkan sticky."},
			[]string{"css"}, "medium", "css-sticky", "",
		),
		fullMCQ(
			"Elemen HTML mana yang paling tepat untuk konten utama halaman?",
			"a",
			[4]string{"<main>", "<div id=\"main\">", "<section> saja", "<article> untuk seluruh halaman"},
			"<main> adalah landmark untuk konten utama — satu per halaman.",
			map[string]string{"b": "div tanpa landmark kurang aksesibel.", "c": "section untuk bagian tematik, bukan seluruh main content.", "d": "article untuk konten mandiri (post), bukan seluruh app."},
			[]string{"html"}, "easy", "html-main", "",
		),
		fullMCQ(
			"Atribut `aria-live` digunakan untuk?",
			"b",
			[4]string{"Styling fokus", "Memberi tahu screen reader konten dinamis yang berubah", "Validasi form", "Lazy load gambar"},
			"aria-live mengumumkan perubahan konten ke assistive tech.",
			map[string]string{"a": "Fokus pakai :focus-visible/tabIndex.", "c": "Validasi pakai constraint API atau JS.", "d": "Lazy load pakai loading=lazy."},
			[]string{"html"}, "hard", "html-aria-live", "",
		),
		fullMCQ(
			"Dalam React, mengapa `setCount(count + 1)` dua kali berturut tidak selalu +2?",
			"c",
			[4]string{"React bug", "Karena strict mode saja", "State update di-batch — baca nilai count yang sama", "Karena useState async ke server"},
			"Batching memakai snapshot count yang sama; pakai functional update untuk increment ganda.",
			map[string]string{"a": "Ini perilaku terdokumentasi.", "b": "Bukan hanya strict mode.", "d": "State lokal, bukan server roundtrip."},
			[]string{"react"}, "hard", "react-batching",
			"setCount(count + 1);\nsetCount(count + 1);",
		),
		fullMCQ(
			"Apa fungsi `key` pada komponen di luar list?",
			"d",
			[4]string{"Wajib di semua komponen", "SEO", "Styling", "Memaksa remount saat key berubah (reset state internal)"},
			"Mengubah key membuat React mount ulang subtree — berguna reset state.",
			map[string]string{"a": "Key hanya wajib di list siblings.", "b": "Bukan untuk SEO.", "c": "Bukan className pengganti."},
			[]string{"react"}, "medium", "react-key-remount", "",
		),
		fullMCQ(
			"`fetch().then(r => r.json())` — kapan body response bisa dibaca?",
			"a",
			[4]string{"Sekali — stream body habis setelah dibaca", "Berulang tanpa batas", "Hanya di Node", "Hanya jika status 200"},
			"Response body adalah stream sekali pakai.",
			map[string]string{"b": "Perlu clone() untuk baca ulang.", "c": "Browser dan Node sama.", "d": "Body bisa dibaca meski 4xx/5xx."},
			[]string{"javascript"}, "medium", "js-fetch", "",
		),
		fullMCQ(
			"Apa output `[1, 2, 3].map(parseInt)` di JavaScript?",
			"c",
			[4]string{"[1, 2, 3]", "[1, NaN, NaN]", "[1, 2, 2]", "[NaN, NaN, NaN]"},
			"parseInt menerima (string, radix); map mengirim (value, index) sebagai argumen.",
			map[string]string{"a": "parseInt('2', 1) bukan 2.", "b": "Index 2 dengan radix 2 menghasilkan 2.", "d": "Elemen pertama tetap 1."},
			[]string{"javascript"}, "hard", "js-parseint-map", "",
		),
		fullMCQ(
			"CSS `min-height: 100vh` pada mobile kadang terlalu tinggi karena?",
			"b",
			[4]string{"vh tidak didukung", "Address bar browser mengubah viewport — pertimbangkan dvh/svh", "Karena flexbox", "Karena rem"},
			"Classic vh tidak selalu match visible viewport di mobile browser.",
			map[string]string{"a": "vh didukung luas.", "c": "Tidak spesifik flexbox.", "d": "rem terkait font root."},
			[]string{"css"}, "medium", "css-viewport-units", "",
		),
		fullMCQ(
			"Grid `grid-template-areas` berguna untuk?",
			"a",
			[4]string{"Menamai area layout dan menempatkan item ke area", "Animasi", "Font sizing", "Z-index"},
			"Named areas membuat layout dashboard mudah dibaca.",
			map[string]string{"b": "Animasi pakai @keyframes.", "c": "Font pakai font-size.", "d": "Z-index terpisah."},
			[]string{"css"}, "medium", "css-grid-areas", "",
		),
		fullMCQ(
			"Form `novalidate` berarti?",
			"c",
			[4]string{"Form tidak bisa disubmit", "Hanya validasi server", "Nonaktifkan validasi HTML5 bawaan browser", "Hapus required"},
			"novalidate mematikan built-in constraint validation — validasi JS tetap bisa.",
			map[string]string{"a": "Submit tetap bisa.", "b": "Bukan otomatis server-only.", "d": "required tetap ada di markup."},
			[]string{"html"}, "medium", "html-novalidate", "",
		),
		fullMCQ(
			"Manakah pola validasi form React yang scalable?",
			"d",
			[4]string{"Satu useState object besar tanpa struktur", "Simpan semua di window", "Hanya alert()", "Schema validation (zod/yup) + library form atau reducer terstruktur"},
			"Form besar butuh schema, error per field, dan pemisahan concern.",
			map[string]string{"a": "Sulit di-maintain dan test.", "b": "Anti-pattern global state.", "c": "alert tidak aksesibel/UX buruk."},
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
		BestPractices:     []string{"Pahami konsep dasar sebelum menghafal jawaban", "Relasikan dengan pengalaman coding harian", "Baca penjelasan setelah submit untuk pembelajaran"},
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

var buildRubric = json.RawMessage(`{"criteria":[{"id":"tests_pass","label":"Kriteria fungsional terpenuhi","points":40,"auto":true},{"id":"controlled","label":"Controlled inputs","points":0,"auto":false},{"id":"validation","label":"Validasi sesuai instruksi","points":0,"auto":false}]}`)

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
	return fmt.Sprintf(`## Tujuan
Latihan form React: controlled input, validasi, dan submit lewat callback.

## Buat komponen %s
- Gunakan **controlled input** untuk field %s (value + onChange)
- **Validasi:** %s
- Ada tombol **Submit**
- Jika invalid, tampilkan **pesan error** (elemen dengan role="alert")
- Jika valid, panggil prop **onSubmit** dengan object yang berisi %s

## Cara cek
Klik **Jalankan test** — solusi tidak harus sama persis dengan referensi, asal memenuhi kriteria di atas.`,
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
	case strings.Contains(validate, "mengandung https"):
		checks = append(checks, assertionCase{Check: "validates_includes", Field: field, Substring: "https"})
	case strings.Contains(validate, "mengandung @"):
		checks = append(checks, assertionCase{Check: "validates_includes", Field: field, Substring: "@"})
	case strings.Contains(validate, "minimal 6"):
		checks = append(checks, assertionCase{Check: "validates_min_length", Field: field, Min: 6})
	case strings.Contains(validate, "minimal 5"):
		checks = append(checks, assertionCase{Check: "validates_min_length", Field: field, Min: 5})
	case strings.Contains(validate, "minimal 4"):
		checks = append(checks, assertionCase{Check: "validates_min_length", Field: field, Min: 4})
	case strings.Contains(validate, "minimal 2"):
		checks = append(checks, assertionCase{Check: "validates_min_length", Field: field, Min: 2})
	case strings.Contains(validate, "minimal 3"):
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
      setError("Maksimal 200 karakter");
      return;
    }`
	case strings.Contains(validate, "max 120"):
		return `    if (value.length > 120) {
      setError("Maksimal 120 karakter");
      return;
    }`
	case strings.Contains(validate, "max 80"):
		return `    if (value.length > 80) {
      setError("Maksimal 80 karakter");
      return;
    }`
	case strings.Contains(validate, "mengandung https"):
		return `    if (!value.includes("https")) {
      setError("URL harus mengandung https");
      return;
    }`
	case strings.Contains(validate, "mengandung @"):
		return `    if (!value.includes("@")) {
      setError("Email harus mengandung @");
      return;
    }`
	case strings.Contains(validate, "minimal 6"):
		return `    if (value.length < 6) {
      setError("Minimal 6 karakter");
      return;
    }`
	case strings.Contains(validate, "minimal 5"):
		return `    if (value.trim().length < 5) {
      setError("Minimal 5 karakter");
      return;
    }`
	case strings.Contains(validate, "minimal 4"):
		return `    if (value.trim().length < 4) {
      setError("Minimal 4 karakter");
      return;
    }`
	case strings.Contains(validate, "minimal 2"):
		return `    if (value.trim().length < 2) {
      setError("Minimal 2 karakter");
      return;
    }`
	case strings.Contains(validate, "minimal 3"):
		return `    if (value.trim().length < 3) {
      setError("Minimal 3 karakter");
      return;
    }`
	default:
		return `    if (!value.trim()) {
      setError("Field wajib diisi");
      return;
    }`
	}
}

func generateBuilds() []buildTask {
	specs := []struct {
		title, name, field, diff string
		validate                 string
	}{
		{"WaitlistForm", "email", "email", "medium", "email harus mengandung @"},
		{"NewsletterSignup", "email", "email", "easy", "email wajib diisi"},
		{"ContactForm", "name", "name", "medium", "name minimal 2 karakter"},
		{"LoginForm", "password", "password", "medium", "password minimal 6 karakter"},
		{"SearchBar", "query", "query", "easy", "tampilkan query di bawah input"},
		{"FeedbackForm", "message", "message", "medium", "message tidak boleh kosong"},
		{"RegisterForm", "email", "email", "hard", "email valid + password match"},
		{"SubscribeForm", "email", "email", "easy", "checkbox consent wajib dicentang"},
		{"ProfileForm", "displayName", "displayName", "medium", "displayName wajib"},
		{"BookingForm", "date", "date", "medium", "date input type=date"},
		{"CouponForm", "code", "code", "easy", "kode minimal 3 karakter"},
		{"AddressForm", "city", "city", "medium", "city wajib diisi"},
		{"PhoneVerifyForm", "phone", "phone", "hard", "phone hanya angka"},
		{"RatingForm", "rating", "rating", "easy", "select rating 1-5"},
		{"CommentForm", "comment", "comment", "medium", "max 200 karakter"},
		{"InviteForm", "inviteEmail", "inviteEmail", "medium", "email teman"},
		{"ResetPasswordForm", "newPassword", "newPassword", "hard", "konfirmasi password sama"},
		{"JobApplyForm", "linkedin", "linkedin", "medium", "URL linkedin opsional valid"},
		{"EventRSVPForm", "guests", "guests", "easy", "jumlah tamu number >=1"},
		{"SupportTicketForm", "subject", "subject", "medium", "subject wajib"},
		{"CheckoutEmailForm", "checkoutEmail", "checkoutEmail", "medium", "email sebelum lanjut"},
		{"BetaAccessForm", "reason", "reason", "hard", "alasan min 20 karakter"},
		{"MailingListForm", "firstName", "firstName", "easy", "firstName wajib"},
		{"SurveyForm", "satisfaction", "satisfaction", "medium", "radio satisfaction"},
		{"DemoRequestForm", "company", "company", "medium", "company wajib"},
		{"PartnerForm", "website", "website", "hard", "website harus http"},
		{"AlertSignupForm", "topic", "topic", "easy", "pilih topic dari select"},
		{"GiftCardForm", "amount", "amount", "medium", "amount number > 0"},
		{"ReferralForm", "referralCode", "referralCode", "medium", "kode referral"},
		{"OnboardingForm", "role", "role", "easy", "pilih role developer/designer"},
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
      <button type="submit">Kirim</button>
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
      <button type="submit">Kirim</button>
    </form>
  );
}
`, cmp, validation, s.field, testID, s.field, s.field, s.field, s.field),
			SolutionExplanation: "Controlled input, validasi sesuai instruksi, error dengan role=alert, onSubmit dipanggil saat valid.",
			RubricJSON:          buildRubric,
			TestCases:           buildAssertions(s.field, s.validate, testID),
			BestPractices:       []string{"Controlled components", "Validasi sebelum submit", "Label htmlFor untuk a11y", "preventDefault pada form submit"},
			CommonMistakes:        []string{"Lupa preventDefault", "Uncontrolled input", "Tidak tampilkan error"},
			LearningObjective:   fmt.Sprintf("Form React — %s", s.title),
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
			"Halaman hang saat Hero mount.",
			"setState dipanggil langsung di body render.",
			"Hapus setState dari render; gunakan event handler atau useEffect dengan deps benar.",
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
			"Subtitle tidak update saat prop berubah.",
			"State diinisialisasi dari props sekali saja tanpa sync.",
			"Render langsung dari props atau sync dengan useEffect saat prop berubah.",
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
			"List CTA salah urutan setelah filter.",
			"Index sebagai key menyebabkan reconciler salah reuse DOM.",
			"Gunakan id stabil dari data sebagai key.",
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
			BestPractices:     []string{"Jangan setState saat render", "Key stabil pada list", "React DevTools Profiler"},
			CommonMistakes:      []string{"Menambah if tanpa paham render cycle", "Menghapus state yang dibutuhkan UI"},
			LearningObjective: fmt.Sprintf("Debug React Hero — sample %02d", i+1),
			Difficulty:        b.diff,
			Points:            35,
		})
	}
	return out
}

func debugSpecMarkdown(title, symptom, hint string) string {
	base := strings.Split(title, " #")[0]
	return fmt.Sprintf(`## Gejala
%s

## Tugas kamu
Perbaiki komponen **%s** di editor sampai preview berjalan benar.

## Petunjuk
%s

## Cara cek
Klik **Jalankan test** — kode tidak harus sama persis dengan solusi referensi.`,
		symptom, base, hint)
}

func debugHint(kind int) string {
	switch kind {
	case 0:
		return "Perhatikan apakah ada setState yang dipanggil saat render, bukan di event handler."
	case 1:
		return "Perhatikan apakah state dari props ikut berubah saat parent mengirim prop baru."
	default:
		return "Perhatikan cara me-render list `items` — React butuh key yang stabil."
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
