package buyerflow

import "time"

// BusinessProfile — tenant business profile fields used for routing.
type BusinessProfile struct {
	BusinessName     string
	Description      *string
	Address          *string
	OpeningHours     *string
	ProductsServices *string
	BasePricing      *string
	DeliveryArea     *string
	GreetingTemplate *string
	Tone             *string
	AIEnabled        bool
	CatalogURL       *string
}

// Message — chat message (simulator uses Direction + Body only).
type Message struct {
	ID        string
	Direction string
	Author    string
	Type      string
	Body      string
	CreatedAt time.Time
}

// KBEntry — knowledge base row for FAQ / order templates.
type KBEntry struct {
	ID       string
	Question string
	Answer   string
	Category *string
	IsActive bool
}

// ClassifyResult — lightweight intent label from rules.
type ClassifyResult struct {
	Label      string
	Confidence float64
}

// CatalogStockLine — per-warehouse stock snapshot embedded on catalog items.
type CatalogStockLine struct {
	WarehouseID   string
	WarehouseName string
	CustomerLabel string
	IsDefault     bool
	DisplayOrder  int
	Available     float64
}

// CatalogItem — product row from business catalog (+ optional stock enrichment).
type CatalogItem struct {
	ID           string
	ExternalCode string
	Name         string
	SellPrice    float64
	SellUnit     string
	StockTracked     bool
	StockAvailable   float64
	StockByWarehouse []CatalogStockLine
}

// OrderLineState — one line in a multi-item order (Redis JSON).
type OrderLineState struct {
	CatalogItemID string  `json:"catalogItemId,omitempty"`
	ExternalCode  string  `json:"externalCode,omitempty"`
	ProductName   string  `json:"productName,omitempty"`
	Size          string  `json:"size,omitempty"`
	Color         string  `json:"color,omitempty"`
	Qty           int     `json:"qty,omitempty"`
	UnitPrice     float64 `json:"unitPrice,omitempty"`
	SellUnit      string  `json:"sellUnit,omitempty"`
	WarehouseID   string  `json:"warehouseId,omitempty"`
}

// OrderState — checkout FSM state (in-memory / Redis JSON).
type OrderState struct {
	Step string `json:"step"`

	Product       string  `json:"product,omitempty"`
	CatalogItemID string  `json:"catalogItemId,omitempty"`
	ExternalCode  string  `json:"externalCode,omitempty"`
	ProductName   string  `json:"productName,omitempty"`
	Size          string  `json:"size,omitempty"`
	Color         string  `json:"color,omitempty"`
	Variant       string  `json:"variant,omitempty"`
	Qty           int     `json:"qty,omitempty"`
	UnitPrice     float64 `json:"unitPrice,omitempty"`
	SellUnit      string  `json:"sellUnit,omitempty"`

	RecipientName  string `json:"recipientName,omitempty"`
	RecipientPhone string `json:"recipientPhone,omitempty"`
	WarehouseID    string `json:"warehouseId,omitempty"`
	Street         string `json:"street,omitempty"`
	RT             string `json:"rt,omitempty"`
	RW             string `json:"rw,omitempty"`
	Kelurahan      string `json:"kelurahan,omitempty"`
	Kecamatan      string `json:"kecamatan,omitempty"`
	City           string `json:"city,omitempty"`
	Province       string `json:"province,omitempty"`
	PostalCode     string `json:"postalCode,omitempty"`
	Country        string `json:"country,omitempty"`

	Items []OrderLineState `json:"items,omitempty"`

	// PersistedOrderID — draft row di DB yang terikat ke keranjang checkout ini.
	PersistedOrderID string `json:"persistedOrderId,omitempty"`
}
