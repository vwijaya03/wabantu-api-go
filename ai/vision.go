package ai

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

const catalogVisionSystem = `Kamu mengekstrak data produk dari screenshot marketplace (Shopee/Tokopedia/dll.) untuk katalog toko.
Aturan:
- Jawab HANYA SATU objek JSON valid, tanpa markdown, tanpa objek JSON kedua.
- Jika screenshot berisi beberapa produk induk, gabungkan SEMUA varian ke satu array "items".
- Setiap baris varian (ukuran/warna) = satu item di array items.
- externalCode: gunakan "Kode Variasi" / Model ID jika terlihat; jika tidak, buat dari ukuran (huruf/angka saja, max 64).
- sellPrice: angka IDR tanpa "Rp" dan tanpa titik ribuan (contoh 42640 untuk Rp42.640).
- sellUnit: default "pcs" jika tidak ada.
- name: judul induk + varian (contoh "CD LETO - M,Acak").
- Jangan mengarang item yang tidak terlihat di gambar.`

const catalogVisionUserTpl = `Baca screenshot daftar produk ini. Ekstrak judul induk (jika ada) dan semua varian/baris produk.

Format JSON:
{
  "parentTitle": "judul produk induk",
  "items": [
    {
      "externalCode": "SKU-atau-kode-variasi",
      "name": "nama lengkap termasuk varian",
      "description": "",
      "sellPrice": 0,
      "sellUnit": "pcs"
    }
  ]
}

Maksimal 50 item. Jika harga tidak terlihat, sellPrice: 0.

PENTING: Jangan kirim dua objek JSON terpisah — hanya satu objek dengan semua item digabung.`

// ExtractCatalogFromScreenshot calls Claude vision (Haiku) and returns raw JSON text.
func ExtractCatalogFromScreenshot(ctx context.Context, apiKey string, imageBytes []byte, mediaType string) (rawJSON string, usage CompletionUsage, err error) {
	if strings.TrimSpace(apiKey) == "" {
		return "", usage, fmt.Errorf("anthropic API key not configured")
	}
	if len(imageBytes) == 0 {
		return "", usage, fmt.Errorf("empty image")
	}
	mediaType = normalizeImageMediaType(mediaType)
	model := DefaultHaikuAPIID()

	client := anthropic.NewClient(option.WithAPIKey(apiKey))
	b64 := base64.StdEncoding.EncodeToString(imageBytes)

	resp, err := client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:       anthropic.Model(model),
		MaxTokens:   4096,
		Temperature: anthropic.Float(0.1),
		System: []anthropic.TextBlockParam{
			{Text: catalogVisionSystem},
		},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(
				anthropic.NewTextBlock(catalogVisionUserTpl),
				anthropic.NewImageBlockBase64(mediaType, b64),
			),
		},
	})
	if err != nil {
		return "", usage, fmt.Errorf("anthropic vision: %w", err)
	}

	usage = CompletionUsage{
		InputTokens:  int(resp.Usage.InputTokens),
		OutputTokens: int(resp.Usage.OutputTokens),
	}

	var parts []string
	for _, block := range resp.Content {
		if block.Type == "text" {
			parts = append(parts, block.Text)
		}
	}
	rawJSON = strings.TrimSpace(strings.Join(parts, "\n"))
	if rawJSON == "" {
		return "", usage, fmt.Errorf("empty vision response")
	}
	return SanitizeVisionJSON(rawJSON), usage, nil
}

// SanitizeVisionJSON strips markdown fences and surrounding prose from model output.
func SanitizeVisionJSON(text string) string {
	return unwrapJSONFromMarkdown(text)
}

// ExtractJSONObject returns the outermost {...} block for lenient JSON parsing.
func ExtractJSONObject(text string) string {
	text = SanitizeVisionJSON(text)
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start >= 0 && end > start {
		return text[start : end+1]
	}
	return text
}

func normalizeImageMediaType(mt string) string {
	mt = strings.ToLower(strings.TrimSpace(mt))
	switch mt {
	case "image/jpeg", "image/jpg", "image/png", "image/webp", "image/gif":
		return mt
	default:
		return "image/jpeg"
	}
}

func unwrapJSONFromMarkdown(text string) string {
	text = strings.TrimSpace(text)
	if !strings.Contains(text, "```") {
		return text
	}
	lines := strings.Split(text, "\n")
	var inner []string
	inside := false
	for _, l := range lines {
		if strings.HasPrefix(strings.TrimSpace(l), "```") {
			inside = !inside
			continue
		}
		if inside {
			inner = append(inner, l)
		}
	}
	if len(inner) > 0 {
		return strings.TrimSpace(strings.Join(inner, "\n"))
	}
	return text
}
