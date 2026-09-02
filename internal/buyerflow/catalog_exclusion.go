package buyerflow

import "strings"

// IsCatalogExclusionQuestion — browse katalog dengan mengecualikan produk tertentu.
func IsCatalogExclusionQuestion(userText string) bool {
	text := strings.ToLower(strings.TrimSpace(userText))
	if text == "" {
		return false
	}
	signals := []string{
		"selain ", "kecuali ", "list lain", "daftar lain",
		"produk lain", "barang lain", "yang lain", "ada yang lain",
		"selain itu", "selain ini", "other than", "besides ",
	}
	for _, s := range signals {
		if strings.Contains(text, s) {
			return true
		}
	}
	if strings.Contains(text, "ada list") || strings.Contains(text, "minta list lain") {
		return true
	}
	return false
}

func buildCatalogListReplyFiltered(formal bool, bizName string, catalog []CatalogItem, profile *BusinessProfile, userText string) string {
	filtered := filterCatalogByExcludeHints(catalog, catalogExcludeHints(userText))
	return buildCatalogListReply(formal, bizName, filtered, profile)
}

func filterCatalogByExcludeHints(catalog []CatalogItem, exclude []string) []CatalogItem {
	if len(exclude) == 0 {
		return catalog
	}
	var out []CatalogItem
	for _, it := range catalog {
		nameLower := strings.ToLower(it.Name)
		if catalogItemExcluded(nameLower, exclude) {
			continue
		}
		out = append(out, it)
	}
	return out
}
