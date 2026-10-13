package simulator

import (
	"encoding/base64"
	"reflect"
	"strings"
	"testing"
)

var png1x1, _ = base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==")

func TestValidateOK(t *testing.T) {
	n, err := Validate(Input{SessionID: "judge-001", Text: "  halo kak, ada gamis? \x07"})
	if err != nil || n.Text != "halo kak, ada gamis?" || n.BuyerName != DefaultBuyer || n.Type() != "text" {
		t.Fatalf("%+v %v", n, err)
	}
	n, err = Validate(Input{SessionID: "judge-001", Image: &Image{Base64: "data:image/png;base64," + base64.StdEncoding.EncodeToString(png1x1), MimeType: "image/png"}})
	if err != nil || n.Type() != "image" || n.ImageMime != "image/png" {
		t.Fatalf("%+v %v", n, err)
	}
}

func TestValidateRejects(t *testing.T) {
	big := base64.StdEncoding.EncodeToString(make([]byte, MaxImageBytes+10))
	cases := map[string]Input{
		"bad session":   {SessionID: "a b", Text: "x"},
		"short session": {SessionID: "abc", Text: "x"},
		"empty":         {SessionID: "judge-001"},
		"long text":     {SessionID: "judge-001", Text: strings.Repeat("a", MaxTextRunes+1)},
		"bad mime":      {SessionID: "judge-001", Image: &Image{Base64: base64.StdEncoding.EncodeToString(png1x1), MimeType: "image/gif"}},
		"bad b64":       {SessionID: "judge-001", Image: &Image{Base64: "!!!", MimeType: "image/png"}},
		"too big":       {SessionID: "judge-001", Image: &Image{Base64: big, MimeType: "image/png"}},
		"not image":     {SessionID: "judge-001", Image: &Image{Base64: base64.StdEncoding.EncodeToString([]byte("<html>hi</html>")), MimeType: "image/png"}},
		"long name":     {SessionID: "judge-001", Text: "x", BuyerName: strings.Repeat("n", 61)},
	}
	for name, in := range cases {
		if _, err := Validate(in); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
}

// The buyer input type must not carry anything that could set price/status/order data.
func TestInputHasNoPriceOrStatusFields(t *testing.T) {
	allowed := map[string]bool{"SessionID": true, "BuyerName": true, "Text": true, "Image": true}
	ty := reflect.TypeOf(Input{})
	for i := 0; i < ty.NumField(); i++ {
		if !allowed[ty.Field(i).Name] {
			t.Errorf("unexpected field %s on simulator input", ty.Field(i).Name)
		}
	}
}

func TestBuyerPhoneStableAndScoped(t *testing.T) {
	a := BuyerPhone("t1", "judge-001")
	if a != BuyerPhone("t1", "judge-001") || a == BuyerPhone("t2", "judge-001") || a == BuyerPhone("t1", "judge-002") {
		t.Fatal("phone derivation")
	}
	if len(a) != 15 || !strings.HasPrefix(a, "62000") {
		t.Fatal(a)
	}
}
