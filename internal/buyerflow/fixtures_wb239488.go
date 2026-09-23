package buyerflow

// WB239488Catalog is the Omah apparel catalog slice for draft WB-239488D0 tests.
func WB239488Catalog() []CatalogItem {
	return append([]CatalogItem{
		{ID: "maggi-berempah", ExternalCode: "MAGGI_BAG_AYAM_BEREMPAH", Name: "Maggi Bumbu Ayam Goreng - Ayam Berempah", SellPrice: 70000, SellUnit: "pcs"},
		{ID: "maggi-percik", ExternalCode: "MAGGI_BAG_AYAM_PERCIK", Name: "Maggi Bumbu Ayam Goreng - Ayam Percik", SellPrice: 70000, SellUnit: "pcs"},
		{ID: "maggi-pepper", ExternalCode: "MAGGI_BAG_BLACK_PEPPER", Name: "Maggi Bumbu Ayam Goreng - Black Pepper", SellPrice: 70000, SellUnit: "pcs"},
		{ID: "maggi-tandoori", ExternalCode: "MAGGI_BAG_TANDOORI", Name: "Maggi Bumbu Ayam Goreng - Tandoori", SellPrice: 70000, SellUnit: "pcs"},
		{ID: "nutella", ExternalCode: "NUTELLA_BISKUIT_193G", Name: "Nutella Biskuit (193g)", SellPrice: 155000, SellUnit: "pcs"},
		{ID: "abon-125", Name: "Abon Sapi 125 Gram", SellPrice: 12500, SellUnit: "pcs"},
		{ID: "abon-250", Name: "Abon Sapi 250 Gram", SellPrice: 20000, SellUnit: "pcs"},
		{ID: "abon-500", Name: "Abon Sapi 500 Gram", SellPrice: 25000, SellUnit: "pcs"},
		{ID: "cad-bar", Name: "Cadbury biscoff bar 130 gram", SellPrice: 105000, SellUnit: "pcs"},
		{ID: "cad-mini", Name: "Cadbury biscoff mini bars", SellPrice: 110000, SellUnit: "pcs"},
		{ID: "oat-white", Name: "Oatlife White Coffee", SellPrice: 200000, SellUnit: "pcs"},
	}, CatalogItem{
		ID: "durian-biscuit", ExternalCode: "Musang-king-durian-biscuit-240GRAM",
		Name: "Musang king durian biscuit 240G", SellPrice: 155000, SellUnit: "pcs",
	})
}

// WB239488DraftState is the persisted draft before a bad amend/cancel turn.
func WB239488DraftState() *OrderState {
	st := &OrderState{
		Step:             "ask_recipient",
		PersistedOrderID: "239488d0-f98e-4ac7-b4b8-8f18f2ad42f6",
		Items: []OrderLineState{
			{CatalogItemID: "cad-bar", ProductName: "Cadbury biscoff bar 130 gram", Qty: 1, UnitPrice: 105000, SellUnit: "pcs"},
			{CatalogItemID: "durian-biscuit", ProductName: "Musang king durian biscuit 240G", Qty: 1, UnitPrice: 155000, SellUnit: "pcs"},
			{CatalogItemID: "maggi-berempah", ProductName: "Maggi Bumbu Ayam Goreng - Ayam Berempah", Qty: 1, UnitPrice: 70000, SellUnit: "pcs"},
		},
	}
	syncOrderStateFromItems(st)
	return st
}

// NewWB239488Simulator hydrates draft WB-239488D0 for behavior regression tests.
func NewWB239488Simulator() *Simulator {
	p := foodProfile()
	return &Simulator{
		Profile: p,
		Catalog: WB239488Catalog(),
		ScopeKW: businessScopeKeywords(p),
		Order:   WB239488DraftState(),
	}
}
