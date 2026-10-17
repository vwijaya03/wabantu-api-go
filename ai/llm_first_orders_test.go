package ai

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"encore.app/wabantu/internal/salesbrain"
)

// lfOrdersCatalog is the Omah steak catalog as the legacy loaders return it.
func lfOrdersCatalog() []dbCatalogItem {
	return []dbCatalogItem{
		{ID: "beef-500", ExternalCode: "AUSTRALIAN_BEEF_500_GRAM", Name: "Australian Beef 500 Gram", SellPrice: 450000, SellUnit: "pcs"},
		{ID: "tbone-1500", ExternalCode: "AUSTRALIAN_BEEF_T_BONE_1_5_KG", Name: "Australian Beef T-Bone 1.5 Kg", SellPrice: 1750000, SellUnit: "pcs"},
		{ID: "salmon-100", ExternalCode: "SALMON_STEAK_100_GRAM", Name: "Salmon Steak 100 Gram", SellPrice: 75000, SellUnit: "pcs"},
		{ID: "salmon-250", ExternalCode: "SALMON_STEAK_250_GRAM_SALINAN_CZED", Name: "Salmon Steak 250 Gram", SellPrice: 105000, SellUnit: "pcs"},
		{ID: "tuna-100", ExternalCode: "YELLOW_TAIL_TUNA_PORTION_100_GRAM", Name: "Yellow Tail Tuna Portion 100 Gram", SellPrice: 350000, SellUnit: "pcs"},
	}
}

func lfSalmonTunaCart() []salesbrain.OrderLine {
	return []salesbrain.OrderLine{
		{ID: "salmon-100", Name: "Salmon Steak 100 Gram", Qty: 1, UnitPrice: 75000},
		{ID: "tuna-100", Name: "Yellow Tail Tuna Portion 100 Gram", Qty: 2, UnitPrice: 350000},
	}
}

// lfTunaState is a checkout state for one tuna steak, waiting for the recipient.
func lfTunaState() *orderState {
	return stateWithCart(nil, []salesbrain.OrderLine{{ID: "tuna-100", Name: "Yellow Tail Tuna Portion 100 Gram", Qty: 1, UnitPrice: 350000}}, lfOrdersCatalog())
}

const lfFullAddressText = "Name: Ana\nPhone: 0872839472828\nAddress: Villa Kalijudan Indah A9, Surabaya, Jawa Timur 60177"

func lfFullAddressFields() *salesbrain.FieldData {
	return &salesbrain.FieldData{
		Name: "Ana", Phone: "0872839472828", Street: "Villa Kalijudan Indah A9",
		City: "Surabaya", Province: "Jawa Timur", PostalCode: "60177",
	}
}

func TestCartFromStateRoundTrip(t *testing.T) {
	cart := lfSalmonTunaCart()
	st := stateWithCart(nil, cart, lfOrdersCatalog())
	if st == nil {
		t.Fatal("a non-empty cart must produce a state")
	}
	if got := cartFromState(st); !reflect.DeepEqual(got, cart) {
		t.Fatalf("round trip changed the cart:\n got %+v\nwant %+v", got, cart)
	}
	if cartFromState(nil) != nil || cartFromState(&orderState{}) != nil {
		t.Fatal("no state or an empty state is an empty cart")
	}
}

func TestCartFromStateLegacySingleItemState(t *testing.T) {
	legacy := &orderState{Step: "ask_recipient", CatalogItemID: "tuna-100", ProductName: "Yellow Tail Tuna Portion 100 Gram", Qty: 2, UnitPrice: 350000}
	want := []salesbrain.OrderLine{{ID: "tuna-100", Name: "Yellow Tail Tuna Portion 100 Gram", Qty: 2, UnitPrice: 350000}}
	if got := cartFromState(legacy); !reflect.DeepEqual(got, want) {
		t.Fatalf("legacy single-item state must read as a one-line cart:\n got %+v\nwant %+v", got, want)
	}

	// A product picked but not quantified yet is not a cart line: quantities are never invented.
	pending := &orderState{Step: "ask_qty", CatalogItemID: "tuna-100", ProductName: "Yellow Tail Tuna Portion 100 Gram"}
	if got := cartFromState(pending); got != nil {
		t.Fatalf("a product without quantity must not become a line, got %+v", got)
	}

	// Legacy rows keep the product name when it only lives in the old Product field.
	oldName := &orderState{Step: "ask_recipient", CatalogItemID: "salmon-100", Product: "Salmon Steak 100 Gram", Qty: 1, UnitPrice: 75000}
	if got := cartFromState(oldName); len(got) != 1 || got[0].Name != "Salmon Steak 100 Gram" {
		t.Fatalf("Product must be used when ProductName is empty, got %+v", got)
	}

	// Prices are stored as float64 in the legacy state: round, never truncate.
	frac := &orderState{Items: []orderLineState{{CatalogItemID: "x", ProductName: "X", Qty: 1, UnitPrice: 74999.6}}}
	if got := cartFromState(frac); len(got) != 1 || got[0].UnitPrice != 75000 {
		t.Fatalf("unit price must be rounded, got %+v", got)
	}
}

func TestLLMFirstOrdersSingleLineMirrorsLegacyFields(t *testing.T) {
	st := lfTunaState()
	if st == nil {
		t.Fatal("expected a state")
	}
	wantLine := orderLineState{
		CatalogItemID: "tuna-100", ExternalCode: "YELLOW_TAIL_TUNA_PORTION_100_GRAM",
		ProductName: "Yellow Tail Tuna Portion 100 Gram", Qty: 1, UnitPrice: 350000, SellUnit: "pcs",
	}
	if len(st.Items) != 1 || st.Items[0] != wantLine {
		t.Fatalf("items = %+v, want one line %+v", st.Items, wantLine)
	}
	if st.CatalogItemID != "tuna-100" || st.ExternalCode != "YELLOW_TAIL_TUNA_PORTION_100_GRAM" ||
		st.ProductName != "Yellow Tail Tuna Portion 100 Gram" || st.Qty != 1 || st.UnitPrice != 350000 || st.SellUnit != "pcs" {
		t.Fatalf("a single line must mirror the legacy single-item fields, got %+v", *st)
	}
	if st.Step != "ask_recipient" {
		t.Fatalf("a new cart waits for the recipient, got step %q", st.Step)
	}
	if !st.HasMultiItems() || !st.CartReadyForDraft() {
		t.Fatalf("legacy helpers must accept the state: multi=%v ready=%v", st.HasMultiItems(), st.CartReadyForDraft())
	}
}

func TestLLMFirstOrdersStateWithCartMovesStepAndKeepsFields(t *testing.T) {
	cart := lfSalmonTunaCart()
	for from, want := range map[string]string{
		"":                 "ask_recipient",
		"ask_product":      "ask_recipient",
		"ask_variant":      "ask_recipient",
		"ask_qty":          "ask_recipient",
		"ask_recipient":    "ask_recipient",
		"ask_address_full": "ask_address_full",
		"ask_address":      "ask_address",
		"confirm":          "confirm",
	} {
		prev := &orderState{
			Step: from, RecipientName: "Ana", RecipientPhone: "+62872839472828",
			Street: "Villa Kalijudan Indah A9", Kecamatan: "Kenjeran", City: "Surabaya", Province: "Jawa Timur", PostalCode: "60177",
			PersistedOrderID: "order-1", UpsellShown: true,
		}
		before := *prev
		got := stateWithCart(prev, cart, lfOrdersCatalog())
		if got == nil {
			t.Fatalf("step %q: expected a state", from)
		}
		if got.Step != want {
			t.Errorf("step %q → %q, want %q", from, got.Step, want)
		}
		if got.RecipientName != "Ana" || got.RecipientPhone != "+62872839472828" || got.Street != "Villa Kalijudan Indah A9" ||
			got.Kecamatan != "Kenjeran" || got.City != "Surabaya" || got.Province != "Jawa Timur" || got.PostalCode != "60177" {
			t.Errorf("step %q: recipient and address must be kept, got %+v", from, *got)
		}
		if got.PersistedOrderID != "order-1" || !got.UpsellShown {
			t.Errorf("step %q: draft binding and upsell flag must be kept, got %+v", from, *got)
		}
		if !reflect.DeepEqual(*prev, before) {
			t.Errorf("step %q: the input state must not be mutated", from)
		}
	}
}

func TestLLMFirstOrdersStateWithCartEmptyIsNil(t *testing.T) {
	if got := stateWithCart(&orderState{Step: "ask_recipient"}, nil, lfOrdersCatalog()); got != nil {
		t.Fatalf("an empty cart has no state, got %+v", got)
	}
	if got := stateWithCart(nil, []salesbrain.OrderLine{}, lfOrdersCatalog()); got != nil {
		t.Fatalf("an empty cart has no state, got %+v", got)
	}
}

func TestLLMFirstOrdersStateWithCartMultiLine(t *testing.T) {
	prev := &orderState{
		Step: "ask_recipient",
		Items: []orderLineState{
			{CatalogItemID: "salmon-100", ProductName: "Salmon Steak 100 Gram", Qty: 1, UnitPrice: 75000, Size: "M", Color: "Pink", WarehouseID: "wh-1"},
			{CatalogItemID: "retired", ExternalCode: "RETIRED_CODE", ProductName: "Retired Item", Qty: 3, UnitPrice: 1000, SellUnit: "box"},
		},
	}
	cart := []salesbrain.OrderLine{
		{ID: "salmon-100", Name: "Salmon Steak 100 Gram", Qty: 4, UnitPrice: 75000},
		{ID: "retired", Name: "Retired Item", Qty: 3, UnitPrice: 1000},
		{ID: "tuna-100", Name: "Yellow Tail Tuna Portion 100 Gram", Qty: 1, UnitPrice: 350000},
	}
	got := stateWithCart(prev, cart, lfOrdersCatalog())
	if got == nil || len(got.Items) != 3 {
		t.Fatalf("want 3 lines, got %+v", got)
	}
	salmon := got.Items[0]
	if salmon.Qty != 4 || salmon.ExternalCode != "SALMON_STEAK_100_GRAM" || salmon.SellUnit != "pcs" {
		t.Errorf("catalog row fills code and unit, qty comes from the cart: %+v", salmon)
	}
	if salmon.Size != "M" || salmon.Color != "Pink" || salmon.WarehouseID != "wh-1" {
		t.Errorf("variant and warehouse of an existing line must survive a qty change: %+v", salmon)
	}
	if retired := got.Items[1]; retired.ExternalCode != "RETIRED_CODE" || retired.SellUnit != "box" {
		t.Errorf("a line missing from the catalog keeps its earlier code and unit: %+v", retired)
	}
	if tuna := got.Items[2]; tuna.ExternalCode != "YELLOW_TAIL_TUNA_PORTION_100_GRAM" || tuna.Size != "" {
		t.Errorf("a new line starts clean: %+v", tuna)
	}
	// Legacy helpers read the first line when there are several (same as ApplyLineToOrderState).
	if got.CatalogItemID != "salmon-100" || got.Qty != 4 || got.UnitPrice != 75000 {
		t.Errorf("the single-item fields must mirror the first line, got %+v", *got)
	}
	if back := cartFromState(got); !reflect.DeepEqual(back, cart) {
		t.Errorf("round trip changed the cart:\n got %+v\nwant %+v", back, cart)
	}
}

func TestLLMFirstOrdersPendingFieldFromState(t *testing.T) {
	for step, want := range map[string]string{
		"":                 "",
		"ask_product":      "",
		"ask_variant":      "",
		"ask_qty":          "",
		"ask_recipient":    "recipient",
		"ask_address":      "address",
		"ask_address_full": "address",
		"confirm":          "confirm",
		"something_else":   "",
	} {
		if got := pendingFieldFromState(&orderState{Step: step}); got != want {
			t.Errorf("step %q → %q, want %q", step, got, want)
		}
	}
	if pendingFieldFromState(nil) != "" {
		t.Error("no state, no pending field")
	}
}

func TestFieldAnswerCompleteAnswerReachesConfirm(t *testing.T) {
	st := lfTunaState()
	problems := applyFieldAnswer(st, lfFullAddressFields(), lfFullAddressText, salesbrain.LangEN)

	if len(problems) != 0 {
		t.Fatalf("unexpected problems: %v", problems)
	}
	if st.RecipientName != "Ana" || st.RecipientPhone != "+62872839472828" || st.Street != "Villa Kalijudan Indah A9" ||
		st.City != "Surabaya" || st.Province != "Jawa Timur" || st.PostalCode != "60177" {
		t.Fatalf("fields not written: %+v", *st)
	}
	if !st.ShippingComplete() {
		t.Fatalf("shipping must be complete: %+v", *st)
	}
	if st.Step != "confirm" {
		t.Fatalf("a complete answer ends in step confirm, got %q", st.Step)
	}
	if pendingFieldFromState(st) != "confirm" {
		t.Fatal("the pending field after a complete answer is confirm")
	}
}

func TestFieldAnswerStagedAnswersWalkTheSteps(t *testing.T) {
	st := lfTunaState()
	if problems := applyFieldAnswer(st, &salesbrain.FieldData{Name: "Ana", Phone: "0872839472828"}, "Ana, 0872839472828", salesbrain.LangEN); len(problems) != 0 {
		t.Fatalf("unexpected problems: %v", problems)
	}
	if st.Step != "ask_address_full" || st.RecipientName != "Ana" || st.RecipientPhone != "+62872839472828" {
		t.Fatalf("name and phone move on to the address, got step %q state %+v", st.Step, *st)
	}

	// Name alone is not enough to leave the recipient step.
	only := lfTunaState()
	applyFieldAnswer(only, &salesbrain.FieldData{Name: "Ana"}, "Ana", salesbrain.LangEN)
	if only.Step != "ask_recipient" || only.RecipientName != "Ana" {
		t.Fatalf("a name without a phone stays on the recipient step, got step %q", only.Step)
	}

	addr := "Villa Kalijudan Indah A9, Surabaya, Jawa Timur 60177"
	if problems := applyFieldAnswer(st, &salesbrain.FieldData{Street: "Villa Kalijudan Indah A9", City: "Surabaya", Province: "Jawa Timur", PostalCode: "60177"}, addr, salesbrain.LangEN); len(problems) != 0 {
		t.Fatalf("unexpected problems: %v", problems)
	}
	if st.Step != "confirm" || !st.ShippingComplete() {
		t.Fatalf("the address completes the checkout data, got step %q", st.Step)
	}
}

func TestFieldAnswerInventedCityIsNotWritten(t *testing.T) {
	st := lfTunaState()
	f := lfFullAddressFields()
	f.City = "Bandung" // the buyer never wrote it
	text := "Name: Ana\nPhone: 0872839472828\nAddress: Villa Kalijudan Indah A9, Jawa Timur 60177"

	applyFieldAnswer(st, f, text, salesbrain.LangEN)

	if st.City != "" {
		t.Fatalf("a city missing from the buyer text must not be stored, got %q", st.City)
	}
	if st.RecipientName != "Ana" || st.Street != "Villa Kalijudan Indah A9" || st.Province != "Jawa Timur" || st.PostalCode != "60177" {
		t.Fatalf("the grounded fields are still written: %+v", *st)
	}
	if st.Step == "confirm" || st.ShippingComplete() {
		t.Fatalf("without a city the checkout data is incomplete (step %q)", st.Step)
	}

	// Every other field is held to the same rule, and case does not matter.
	st = lfTunaState()
	applyFieldAnswer(st, &salesbrain.FieldData{Name: "Budi", Street: "Jl. Mawar 5", Province: "Bali", District: "Kuta"}, "my name is ana, jalan mawar", salesbrain.LangEN)
	if st.RecipientName != "" || st.Street != "" || st.Province != "" || st.Kecamatan != "" {
		t.Fatalf("invented values must never be stored: %+v", *st)
	}
	st = lfTunaState()
	applyFieldAnswer(st, &salesbrain.FieldData{Name: "ANA", City: "SURABAYA"}, "ana from surabaya", salesbrain.LangEN)
	if st.RecipientName != "ANA" || st.City != "SURABAYA" {
		t.Fatalf("matching is case-insensitive, got %+v", *st)
	}
}

func TestFieldAnswerBadPhoneReportsDigits(t *testing.T) {
	st := lfTunaState()
	problems := applyFieldAnswer(st, &salesbrain.FieldData{Name: "Ana", Phone: "98822123"}, "Ana, 98822123", salesbrain.LangEN)

	if len(problems) != 1 {
		t.Fatalf("want exactly one problem, got %v", problems)
	}
	for _, want := range []string{"98822123", "8 digits"} {
		if !strings.Contains(problems[0], want) {
			t.Errorf("EN problem must mention %q: %q", want, problems[0])
		}
	}
	if st.RecipientPhone != "" {
		t.Fatalf("an invalid phone must not be stored, got %q", st.RecipientPhone)
	}
	if st.RecipientName != "Ana" {
		t.Fatalf("a valid name beside a bad phone is still kept, got %q", st.RecipientName)
	}
	if st.Step != "ask_recipient" {
		t.Fatalf("still waiting for the phone, got step %q", st.Step)
	}

	id := applyFieldAnswer(lfTunaState(), &salesbrain.FieldData{Phone: "98822123"}, "hp 98822123", salesbrain.LangID)
	if len(id) != 1 || !strings.Contains(id[0], "nomor HP 98822123 belum valid") || !strings.Contains(id[0], "8 digit") {
		t.Fatalf("ID problem wording: %v", id)
	}
}

func TestFieldAnswerPhoneFormats(t *testing.T) {
	for _, tc := range []struct {
		name, modelPhone, buyerText string
		wantStored                  string // "" = rejected
	}{
		{"local", "0872839472828", "0872839472828", "+62872839472828"},
		{"spaces and dashes", "0872-8394-72828", "call 0872-8394-72828 please", "+62872839472828"},
		{"intl with plus", "+62 872 8394 72828", "+62 872 8394 72828", "+62872839472828"},
		{"model normalised intl to local", "+62872839472828", "my number is 0872839472828", "+62872839472828"},
		{"model normalised local to intl", "0872839472828", "wa +62 872 8394 72828", "+62872839472828"},
		{"too short", "0812345", "0812345", ""},
		{"too long", "08123456789012345", "08123456789012345", ""},
		{"not a mobile prefix", "0212345678901", "0212345678901", ""},
		{"invented phone", "0872839472828", "i have no number yet", ""},
	} {
		st := lfTunaState()
		problems := applyFieldAnswer(st, &salesbrain.FieldData{Phone: tc.modelPhone}, tc.buyerText, salesbrain.LangEN)
		if st.RecipientPhone != tc.wantStored {
			t.Errorf("%s: stored %q, want %q", tc.name, st.RecipientPhone, tc.wantStored)
		}
		invented := tc.name == "invented phone"
		if tc.wantStored == "" && !invented && len(problems) != 1 {
			t.Errorf("%s: an unusable phone from the buyer must be reported, got %v", tc.name, problems)
		}
		if (tc.wantStored != "" || invented) && len(problems) != 0 {
			t.Errorf("%s: unexpected problems %v", tc.name, problems)
		}
	}
}

func TestFieldAnswerQuestionNeverChangesName(t *testing.T) {
	// The incident: a delivery question became the recipient name.
	for _, q := range []string{
		"delivered today or tomorrow woi",
		"Is it frozen or freshly cut?",
		"when will it arrive", // short, no "?", letters only: PlausibleNameLine alone accepts it
		"how long is the shipping?",
	} {
		st := lfTunaState()
		applyFieldAnswer(st, &salesbrain.FieldData{Name: q}, q, salesbrain.LangEN)
		if st.RecipientName != "" {
			t.Errorf("question %q became the recipient name %q", q, st.RecipientName)
		}

		st = lfTunaState()
		st.RecipientName = "Ana"
		applyFieldAnswer(st, &salesbrain.FieldData{Name: q}, q, salesbrain.LangEN)
		if st.RecipientName != "Ana" {
			t.Errorf("question %q replaced the stored name with %q", q, st.RecipientName)
		}
	}

	// A mid-checkout question that also carries a labelled name is still a name answer.
	st := lfTunaState()
	applyFieldAnswer(st, &salesbrain.FieldData{Name: "Ana Putri"}, "Name: Ana Putri\nis it frozen?", salesbrain.LangEN)
	if st.RecipientName != "Ana Putri" {
		t.Fatalf("an explicit name label must still be honoured, got %q", st.RecipientName)
	}

	// Words that cannot be a name are refused even in a plain answer.
	st = lfTunaState()
	applyFieldAnswer(st, &salesbrain.FieldData{Name: "Ana 081"}, "Ana 081", salesbrain.LangEN)
	if st.RecipientName != "" {
		t.Fatalf("digits are not part of a name, got %q", st.RecipientName)
	}
}

func TestFieldAnswerPostalCodeMustBeFiveStandaloneDigits(t *testing.T) {
	st := lfTunaState()
	problems := applyFieldAnswer(st, &salesbrain.FieldData{PostalCode: "6017"}, "Surabaya 6017", salesbrain.LangEN)
	if st.PostalCode != "" || len(problems) != 1 || !strings.Contains(problems[0], "6017") || !strings.Contains(problems[0], "4 digits") {
		t.Fatalf("a 4-digit postal code is reported, not stored: stored=%q problems=%v", st.PostalCode, problems)
	}

	// A postal code lifted out of a longer number (a phone, say) is not grounded.
	st = lfTunaState()
	problems = applyFieldAnswer(st, &salesbrain.FieldData{PostalCode: "72839"}, "phone 0872839472828", salesbrain.LangEN)
	if st.PostalCode != "" || len(problems) != 0 {
		t.Fatalf("a postal code carved from a phone number must be ignored silently: stored=%q problems=%v", st.PostalCode, problems)
	}

	st = lfTunaState()
	problems = applyFieldAnswer(st, &salesbrain.FieldData{PostalCode: "60177"}, "Surabaya 60177", salesbrain.LangID)
	if st.PostalCode != "60177" || len(problems) != 0 {
		t.Fatalf("a valid postal code is stored: stored=%q problems=%v", st.PostalCode, problems)
	}

	id := applyFieldAnswer(lfTunaState(), &salesbrain.FieldData{PostalCode: "601"}, "kode pos 601", salesbrain.LangID)
	if len(id) != 1 || !strings.Contains(id[0], "kode pos 601 belum valid") {
		t.Fatalf("ID postal code problem wording: %v", id)
	}
}

func TestFieldAnswerNeverBlanksStoredFields(t *testing.T) {
	st := lfTunaState()
	st.RecipientName, st.City = "Ana", "Surabaya"
	applyFieldAnswer(st, &salesbrain.FieldData{Street: "Villa Kalijudan Indah A9"}, "Villa Kalijudan Indah A9", salesbrain.LangEN)
	if st.RecipientName != "Ana" || st.City != "Surabaya" || st.Street != "Villa Kalijudan Indah A9" {
		t.Fatalf("empty fields must not erase stored data: %+v", *st)
	}

	// A buyer correcting a field overwrites it.
	applyFieldAnswer(st, &salesbrain.FieldData{City: "Malang"}, "sorry, the city is Malang", salesbrain.LangEN)
	if st.City != "Malang" {
		t.Fatalf("a grounded correction overwrites, got %q", st.City)
	}
}

func TestFieldAnswerDistrictAndNilSafety(t *testing.T) {
	st := lfTunaState()
	applyFieldAnswer(st, &salesbrain.FieldData{District: "Kenjeran"}, "Kecamatan Kenjeran", salesbrain.LangEN)
	if st.Kecamatan != "Kenjeran" {
		t.Fatalf("district goes to Kecamatan, got %q", st.Kecamatan)
	}

	if got := applyFieldAnswer(nil, lfFullAddressFields(), lfFullAddressText, salesbrain.LangEN); got != nil {
		t.Fatalf("nil state: %v", got)
	}
	st = lfTunaState()
	before := *st
	if got := applyFieldAnswer(st, nil, lfFullAddressText, salesbrain.LangEN); got != nil || !reflect.DeepEqual(*st, before) {
		t.Fatalf("nil fields must change nothing, got problems=%v state=%+v", got, *st)
	}
}

func TestFieldAnswerNumberScanStaysFastOnHostileText(t *testing.T) {
	// One 5000-group "number": scanning for a phone or postal code must stay linear.
	text := strings.Repeat("1 ", 5000)
	st := lfTunaState()
	start := time.Now()
	problems := applyFieldAnswer(st, &salesbrain.FieldData{Phone: "0872839472828", PostalCode: "60177"}, text, salesbrain.LangEN)
	if d := time.Since(start); d > time.Second {
		t.Fatalf("number scan took %v on a hostile message", d)
	}
	if st.RecipientPhone != "" || st.PostalCode != "" || len(problems) != 0 {
		t.Fatalf("nothing in that text is grounded: %+v problems=%v", *st, problems)
	}
}
