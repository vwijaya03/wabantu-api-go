package buyerflow

import "strings"
import "testing"

func TestAddressIndonesiaDoesNotSwitchLanguage(t *testing.T) {
	msg := "Address: Villa kalijudan indah A9, surabaya, indonesia"
	if lang, ok := ParseLanguagePreferenceAnswer(msg); ok {
		t.Fatalf("address switched language to %s", lang)
	}
	if lang, ok := ParseExplicitLanguageSwitch(msg); ok {
		t.Fatalf("address explicit switch %s", lang)
	}
}

func TestCantSpeakIndonesianStaysEnglish(t *testing.T) {
	msg := "No, i can't speak bahasa indonesia"
	lang, ok := ParseExplicitLanguageSwitch(msg)
	if !ok || lang != LangEN {
		pref, prefOK := ParseLanguagePreferenceAnswer(msg)
		if prefOK && pref == LangID {
			t.Fatalf("negated indonesian parsed as ID")
		}
		if !ok || lang != LangEN {
			if pref != LangEN {
				t.Fatalf("explicit=%v %q preference=%v %q", ok, lang, prefOK, pref)
			}
		}
	}
}

func TestGreetingWithProductQuestionIsNotGreeting(t *testing.T) {
	msg := "Hello morning\n\nI would like to ask about your product. Do you have any tuna steak?"
	if IsGreetingLike(msg) {
		t.Fatal("product question after hello was treated as a greeting")
	}
	if !IsGreetingLike("Hello morning") {
		t.Fatal("bare hello morning should stay a greeting")
	}
}

func TestAskAboutTunaDoesNotAppendToSalmonDraft(t *testing.T) {
	s := newOmahSteakSimulator()
	s.SavedBuyerLang = "en"
	s.Order = &OrderState{
		Step:          "ask_recipient",
		CatalogItemID: "salmon-100",
		ProductName:   "Salmon Steak 100 Gram",
		Qty:           1,
		Items: []OrderLineState{{
			CatalogItemID: "salmon-100",
			ProductName:   "Salmon Steak 100 Gram",
			Qty:           1,
		}},
	}
	out := s.Turn("Heloo \nI want to ask about your tuna steak product")
	if s.Order != nil {
		for _, ln := range s.Order.Items {
			if strings.Contains(strings.ToLower(ln.ProductName), "tuna") {
				t.Fatalf("asking about tuna appended it: %+v path=%s", s.Order.Items, out.Path)
			}
		}
		if strings.Contains(strings.ToLower(s.Order.ProductName), "tuna") && !strings.Contains(strings.ToLower(s.Order.ProductName), "salmon") {
			t.Fatalf("draft replaced with tuna on a question: %s", s.Order.ProductName)
		}
	}
}

func TestVillaSurabayaAddressCompletes(t *testing.T) {
	st := &OrderState{RecipientName: "Ana", RecipientPhone: "+628728394728"}
	mergeShippingText(st, "Gak bisa baca apa\n\nVilla kalijudan indah A9, jawa timur, surabaya, indonesia, 60177")
	if !st.ShippingComplete() {
		t.Fatalf("street=%q city=%q province=%q postal=%q", st.Street, st.City, st.Province, st.PostalCode)
	}
	if lang, ok := ParseLanguagePreferenceAnswer("Villa kalijudan indah A9, jawa timur, surabaya, indonesia, 60177"); ok {
		t.Fatalf("postal address switched to %s", lang)
	}
}
