package ai

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

const (
	catalogToolSearch    = "search_catalog"
	catalogToolGetProduct = "get_product"
)

// catalogToolDescriptionMaxRunes bounds the description handed to the model per tool result.
const catalogToolDescriptionMaxRunes = 300

// catalogToolItem — hasil tool untuk LLM (tanpa ID internal sensitif).
type catalogToolItem struct {
	Name        string  `json:"name"`
	Price       string  `json:"price"`
	PackCount   int     `json:"pack_count,omitempty"`
	PerPiece    string  `json:"per_piece,omitempty"`
	SellUnit    string  `json:"sell_unit,omitempty"`
	ExternalRef string  `json:"external_ref,omitempty"`
	Description string  `json:"description,omitempty"`
	Score       float64 `json:"match_score,omitempty"`
}

// CatalogToolExecutor menjalankan tool katalog terhadap snapshot DB.
type CatalogToolExecutor struct {
	catalog []dbCatalogItem
}

func NewCatalogToolExecutor(catalog []dbCatalogItem) *CatalogToolExecutor {
	return &CatalogToolExecutor{catalog: catalog}
}

func (e *CatalogToolExecutor) Run(toolName string, input json.RawMessage) (string, error) {
	if e == nil {
		return "", fmt.Errorf("catalog executor nil")
	}
	switch toolName {
	case catalogToolSearch:
		var args struct {
			Query string `json:"query"`
			Limit int    `json:"limit"`
		}
		if err := json.Unmarshal(input, &args); err != nil {
			return "", fmt.Errorf("invalid search_catalog input: %w", err)
		}
		items := searchCatalogItems(args.Query, e.catalog, args.Limit)
		if len(items) == 0 {
			// Neutral JSON on purpose: a prose message here (it used to be Indonesian) gets echoed
			// to the buyer in the wrong language; the model words the miss in the buyer's language.
			return `{"found":false,"items":[]}`, nil
		}
		out, err := json.Marshal(map[string]any{"found": true, "items": items})
		return string(out), err
	case catalogToolGetProduct:
		var args struct {
			Ref string `json:"ref"`
		}
		if err := json.Unmarshal(input, &args); err != nil {
			return "", fmt.Errorf("invalid get_product input: %w", err)
		}
		it := getCatalogProductByRef(args.Ref, e.catalog)
		if it == nil {
			return `{"found":false}`, nil
		}
		out, err := json.Marshal(map[string]any{"found": true, "product": it})
		return string(out), err
	default:
		return "", fmt.Errorf("unknown catalog tool: %s", toolName)
	}
}

func searchCatalogItems(query string, catalog []dbCatalogItem, limit int) []catalogToolItem {
	if limit < 1 || limit > 10 {
		limit = 5
	}
	query = strings.TrimSpace(query)
	if query == "" || len(catalog) == 0 {
		return nil
	}
	type scored struct {
		item  catalogToolItem
		score float64
	}
	var hits []scored
	tokens := tokenize(query)
	for i := range catalog {
		it := &catalog[i]
		nameLower := strings.ToLower(it.Name)
		score := overlapScore(tokens, tokenize(nameLower))
		// The buyer's words ("tuna steak") often appear only in the description (the product is named
		// "Tuna Portion"). Score the share of query words the description covers rather than Dice
		// overlap, which a long description would dilute to nothing; a name hit still outranks it
		// because the name bonuses below are added on top.
		if descScore := queryCoverage(tokens, tokenize(strings.ToLower(it.Description))); descScore > 0 {
			score = max(score, 0.6*descScore)
		}
		if strings.Contains(nameLower, strings.ToLower(query)) {
			score += 0.4
		}
		codeLower := strings.ToLower(it.ExternalCode)
		if codeLower != "" && (codeLower == strings.ToLower(query) || strings.Contains(codeLower, strings.ToLower(query))) {
			score += 0.5
		}
		if score < 0.1 {
			continue
		}
		hits = append(hits, scored{item: formatCatalogToolItem(it), score: score})
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].score > hits[j].score })
	if len(hits) > limit {
		hits = hits[:limit]
	}
	out := make([]catalogToolItem, len(hits))
	for i, h := range hits {
		h.item.Score = h.score
		out[i] = h.item
	}
	return out
}

func getCatalogProductByRef(ref string, catalog []dbCatalogItem) *catalogToolItem {
	ref = strings.TrimSpace(ref)
	if ref == "" || len(catalog) == 0 {
		return nil
	}
	refLower := strings.ToLower(ref)
	for i := range catalog {
		it := &catalog[i]
		if strings.EqualFold(it.ExternalCode, ref) || strings.EqualFold(it.Name, ref) {
			item := formatCatalogToolItem(it)
			return &item
		}
	}
	if match := matchCatalogItem(ref, catalog); match != nil {
		item := formatCatalogToolItem(match)
		return &item
	}
	for i := range catalog {
		it := &catalog[i]
		if strings.Contains(strings.ToLower(it.Name), refLower) {
			item := formatCatalogToolItem(it)
			return &item
		}
	}
	return nil
}

func formatCatalogToolItem(it *dbCatalogItem) catalogToolItem {
	if it == nil {
		return catalogToolItem{}
	}
	info := parseCatalogPriceInfo(it)
	item := catalogToolItem{
		Name:        it.Name,
		Price:       formatCatalogPrice(it),
		SellUnit:    info.UnitLabel,
		ExternalRef: it.ExternalCode,
		Description: clipRunes(strings.TrimSpace(it.Description), catalogToolDescriptionMaxRunes),
	}
	if info.IsPackListing {
		item.PackCount = info.PackCount
		item.PerPiece = formatMoney(info.PerPiecePrice)
	}
	return item
}

// clipRunes shortens s to at most limit runes (never splitting a multi-byte character) and
// marks the cut with an ellipsis.
func clipRunes(s string, limit int) string {
	if limit <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= limit {
		return s
	}
	return strings.TrimSpace(string(r[:limit])) + "…"
}

// queryCoverage is the share of distinct query tokens that also occur in doc (0..1).
func queryCoverage(query, doc []string) float64 {
	if len(query) == 0 || len(doc) == 0 {
		return 0
	}
	inDoc := make(map[string]struct{}, len(doc))
	for _, t := range doc {
		inDoc[t] = struct{}{}
	}
	seen := make(map[string]struct{}, len(query))
	hit := 0
	for _, t := range query {
		if _, dup := seen[t]; dup {
			continue
		}
		seen[t] = struct{}{}
		if _, ok := inDoc[t]; ok {
			hit++
		}
	}
	return float64(hit) / float64(len(seen))
}
