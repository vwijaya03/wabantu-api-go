// Package aivision provides Claude vision helpers without importing the main ai package
// (avoids import cycles: ai → order → finance).
package aivision

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

const haikuModelID = "claude-haiku-4-5-20251001"

// Usage reports token consumption for one vision call.
type Usage struct {
	InputTokens  int
	OutputTokens int
}

const transactionVisionSystem = `Kamu mengekstrak daftar transaksi keuangan dari screenshot aplikasi keuangan (WABantu, bank, e-wallet, dll.) untuk impor massal.
Aturan:
- Jawab HANYA SATU objek JSON valid, tanpa markdown.
- Setiap baris transaksi di UI = satu item di array "items".
- type HARUS "income" (uang masuk) atau "expense" (uang keluar) — bukan transfer/investasi kecuali jelas sekali.

Menentukan income vs expense (gabungkan semua petunjuk visual & teks):
- WABantu / daftar transaksi: ikon lingkaran hijau dengan tanda PLUS (+) dan nominal hijau dengan awalan + → income.
- WABantu: ikon lingkaran merah dengan MINUS (−) dan nominal merah dengan awalan − → expense.
- Warna teks nominal: hijau → income, merah → expense (jika tidak bertentangan dengan tanda).
- Awalan nominal: + atau tanpa tanda pada pemasukan → income; − atau kurung → expense.
- Label jenis: "Pemasukan", "Masuk", "Terima", "Credit" (konteks penerimaan) → income; "Pengeluaran", "Keluar", "Bayar", "Debit" (konteks pengeluaran pribadi) → expense.
- Jangan terbalik: di mutasi rekening bank, "DB" / debit sering berarti uang KELUAR (expense); "CR" / kredit sering berarti uang MASUK (income) — sesuaikan dari sudut pandang pemilik dompet.
- amount: angka positif IDR tanpa "Rp", tanpa titik ribuan (21722894 untuk Rp 21.722.894).
- transactionDate: YYYY-MM-DD jika terlihat; jika tidak, kosongkan "".
- walletNameHint / categoryNameHint: teks dompet/kategori di baris jika ada (mis. "Jenius", "Pemasukan").
- typeSignals: array singkat petunjuk yang dipakai, mis. ["green_amount","plus_prefix","label_pemasukan"].
- Jangan mengarang transaksi yang tidak terlihat. Abaikan header, total saldo, filter UI.`

const transactionVisionUserTpl = `Baca screenshot daftar transaksi ini. Ekstrak setiap baris transaksi yang terlihat.

Format JSON:
{
  "items": [
    {
      "type": "income",
      "typeSignals": ["green_amount", "plus_prefix"],
      "amount": 0,
      "description": "deskripsi/judul transaksi",
      "transactionDate": "2026-05-25",
      "walletNameHint": "Jenius",
      "categoryNameHint": "Pemasukan"
    }
  ]
}

Maksimal 50 item. type hanya "income" atau "expense". amount harus > 0 jika terlihat.`

// ExtractTransactionsFromScreenshot calls Claude vision (Haiku) for finance transaction import.
func ExtractTransactionsFromScreenshot(ctx context.Context, apiKey string, imageBytes []byte, mediaType string) (rawJSON string, usage Usage, err error) {
	return visionExtract(ctx, apiKey, imageBytes, mediaType, transactionVisionSystem, transactionVisionUserTpl)
}

// SanitizeVisionJSON strips markdown fences from model output.
func SanitizeVisionJSON(text string) string {
	return unwrapJSONFromMarkdown(text)
}

func visionExtract(ctx context.Context, apiKey string, imageBytes []byte, mediaType, system, user string) (string, Usage, error) {
	var usage Usage
	if strings.TrimSpace(apiKey) == "" {
		return "", usage, fmt.Errorf("kunci Anthropic belum dikonfigurasi — set secret AnthropicAPIKey (encore secret set --type local AnthropicAPIKey)")
	}
	if len(imageBytes) == 0 {
		return "", usage, fmt.Errorf("empty image")
	}
	mediaType = normalizeImageMediaType(mediaType)

	client := anthropic.NewClient(option.WithAPIKey(apiKey))
	b64 := base64.StdEncoding.EncodeToString(imageBytes)

	resp, err := client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:       anthropic.Model(haikuModelID),
		MaxTokens:   4096,
		Temperature: anthropic.Float(0.1),
		System: []anthropic.TextBlockParam{
			{Text: system},
		},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(
				anthropic.NewTextBlock(user),
				anthropic.NewImageBlockBase64(mediaType, b64),
			),
		},
	})
	if err != nil {
		return "", usage, fmt.Errorf("anthropic vision: %w", err)
	}

	usage = Usage{
		InputTokens:  int(resp.Usage.InputTokens),
		OutputTokens: int(resp.Usage.OutputTokens),
	}

	var parts []string
	for _, block := range resp.Content {
		if block.Type == "text" {
			parts = append(parts, block.Text)
		}
	}
	rawJSON := strings.TrimSpace(strings.Join(parts, "\n"))
	if rawJSON == "" {
		return "", usage, fmt.Errorf("empty vision response")
	}
	return SanitizeVisionJSON(rawJSON), usage, nil
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
