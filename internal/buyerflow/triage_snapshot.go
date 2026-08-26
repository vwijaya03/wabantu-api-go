package buyerflow

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// TriageSimulatorSnapshot freezes tenant profile/catalog/KB for auto-gen regression tests.
type TriageSimulatorSnapshot struct {
	TenantSchema string                  `json:"tenantSchema,omitempty"`
	Profile      triageProfileSnapshot   `json:"profile"`
	Catalog      []triageCatalogSnapshot `json:"catalog"`
	KB           []triageKBSnapshot      `json:"kb,omitempty"`
}

type triageProfileSnapshot struct {
	BusinessName     string  `json:"businessName"`
	Description      *string `json:"description,omitempty"`
	Address          *string `json:"address,omitempty"`
	OpeningHours     *string `json:"openingHours,omitempty"`
	ProductsServices *string `json:"productsServices,omitempty"`
	BasePricing      *string `json:"basePricing,omitempty"`
	DeliveryArea     *string `json:"deliveryArea,omitempty"`
	GreetingTemplate *string `json:"greetingTemplate,omitempty"`
	Tone             *string `json:"tone,omitempty"`
	AIEnabled        bool    `json:"aiEnabled,omitempty"`
	CatalogURL       *string `json:"catalogUrl,omitempty"`
}

type triageCatalogSnapshot struct {
	ID               string                    `json:"id"`
	ExternalCode     string                    `json:"externalCode,omitempty"`
	Name             string                    `json:"name"`
	SellPrice        float64                   `json:"sellPrice,omitempty"`
	SellUnit         string                    `json:"sellUnit,omitempty"`
	StockTracked     bool                      `json:"stockTracked,omitempty"`
	StockAvailable   float64                   `json:"stockAvailable,omitempty"`
	StockByWarehouse []triageStockLineSnapshot `json:"stockByWarehouse,omitempty"`
}

type triageStockLineSnapshot struct {
	WarehouseID   string  `json:"warehouseId"`
	WarehouseName string  `json:"warehouseName,omitempty"`
	CustomerLabel string  `json:"customerLabel,omitempty"`
	IsDefault     bool    `json:"isDefault,omitempty"`
	DisplayOrder  int     `json:"displayOrder,omitempty"`
	Available     float64 `json:"available,omitempty"`
}

type triageKBSnapshot struct {
	ID       string  `json:"id,omitempty"`
	Question string  `json:"question"`
	Answer   string  `json:"answer"`
	Category *string `json:"category,omitempty"`
	IsActive bool    `json:"isActive,omitempty"`
}

// SimulatorToSnapshot copies simulator state used during triage analyze.
func SimulatorToSnapshot(sim *Simulator, tenantSchema string) *TriageSimulatorSnapshot {
	if sim == nil || sim.Profile == nil {
		return nil
	}
	snap := &TriageSimulatorSnapshot{
		TenantSchema: strings.TrimSpace(tenantSchema),
		Profile: triageProfileSnapshot{
			BusinessName:     sim.Profile.BusinessName,
			Description:      sim.Profile.Description,
			Address:          sim.Profile.Address,
			OpeningHours:     sim.Profile.OpeningHours,
			ProductsServices: sim.Profile.ProductsServices,
			BasePricing:      sim.Profile.BasePricing,
			DeliveryArea:     sim.Profile.DeliveryArea,
			GreetingTemplate: sim.Profile.GreetingTemplate,
			Tone:             sim.Profile.Tone,
			AIEnabled:        sim.Profile.AIEnabled,
			CatalogURL:       sim.Profile.CatalogURL,
		},
		Catalog: make([]triageCatalogSnapshot, 0, len(sim.Catalog)),
		KB:      make([]triageKBSnapshot, 0, len(sim.KB)),
	}
	for _, it := range sim.Catalog {
		item := triageCatalogSnapshot{
			ID:             it.ID,
			ExternalCode:   it.ExternalCode,
			Name:           it.Name,
			SellPrice:      it.SellPrice,
			SellUnit:       it.SellUnit,
			StockTracked:   it.StockTracked,
			StockAvailable: it.StockAvailable,
		}
		for _, wh := range it.StockByWarehouse {
			item.StockByWarehouse = append(item.StockByWarehouse, triageStockLineSnapshot{
				WarehouseID:   wh.WarehouseID,
				WarehouseName: wh.WarehouseName,
				CustomerLabel: wh.CustomerLabel,
				IsDefault:     wh.IsDefault,
				DisplayOrder:  wh.DisplayOrder,
				Available:     wh.Available,
			})
		}
		snap.Catalog = append(snap.Catalog, item)
	}
	for _, kb := range sim.KB {
		snap.KB = append(snap.KB, triageKBSnapshot{
			ID:       kb.ID,
			Question: kb.Question,
			Answer:   kb.Answer,
			Category: kb.Category,
			IsActive: kb.IsActive,
		})
	}
	return snap
}

// SimulatorFromSnapshot rebuilds a Simulator from a frozen snapshot.
func SimulatorFromSnapshot(snap *TriageSimulatorSnapshot) (*Simulator, error) {
	if snap == nil {
		return nil, fmt.Errorf("snapshot is nil")
	}
	if strings.TrimSpace(snap.Profile.BusinessName) == "" {
		return nil, fmt.Errorf("snapshot profile missing businessName")
	}
	profile := &BusinessProfile{
		BusinessName:     snap.Profile.BusinessName,
		Description:      snap.Profile.Description,
		Address:          snap.Profile.Address,
		OpeningHours:     snap.Profile.OpeningHours,
		ProductsServices: snap.Profile.ProductsServices,
		BasePricing:      snap.Profile.BasePricing,
		DeliveryArea:     snap.Profile.DeliveryArea,
		GreetingTemplate: snap.Profile.GreetingTemplate,
		Tone:             snap.Profile.Tone,
		AIEnabled:        snap.Profile.AIEnabled,
		CatalogURL:       snap.Profile.CatalogURL,
	}
	catalog := make([]CatalogItem, 0, len(snap.Catalog))
	for _, it := range snap.Catalog {
		item := CatalogItem{
			ID:             it.ID,
			ExternalCode:   it.ExternalCode,
			Name:           it.Name,
			SellPrice:      it.SellPrice,
			SellUnit:       it.SellUnit,
			StockTracked:   it.StockTracked,
			StockAvailable: it.StockAvailable,
		}
		for _, wh := range it.StockByWarehouse {
			item.StockByWarehouse = append(item.StockByWarehouse, CatalogStockLine{
				WarehouseID:   wh.WarehouseID,
				WarehouseName: wh.WarehouseName,
				CustomerLabel: wh.CustomerLabel,
				IsDefault:     wh.IsDefault,
				DisplayOrder:  wh.DisplayOrder,
				Available:     wh.Available,
			})
		}
		catalog = append(catalog, item)
	}
	kb := make([]KBEntry, 0, len(snap.KB))
	for _, entry := range snap.KB {
		kb = append(kb, KBEntry{
			ID:       entry.ID,
			Question: entry.Question,
			Answer:   entry.Answer,
			Category: entry.Category,
			IsActive: entry.IsActive,
		})
	}
	return &Simulator{
		Profile: profile,
		Catalog: catalog,
		KB:      kb,
		ScopeKW: businessScopeKeywords(profile),
	}, nil
}

// SimulatorFromSnapshotJSON parses embedded JSON from auto-gen regression file.
func SimulatorFromSnapshotJSON(jsonLiteral string) (*Simulator, error) {
	jsonLiteral = strings.TrimSpace(jsonLiteral)
	if jsonLiteral == "" {
		return nil, fmt.Errorf("empty snapshot json")
	}
	var snap TriageSimulatorSnapshot
	if err := json.Unmarshal([]byte(jsonLiteral), &snap); err != nil {
		return nil, err
	}
	return SimulatorFromSnapshot(&snap)
}

// FormatSnapshotGoConst returns a Go string literal for embedding in generated tests.
func FormatSnapshotGoConst(snap *TriageSimulatorSnapshot) (string, error) {
	if snap == nil {
		return `""`, nil
	}
	b, err := json.Marshal(snap)
	if err != nil {
		return "", err
	}
	return strconv.Quote(string(b)), nil
}
