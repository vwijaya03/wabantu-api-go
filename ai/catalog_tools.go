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

// catalogToolItem — hasil tool untuk LLM (tanpa ID internal sensitif).
type catalogToolItem struct {
	Name        string  `json:"name"`
	Price       string  `json:"price"`
	PackCount   int     `json:"pack_count,omitempty"`
	PerPiece    string  `json:"per_piece,omitempty"`
	SellUnit    string  `json:"sell_unit,omitempty"`
	ExternalRef string  `json:"external_ref,omitempty"`
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
			return `{"found":false,"items":[],"message":"Saya belum menemukan data tersebut di katalog saat ini."}`, nil
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
			return `{"found":false,"message":"Saya belum menemukan data tersebut di katalog saat ini."}`, nil
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
		SellUnit:    info.unitLabel,
		ExternalRef: it.ExternalCode,
	}
	if info.isPackListing {
		item.PackCount = info.packCount
		item.PerPiece = formatMoney(info.perPiecePrice)
	}
	return item
}
