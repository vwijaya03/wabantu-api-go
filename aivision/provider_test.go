package aivision

import (
	"context"
	"testing"

	"encore.app/wabantu/shared/llm"
)

func TestVisionWithSanitizes(t *testing.T) {
	m := &llm.MockProvider{Name: "gemini", Reply: "```json\n{\"a\":1}\n```"}
	out, u, err := VisionWith(context.Background(), m, "gemini-2.5-flash", []byte{1}, "IMAGE/JPG", "s", "u")
	if err != nil || out != `{"a":1}` || u.Provider != "gemini" || u.ImageCount != 1 {
		t.Fatalf("%q %+v %v", out, u, err)
	}
}

func TestAnthropicVisionNoKey(t *testing.T) {
	_, u, err := AnthropicVision{}.Vision(context.Background(), "", []byte{1}, "image/png", "s", "u")
	if err == nil || u.Model != haikuModelID || u.Provider != "anthropic" {
		t.Fatalf("%+v %v", u, err)
	}
}
