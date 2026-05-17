package business

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	"encore.dev/rlog"

	apperr "encore.app/wabantu/shared/errs"
)

const (
	maxHTMLChars     = 600_000
	maxResponseBytes = 1_500_000
	fetchTimeout     = 14 * time.Second
	maxRedirects     = 4
	crawlUserAgent   = "WABantuWebsiteImport/1.0 (+https://wabantu)"
)

var fieldLimits = map[string]int{
	"businessName": 200, "description": 2000, "address": 500,
	"openingHours": 500, "productsServices": 2000,
	"basePricing": 500, "deliveryArea": 500,
}

var blockedHosts = map[string]bool{
	"localhost": true, "127.0.0.1": true, "0.0.0.0": true, "::1": true,
	"metadata.google.internal": true, "metadata.google.internal.": true,
	"kubernetes.default": true, "kubernetes.default.svc": true,
	"kubernetes.default.svc.cluster.local": true,
}

// ---------------------------------------------------------------------------
// Entry point
// ---------------------------------------------------------------------------

func previewFromURL(ctx context.Context, rawURL, anthropicKey string) (*ImportPreviewResponse, error) {
	rawURL = strings.TrimSpace(rawURL)
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Host == "" {
		return nil, apperr.BadRequest("URL tidak valid")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, apperr.BadRequest("Hanya URL http(s) yang didukung")
	}
	if parsed.User != nil {
		return nil, apperr.BadRequest("URL dengan kredensial tidak didukung")
	}
	if err := assertURLSafe(parsed); err != nil {
		return nil, err
	}

	html, finalURL, ct, err := fetchHTMLChain(ctx, parsed)
	if err != nil {
		return nil, err
	}
	ctLow := strings.ToLower(ct)
	if !strings.Contains(ctLow, "text/html") && !strings.Contains(ctLow, "application/xhtml+xml") {
		return nil, apperr.BadRequest("Halaman bukan HTML (periksa apakah URL mengarah ke dokumen / API).")
	}

	pageTitle := extractDocumentTitle(html)
	ogTitle, ogDesc, metaDesc := extractOGAndMeta(html)
	visibleText := htmlToVisibleText(html)
	notes := []string{}

	fields := heuristicFields(ogTitle, ogDesc, metaDesc, pageTitle)

	usedLLM := false
	if anthropicKey != "" {
		llm, llmErr := extractWithAnthropic(ctx, anthropicKey, finalURL, strCoalesce(ogTitle, pageTitle), visibleText)
		if llmErr != nil {
			rlog.Warn("anthropic extraction failed", "err", llmErr)
			notes = append(notes, "Model AI tidak tersedia atau gagal; dipakai heuristik judul/meta.")
		} else {
			fields = mergePreferLLM(fields, llm)
			notes = append(notes, "Ringkasan field diproses dengan model AI dari teks halaman.")
			usedLLM = true
		}
	} else {
		notes = append(notes, "Model AI tidak tersedia atau gagal; dipakai heuristik judul/meta.")
	}

	fields = clampFields(fields)
	valid, invalidReason, confidence := evaluatePreview(fields, len(visibleText), usedLLM)

	if fp, _ := url.Parse(finalURL); fp != nil {
		for _, h := range marketplaceCrawlHints(fp.Hostname()) {
			notes = append(notes, h)
		}
	}

	return &ImportPreviewResponse{
		Valid:         valid,
		InvalidReason: invalidReason,
		SourceURL:     parsed.String(),
		FinalURL:      finalURL,
		PageTitle:     strCoalesce(ogTitle, pageTitle),
		Confidence:    confidence,
		Fields:        fields,
		Notes:         notes,
	}, nil
}

// ---------------------------------------------------------------------------
// SSRF guard
// ---------------------------------------------------------------------------

func assertURLSafe(u *url.URL) error {
	host := normalizeHost(u.Hostname())
	if host == "" {
		return apperr.BadRequest("Host tidak valid")
	}
	if blockedHosts[host] || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".onion") {
		return apperr.BadRequest("Host tidak diizinkan.")
	}

	if ip := net.ParseIP(host); ip != nil {
		if isPrivateIP(ip) {
			return apperr.BadRequest("Alamat IP tidak diizinkan.")
		}
		return nil
	}
	return assertDNSPublic(host)
}

func assertDNSPublic(hostname string) error {
	ips, err := net.LookupIP(hostname)
	if err != nil || len(ips) == 0 {
		return apperr.BadRequest("DNS host tidak dapat di-resolve.")
	}
	for _, ip := range ips {
		if isPrivateIP(ip) {
			return apperr.BadRequest("Host meresolve ke jaringan privat atau diblokir demi keamanan.")
		}
	}
	return nil
}

func isPrivateIP(ip net.IP) bool {
	if v4 := ip.To4(); v4 != nil {
		a, b := v4[0], v4[1]
		switch {
		case a == 10, a == 127, a == 0:
			return true
		case a == 169 && b == 254:
			return true
		case a == 192 && b == 168:
			return true
		case a == 172 && b >= 16 && b <= 31:
			return true
		case a == 100 && b >= 64 && b <= 127:
			return true
		case a == 255 && b == 255:
			return true
		}
		return false
	}
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return true
	}
	if len(ip) == net.IPv6len && (ip[0] == 0xfc || ip[0] == 0xfd) {
		return true
	}
	return false
}

func normalizeHost(hostname string) string {
	return strings.TrimSuffix(strings.ToLower(hostname), ".")
}

// ---------------------------------------------------------------------------
// HTML fetch with manual redirect following
// ---------------------------------------------------------------------------

func fetchHTMLChain(ctx context.Context, start *url.URL) (html, finalURL, contentType string, err error) {
	client := &http.Client{
		Timeout: fetchTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12},
		},
	}

	current := start
	for hop := 0; hop <= maxRedirects; hop++ {
		req, reqErr := http.NewRequestWithContext(ctx, http.MethodGet, current.String(), nil)
		if reqErr != nil {
			return "", "", "", apperr.BadRequest("URL tidak valid")
		}
		req.Header.Set("User-Agent", crawlUserAgent)
		req.Header.Set("Accept", "text/html,application/xhtml+xml;q=0.9,*/*;q=0.1")
		req.Header.Set("Accept-Language", "id,en;q=0.9")

		resp, doErr := client.Do(req)
		if doErr != nil {
			rlog.Warn("fetch failed", "host", current.Hostname(), "err", doErr)
			return "", "", "", apperr.Unavailable("Tidak dapat mengambil halaman (timeout, DNS, atau server menolak).")
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, int64(maxResponseBytes)))
		resp.Body.Close()

		if resp.StatusCode >= 300 && resp.StatusCode < 400 {
			loc := resp.Header.Get("Location")
			if loc == "" || hop == maxRedirects {
				return "", "", "", apperr.BadRequest("Redirect tidak valid atau terlalu panjang.")
			}
			next, parseErr := url.Parse(loc)
			if parseErr != nil {
				return "", "", "", apperr.BadRequest("URL redirect tidak valid.")
			}
			current = current.ResolveReference(next)
			if ssrfErr := assertURLSafe(current); ssrfErr != nil {
				return "", "", "", ssrfErr
			}
			continue
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return "", "", "", apperr.BadRequest(fmt.Sprintf("Server mengembalikan status HTTP %d.", resp.StatusCode))
		}

		htmlStr := string(body)
		if len(htmlStr) > maxHTMLChars {
			htmlStr = htmlStr[:maxHTMLChars]
		}
		if len(strings.TrimSpace(htmlStr)) < 80 {
			return "", "", "", apperr.BadRequest("Isi halaman terlalu pendek untuk dianalisis.")
		}
		return htmlStr, current.String(), resp.Header.Get("Content-Type"), nil
	}
	return "", "", "", apperr.BadRequest("Terlalu banyak redirect.")
}

// ---------------------------------------------------------------------------
// HTML parsing helpers
// ---------------------------------------------------------------------------

var (
	reTitle     = regexp.MustCompile(`(?i)<title[^>]*>([\s\S]*?)</title>`)
	reScript    = regexp.MustCompile(`(?is)<script\b[^>]*>.*?</script>`)
	reStyle     = regexp.MustCompile(`(?is)<style\b[^>]*>.*?</style>`)
	reNoscript  = regexp.MustCompile(`(?is)<noscript\b[^>]*>.*?</noscript>`)
	reTag       = regexp.MustCompile(`<[^>]+>`)
	reSpace     = regexp.MustCompile(`\s+`)
)

func extractDocumentTitle(html string) *string {
	m := reTitle.FindStringSubmatch(html)
	if len(m) < 2 {
		return nil
	}
	t := strings.TrimSpace(reSpace.ReplaceAllString(decodeEntities(stripTags(m[1])), " "))
	if t == "" {
		return nil
	}
	return &t
}

func extractOGAndMeta(html string) (ogTitle, ogDesc, metaDesc *string) {
	ogTitle = pickMeta(html, "og:title")
	ogDesc = pickMeta(html, "og:description")
	metaDesc = pickMetaName(html, "description")
	return
}

func pickMeta(html, property string) *string {
	esc := regexp.QuoteMeta(property)
	for _, pat := range []string{
		`(?i)property=["']` + esc + `["'][^>]*content=["']([^"']+)["']`,
		`(?i)content=["']([^"']+)["'][^>]*property=["']` + esc + `["']`,
	} {
		if m := regexp.MustCompile(pat).FindStringSubmatch(html); len(m) > 1 {
			if v := cleanMeta(m[1]); v != "" {
				return &v
			}
		}
	}
	return nil
}

func pickMetaName(html, name string) *string {
	esc := regexp.QuoteMeta(name)
	for _, pat := range []string{
		`(?i)name=["']` + esc + `["'][^>]*content=["']([^"']+)["']`,
		`(?i)content=["']([^"']+)["'][^>]*name=["']` + esc + `["']`,
	} {
		if m := regexp.MustCompile(pat).FindStringSubmatch(html); len(m) > 1 {
			if v := cleanMeta(m[1]); v != "" {
				return &v
			}
		}
	}
	return nil
}

func cleanMeta(s string) string {
	return strings.TrimSpace(reSpace.ReplaceAllString(decodeEntities(s), " "))
}

func stripTags(s string) string  { return reTag.ReplaceAllString(s, " ") }

func decodeEntities(s string) string {
	r := strings.NewReplacer(
		"&nbsp;", " ", "&amp;", "&", "&lt;", "<", "&gt;", ">", "&quot;", `"`,
	)
	return r.Replace(s)
}

func htmlToVisibleText(html string) string {
	if len(html) > maxHTMLChars {
		html = html[:maxHTMLChars]
	}
	s := reScript.ReplaceAllString(html, " ")
	s = reStyle.ReplaceAllString(s, " ")
	s = reNoscript.ReplaceAllString(s, " ")
	return strings.TrimSpace(reSpace.ReplaceAllString(decodeEntities(stripTags(s)), " "))
}

// ---------------------------------------------------------------------------
// Anthropic extraction
// ---------------------------------------------------------------------------

func extractWithAnthropic(ctx context.Context, apiKey, sourceURL string, pageTitle *string, visibleText string) (ImportFieldSet, error) {
	client := anthropic.NewClient(option.WithAPIKey(apiKey))

	truncated := visibleText
	if len(truncated) > 12_000 {
		truncated = truncated[:12_000]
	}
	title := ""
	if pageTitle != nil {
		title = *pageTitle
	}

	prompt := fmt.Sprintf(`Kamu adalah asisten yang membantu mengekstrak informasi profil bisnis dari halaman website.

URL sumber: %s
Judul halaman: %s

Teks halaman (mungkin terpotong):
---
%s
---

Ekstrak informasi berikut dalam format JSON. Gunakan bahasa Indonesia jika konten berbahasa Indonesia. Jika informasi tidak ditemukan, isi null.

{
  "businessName": "nama bisnis/toko",
  "description": "deskripsi singkat bisnis (max 2000 karakter)",
  "address": "alamat lengkap jika ada",
  "openingHours": "jam operasional",
  "productsServices": "produk atau layanan utama (max 2000 karakter)",
  "basePricing": "kisaran harga dasar jika ada",
  "deliveryArea": "area pengiriman/jangkauan"
}

Jawab HANYA dengan JSON, tanpa penjelasan.`, sourceURL, title, truncated)

	msg, err := client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     anthropic.ModelClaudeSonnet4_20250514,
		MaxTokens: 1024,
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(prompt)),
		},
	})
	if err != nil {
		return ImportFieldSet{}, fmt.Errorf("anthropic API: %w", err)
	}

	var text string
	for _, block := range msg.Content {
		if block.Type == "text" {
			text = block.Text
			break
		}
	}
	if text == "" {
		return ImportFieldSet{}, fmt.Errorf("no text in anthropic response")
	}

	text = strings.TrimSpace(text)
	if idx := strings.Index(text, "```"); idx >= 0 {
		lines := strings.Split(text, "\n")
		var inner []string
		inside := false
		for _, l := range lines {
			if strings.HasPrefix(strings.TrimSpace(l), "```") {
				inside = !inside
				continue
			}
			if inside {
				inner = append(inner, l)
			}
		}
		if len(inner) > 0 {
			text = strings.Join(inner, "\n")
		}
	}

	var result ImportFieldSet
	if err := json.Unmarshal([]byte(text), &result); err != nil {
		return ImportFieldSet{}, fmt.Errorf("parse LLM JSON: %w", err)
	}
	return result, nil
}

// ---------------------------------------------------------------------------
// Field evaluation
// ---------------------------------------------------------------------------

func heuristicFields(ogTitle, ogDesc, metaDesc, docTitle *string) ImportFieldSet {
	return ImportFieldSet{
		BusinessName: strCoalesce(ogTitle, docTitle),
		Description:  strCoalesce(ogDesc, metaDesc),
	}
}

func mergePreferLLM(base, llm ImportFieldSet) ImportFieldSet {
	pick := func(a, b *string) *string {
		if a != nil && strings.TrimSpace(*a) != "" {
			return a
		}
		if b != nil && strings.TrimSpace(*b) != "" {
			return b
		}
		return nil
	}
	return ImportFieldSet{
		BusinessName:     pick(llm.BusinessName, base.BusinessName),
		Description:      pick(llm.Description, base.Description),
		Address:          pick(llm.Address, base.Address),
		OpeningHours:     pick(llm.OpeningHours, base.OpeningHours),
		ProductsServices: pick(llm.ProductsServices, base.ProductsServices),
		BasePricing:      pick(llm.BasePricing, base.BasePricing),
		DeliveryArea:     pick(llm.DeliveryArea, base.DeliveryArea),
	}
}

func clampFields(f ImportFieldSet) ImportFieldSet {
	clamp := func(v *string, max int) *string {
		if v == nil {
			return nil
		}
		if len(*v) > max {
			s := (*v)[:max]
			return &s
		}
		return v
	}
	return ImportFieldSet{
		BusinessName:     clamp(f.BusinessName, fieldLimits["businessName"]),
		Description:      clamp(f.Description, fieldLimits["description"]),
		Address:          clamp(f.Address, fieldLimits["address"]),
		OpeningHours:     clamp(f.OpeningHours, fieldLimits["openingHours"]),
		ProductsServices: clamp(f.ProductsServices, fieldLimits["productsServices"]),
		BasePricing:      clamp(f.BasePricing, fieldLimits["basePricing"]),
		DeliveryArea:     clamp(f.DeliveryArea, fieldLimits["deliveryArea"]),
	}
}

func evaluatePreview(fields ImportFieldSet, textLen int, usedLLM bool) (bool, *string, string) {
	nameLen := ptrLen(fields.BusinessName)
	descLen := ptrLen(fields.Description)
	prodLen := ptrLen(fields.ProductsServices)

	hasName := nameLen >= 2
	hasBlurb := descLen >= 28 || prodLen >= 28 || descLen+prodLen >= 36

	confidence := "low"
	switch {
	case usedLLM && hasName && hasBlurb:
		confidence = "high"
	case usedLLM && hasName, hasName && hasBlurb:
		confidence = "medium"
	}

	reason := func(s string) *string { return &s }

	if !hasName {
		return false, reason("Tidak menemukan nama bisnis yang cukup jelas dari halaman ini."), confidence
	}
	if !hasBlurb && textLen < 220 {
		return false, reason("Konten bisnis di halaman terlalu tipis (sedikit teks terlihat). Coba URL beranda atau halaman \"Tentang kami\"."), confidence
	}
	if !hasBlurb {
		return false, reason("Deskripsi atau produk/jasa tidak cukup untuk dipakai AI; lengkapi manual atau pilih halaman yang lebih informatif."), confidence
	}
	return true, nil, confidence
}

func marketplaceCrawlHints(hostname string) []string {
	h := strings.ToLower(hostname)
	for _, m := range []string{"shopee.", "tokopedia.", "toko.id", "lazada.", "bukalapak.", "blibli.", "tiktok.com"} {
		if strings.Contains(h, m) {
			return []string{
				"Domain marketplace: banyak konten produk dimuat lewat JavaScript, sehingga crawl server-side sering tidak mendapat daftar/harga lengkap. Untuk AI yang mengenal SKU detail, gunakan situs sendiri, FAQ, atau sinkron katalog terstruktur.",
			}
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Tiny string helpers
// ---------------------------------------------------------------------------

func strCoalesce(vals ...*string) *string {
	for _, v := range vals {
		if v != nil && strings.TrimSpace(*v) != "" {
			return v
		}
	}
	return nil
}

func ptrLen(s *string) int {
	if s == nil {
		return 0
	}
	return len(strings.TrimSpace(*s))
}
