package ai

import (
	"context"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

const catalogTextSystem = `Kamu mengekstrak data produk dari teks informal seller Indonesia (WhatsApp, caption marketplace, catatan toko) untuk katalog toko.
Aturan:
- Jawab HANYA SATU objek JSON valid, tanpa markdown.
- Jika teks berisi beberapa produk/varian berbeda (rasa, ukuran, pack), buat satu item per varian di array items.
- Jika teks hanya mendeskripsikan satu produk (bullet fitur, isi pack), buat satu item dengan deskripsi lengkap.
- Bullet fitur (dairy free, rendah gula, dll) masuk ke description (gabung dengan newline).
- "Isi 12" / pack count: tambahkan ke name (mis. "(Isi 12)") dan set sellUnit sesuai (box, pack, pcs).
- externalCode: huruf/angka/underscore/dash, max 32 karakter, uppercase.
- sellPrice: angka IDR tanpa "Rp"; 0 jika tidak disebut.
- sellUnit: default "pcs" jika tidak jelas.
- Jangan mengarang varian yang tidak ada di teks.`

const catalogTextUserTpl = `Baca teks produk berikut. Ekstrak judul induk (jika ada) dan semua varian/produk yang bisa dijual terpisah.

Format JSON:
{
  "parentTitle": "judul produk induk",
  "items": [
    {
      "externalCode": "SKU-SINGKAT",
      "name": "nama lengkap termasuk varian",
      "description": "fitur dan detail produk",
      "sellPrice": 0,
      "sellUnit": "pcs"
    }
  ]
}

Maksimal 50 item.

Teks:
%s`

// ExtractCatalogFromText calls Claude and returns raw JSON for catalog draft rows.
func ExtractCatalogFromText(ctx context.Context, apiKey string, text string) (rawJSON string, usage CompletionUsage, err error) {
	if strings.TrimSpace(apiKey) == "" {
		return "", usage, fmt.Errorf("anthropic API key not configured")
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return "", usage, fmt.Errorf("empty text")
	}
	if len(text) > 12000 {
		text = text[:12000]
	}

	model := DefaultHaikuAPIID()
	client := anthropic.NewClient(option.WithAPIKey(apiKey))

	resp, err := client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:       anthropic.Model(model),
		MaxTokens:   4096,
		Temperature: anthropic.Float(0.1),
		System: []anthropic.TextBlockParam{
			{Text: catalogTextSystem},
		},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(fmt.Sprintf(catalogTextUserTpl, text))),
		},
	})
	if err != nil {
		return "", usage, fmt.Errorf("anthropic text: %w", err)
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
		return "", usage, fmt.Errorf("empty text response")
	}
	return SanitizeVisionJSON(rawJSON), usage, nil
}
