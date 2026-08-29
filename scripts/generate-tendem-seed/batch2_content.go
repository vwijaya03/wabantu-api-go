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
		{"`useLayoutEffect` berbeda dari `useEffect` karena?", "b", [4]string{"Hanya di server", "Jalan sinkron setelah DOM mutate, sebelum paint", "Tidak bisa cleanup", "Hanya class"}, "useLayoutEffect cocok mengukur DOM sebelum user melihat flicker.", "medium"},
		{"Portal React (`createPortal`) berguna untuk?", "a", [4]string{"Styling global", "Render subtree ke DOM node di luar parent (modal)", "Ganti router", "Memoization"}, "Portal memutus overflow/z-index parent untuk overlay.", "medium"},
		{"`forwardRef` dipakai ketika?", "c", [4]string{"Semua komponen", "Hanya class", "Parent perlu akses DOM ref child", "Ganti props"}, "forwardRef meneruskan ref ke elemen di dalam komponen.", "medium"},
		{"Strict Mode di dev membantu menemukan?", "d", [4]string{"CSS bug", "Network error", "SEO issue", "Side effect tidak aman / double invoke"}, "React 18 Strict Mode double-invoke effects di dev.", "medium"},
		{"Controlled vs uncontrolled input?", "b", [4]string{"Sama", "Controlled: value dari state React", "Uncontrolled lebih aman", "Hanya di form HTML"}, "Controlled = single source of truth di React state.", "easy"},
		{"Kapan `key` pada list harus stabil?", "a", [4]string{"Saat list bisa reorder/filter", "Tidak pernah", "Hanya di production", "Hanya string"}, "Key stabil menjaga identitas item antar render.", "easy"},
		{"`children` sebagai function (render prop) untuk?", "c", [4]string{"SEO", "Styling", "Delegasi rendering ke parent", "Database"}, "Pattern render props membagikan logic UI.", "hard"},
		{"Error di event handler ditangani dengan?", "b", [4]string{"Error boundary otomatis", "try/catch atau state error lokal", "Suspense", "Portal"}, "Error boundary tidak menangkap event handler errors.", "medium"},
		{"`lazy()` + `Suspense` untuk?", "d", [4]string{"State global", "CSS", "Form validation", "Code-splitting komponen async"}, "lazy memuat komponen dinamis; Suspense fallback loading.", "medium"},
		{"Reconciliation React mengacu pada?", "a", [4]string{"Proses diff virtual DOM", "HTTP cache", "CSS layout", "npm install"}, "Reconciler membandingkan tree lama vs baru.", "hard"},
		{"`useId` (React 18) berguna untuk?", "c", [4]string{"Random key list", "UUID database", "ID stabil SSR/hydration untuk a11y", "Routing"}, "useId menghindari mismatch id server/client.", "medium"},
		{"Anti-pattern: `useEffect` fetch tanpa cleanup abort?", "b", [4]string{"Best practice", "Race condition saat unmount/fast re-fetch", "Wajib", "Hanya di mobile"}, "AbortController mencegah setState pada unmounted component.", "hard"},
		{"`memo` + `useCallback` paling masuk akal ketika?", "d", [4]string{"Selalu", "File kecil", "Tanpa profiler", "Child mahal re-render dan props stabil"}, "Optimasi prematur bisa menambah kompleksitas.", "hard"},
		{"Fragment dengan key ditulis?", "a", [4]string{"`<React.Fragment key={id}>`", "`<key>`", "`#fragment`", "Tidak bisa"}, "Keyed fragment untuk list of fragments.", "medium"},
		{"Hydration mismatch sering disebabkan?", "c", [4]string{"CSS", "useMemo", "HTML berbeda server vs client", "import CSS"}, "Contoh: Date.now() atau random di render awal.", "hard"},
		{"`defaultValue` pada uncontrolled input?", "b", [4]string{"Sync tiap render", "Nilai awal saja", "Sama dengan value", "Wajib di TS"}, "defaultValue tidak mengontrol subsequent updates.", "easy"},
		{"Context value object inline `value={{a,b}}` masalahnya?", "d", [4]string{"Tidak ada", "Lebih cepat", "SEO", "Referensi baru tiap render memicu re-render consumer"}, "Memoize value object atau pisah context.", "medium"},
		{"`useRef` untuk menyimpan interval ID karena?", "a", [4]string{"Mutable tanpa re-render", "Lebih cepat dari state", "Wajib hooks rules", "SSR"}, "Ref cocok nilai yang tidak perlu trigger UI update.", "medium"},
	}
	for _, it := range react {
		add(it.q, it.c, it.o, it.e, it.d, "react", "react-v2")
	}

	js := []struct {
		q, c string
		o    [4]string
		e, d string
	}{
		{"`Array.prototype.find` mengembalikan?", "b", [4]string{"Index", "Elemen pertama yang match atau undefined", "Array baru", "Boolean"}, "find berhenti di elemen pertama yang lolos predicate.", "easy"},
		{"`Object.freeze` melakukan?", "c", [4]string{"Deep immutable", "Hanya array", "Shallow freeze properti", "Clone"}, "freeze shallow — nested object masih bisa diubah.", "medium"},
		{"`??` (nullish coalescing) vs `||`?", "a", [4]string{"?? hanya null/undefined", "Sama", "|| lebih ketat", "?? untuk string kosong"}, "|| menganggap '' dan 0 falsy; ?? tidak.", "medium"},
		{"`Promise.allSettled` berguna ketika?", "d", [4]string{"Satu gagal cancel semua", "Sync loop", "DOM", "Butuh hasil semua promise meski ada yang reject"}, "allSettled tidak short-circuit pada rejection.", "medium"},
		{"TDZ (Temporal Dead Zone) terkait?", "b", [4]string{"var", "let/const sebelum deklarasi", "function", "import"}, "Akses let/const sebelum line deklarasi throw.", "hard"},
		{"`structuredClone` untuk?", "c", [4]string{"JSON saja", "Shallow copy", "Deep clone built-in (terbatas)", "Immutable.js"}, "structuredClone deep clone di browser modern.", "medium"},
		{"Event loop: setTimeout(0) vs Promise.then?", "a", [4]string{".then microtask lebih dulu", "setTimeout selalu dulu", "Random", "Parallel"}, "Microtask queue sebelum macrotask berikutnya.", "hard"},
		{"`in` operator pada object mengecek?", "b", [4]string{"Nilai", "Keberadaan key (termasuk prototype chain)", "Tipe", "Length"}, "Object.hasOwn lebih aman untuk own property.", "medium"},
		{"`fetch` credentials 'include'?", "d", [4]string{"Tanpa cookie", "Hanya POST", "CORS tidak perlu", "Kirim cookie cross-origin jika server allow"}, "Credentials mode untuk session cookie.", "hard"},
		{"Debounce vs throttle?", "c", [4]string{"Sama", "Throttle tunggu idle", "Debounce tunggu pause; throttle batasi rate", "Hanya UI"}, "Debounce: search input; throttle: scroll.", "medium"},
		{"`Map` vs plain object untuk key?", "a", [4]string{"Map boleh key non-string", "Object lebih cepat selalu", "Map tidak iterable", "Sama"}, "Map menjaga insertion order dan key arbitrary.", "medium"},
		{"`Array.sort` default membandingkan?", "b", [4]string{"Number numerik", "String unicode", "Random", "Length"}, "Sort default string — beri compareFn untuk angka.", "easy"},
		{"IIFE `(function(){})()` dipakai untuk?", "d", [4]string{"Import", "Class", "Hook", "Scope terisolasi / hindari pollute global"}, "Pattern lama sebelum modules.", "easy"},
		{"`Symbol` di JS untuk?", "c", [4]string{"Math", "CSS", "Unique property key", "Async"}, "Symbol.uniq untuk key yang tidak bentrok.", "medium"},
		{"`WeakMap` key harus?", "a", [4]string{"Object (garbage collectible)", "String", "Number", "Symbol saja"}, "WeakMap tidak mencegah GC key object.", "hard"},
		{"`try/finally` tanpa catch?", "b", [4]string{"Illegal", "Legal — finally tetap jalan", "Hanya async", "Hanya TS"}, "finally jalan meski return di try.", "medium"},
		{"Template literal tagged function?", "d", [4]string{"CSS only", "SQL injection", "JSON", "Custom processing string parts"}, "Tagged templates untuk i18n/styled patterns.", "hard"},
		{"`Object.entries` mengembalikan?", "c", [4]string{"Keys saja", "Values saja", "Array [key,value] pairs", "Map"}, "entries untuk iterasi object enumerable.", "easy"},
	}
	for _, it := range js {
		add(it.q, it.c, it.o, it.e, it.d, "javascript", "js-v2")
	}

	css := []struct {
		q, c string
		o    [4]string
		e, d string
	}{
		{"`display: grid` vs `flex`?", "a", [4]string{"Grid dua dimensi, flex satu dimensi utama", "Sama", "Flex untuk table", "Grid tidak responsive"}, "Pilih flex untuk baris/kolom tunggal; grid untuk layout 2D.", "easy"},
		{"`fr` unit di CSS Grid?", "b", [4]string{"Font relative", "Fraction sisa ruang track", "Frame rate", "Rem"}, "1fr = bagian proporsional ruang tersedia.", "medium"},
		{"`position: sticky` perlu?", "c", [4]string{"z-index saja", "fixed parent", "Offset + ancestor scroll + tidak overflow hidden parent", "display flex"}, "Parent overflow:hidden bisa mematikan sticky.", "medium"},
		{"`clamp(min, pref, max)` untuk?", "d", [4]string{"Animation", "Grid", "Print", "Fluid typography/spacing terbatas"}, "clamp responsif dengan batas min/max.", "medium"},
		{"Specificity: inline style vs #id?", "b", [4]string{"#id menang", "Inline menang kecuali !important id", "Sama", "class menang"}, "Inline 1,0,0,0 — id 0,1,0,0.", "medium"},
		{"`aspect-ratio` property?", "a", [4]string{"Menjaga rasio lebar/tinggi", "Font size", "Grid gap", "Flex order"}, "Berguna video/card responsif.", "easy"},
		{"`@layer` di CSS untuk?", "c", [4]string{"Animation", "Font", "Mengatur urutan cascade layer", "Media query"}, "Layer mengontrol precedence tanpa specificity war.", "hard"},
		{"`contain: layout` membantu?", "d", [4]string{"SEO", "Font", "Color", "Isolasi layout subtree performa"}, "Contain membatasi reflow ke subtree.", "hard"},
		{"`logical` properties (`margin-inline`)?", "b", [4]string{"Print only", "Mengikuti writing mode LTR/RTL", "Grid only", "Deprecated"}, "logical props untuk i18n layout.", "medium"},
		{"`prefers-reduced-motion`?", "a", [4]string{"Media query a11y kurangi animasi", "Dark mode", "Print", "Hover"}, "Hormati preferensi user motion sensitivity.", "easy"},
		{"Flex `gap` vs margin pada item?", "c", [4]string{"Margin lebih modern", "Sama", "gap spacing antar item tanpa margin collapse", "gap hanya grid"}, "gap lebih bersih untuk flex/grid spacing.", "easy"},
		{"`object-fit: cover` pada img?", "b", [4]string{"Stretch distort", "Crop isi box menjaga ratio", "Contain blur", "SVG only"}, "cover fill container crop center.", "easy"},
		{"`::before` pseudo-element default `display`?", "d", [4]string{"block selalu", "inline selalu", "none", "inline — perlu content & sering di-set block"}, "Perlu property content untuk tampil.", "medium"},
		{"`minmax(200px, 1fr)` di grid?", "a", [4]string{"Track min 200px max sisa", "Fixed 200", "Hanya max", "Flex only"}, "minmax fleksibel track sizing.", "medium"},
		{"`will-change` anti-pattern jika?", "c", [4]string{"Hover kecil", "Transform animasi", "Diterapkan permanen ke banyak elemen", "GPU layer sekali"}, "Overuse will-change boros memori.", "hard"},
		{"`color-scheme` pada root?", "b", [4]string{"Font color", "Memberi hint browser dark/light UI chrome", "Grid", "Flex"}, "Mempengaruhi scrollbar/form native theming.", "medium"},
		{"`subgrid` (CSS Grid)?", "d", [4]string{"Flex feature", "Deprecated", "Table only", "Child grid inherit track parent"}, "subgrid align nested grid ke parent tracks.", "hard"},
	}
	for _, it := range css {
		add(it.q, it.c, it.o, it.e, it.d, "css", "css-v2")
	}

	html := []struct {
		q, c string
		o    [4]string
		e, d string
	}{
		{"`<button>` di dalam `<form>` default type?", "b", [4]string{"button", "submit", "reset", "menu"}, "Tanpa type eksplisit, button = submit.", "easy"},
		{"`<label for>` harus match?", "a", [4]string{"id input terkait", "name", "class", "type"}, "for/id menghubungkan label ke control.", "easy"},
		{"`<dialog>` element native?", "c", [4]string{"Tidak ada", "Hanya Safari", "Modal dialog HTML dengan showModal()", "React only"}, "dialog + ::backdrop untuk modal native.", "medium"},
		{"`loading=\"lazy\"` pada img?", "b", [4]string{"SEO", "Defer load sampai dekat viewport", "CDN", "Blur"}, "Native lazy loading gambar.", "easy"},
		{"Landmark `<main>` sebaiknya?", "d", [4]string{"Banyak per halaman", "Di footer", "Di nav", "Satu per halaman untuk konten utama"}, "Satu main landmark per dokumen.", "easy"},
		{"`aria-expanded` pada accordion?", "a", [4]string{"State buka/tutup untuk AT", "Styling", "Focus", "Tab order"}, "Screen reader tahu panel terbuka.", "medium"},
		{"`<input type=\"number\">` caveat?", "c", [4]string{"Selalu integer", "Tidak ada step", "Bisa spinner & locale quirks — validasi tambahan", "Tidak di mobile"}, "number input bukan pengganti validasi bisnis.", "medium"},
		{"`<details>/<summary>` untuk?", "b", [4]string{"Modal", "Disclosure widget tanpa JS", "Table", "Form"}, "Native expand/collapse konten.", "easy"},
		{"`tabindex=\"0\"`?", "d", [4]string{"Hapus dari tab order", "Hanya mouse", "Negative focus trap", "Masuk natural tab order"}, "tabindex -1 programmatic focus only.", "medium"},
		{"`role=\"alert\"`?", "a", [4]string{"Live region urgent untuk error", "Button", "Link", "Heading"}, "Pesan error form sebaiknya role alert.", "easy"},
		{"`<meta viewport>` mobile?", "b", [4]string{"SEO", "width=device-width initial-scale=1", "HTTPS", "PWA"}, "Viewport meta untuk responsive mobile.", "easy"},
		{"`<picture>` + `<source>` untuk?", "c", [4]string{"Video", "Font", "Art direction / format gambar responsif", "SVG"}, "picture untuk responsive images art direction.", "medium"},
		{"Skip link accessibility?", "d", [4]string{"SEO", "CSS", "Analytics", "Link ke main content untuk keyboard user"}, "Skip nav ke konten utama.", "medium"},
		{"`autocomplete` attribute pada form?", "a", [4]string{"Bantu browser isi ulang field benar", "Validation", "CSRF", "Routing"}, "autocomplete=\"email\" dll untuk UX.", "easy"},
		{"`<fieldset>` + `<legend>`?", "b", [4]string{"Table", "Kelompok form dengan judul aksesibel", "Modal", "Grid"}, "fieldset mengelompokkan radio/checkbox.", "easy"},
		{"`hidden` attribute vs `display:none` CSS?", "c", [4]string{"Sama selalu", "hidden tidak semantik", "hidden tidak boleh ditampilkan & tidak relevan AT", "hidden untuk SEO"}, "hidden=until-found di HTML baru untuk find-in-page.", "medium"},
		{"`<template>` content?", "d", [4]string{"Render langsung", "SEO utama", "SSR only", "Inert sampai di-clone ke DOM via JS"}, "template untuk fragment tidak aktif.", "hard"},
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
		{"UsernameForm", "username", "easy", "username minimal 3 karakter"},
		{"OTPForm", "otpCode", "medium", "kode OTP minimal 6 digit"},
		{"PinForm", "pin", "medium", "PIN minimal 4 digit"},
		{"BioForm", "bio", "easy", "bio max 120 karakter"},
		{"WebsiteForm", "siteUrl", "medium", "URL wajib mengandung https"},
		{"AgeForm", "age", "easy", "usia wajib diisi"},
		{"ZipCodeForm", "zipCode", "medium", "kode pos minimal 5 digit"},
		{"TeamNameForm", "teamName", "easy", "nama tim wajib diisi"},
		{"TaglineForm", "tagline", "medium", "tagline max 80 karakter"},
		{"BudgetForm", "budget", "hard", "budget wajib diisi"},
		{"DeadlineForm", "dueDate", "medium", "tanggal wajib diisi"},
		{"PriorityForm", "priority", "easy", "priority wajib diisi"},
		{"ChannelForm", "channel", "medium", "channel wajib diisi"},
		{"LocaleForm", "locale", "easy", "locale wajib diisi"},
		{"NicknameForm", "nickname", "medium", "nickname minimal 3 karakter"},
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
			CommonMistakes:      []string{"Lupa preventDefault", "Uncontrolled input", "Tidak tampilkan error"},
			LearningObjective:   fmt.Sprintf("Form React — %s", s.title),
			Difficulty:          s.diff,
			Points:              40,
		})
	}
	return out
}
