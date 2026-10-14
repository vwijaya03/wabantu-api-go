package aivision

import "encoding/json"

func jsonQuote(s string) (string, error) {
	b, err := json.Marshal(s)
	return string(b), err
}
