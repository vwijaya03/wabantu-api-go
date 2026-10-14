# Gemini default → gemini-3.1-flash-lite

Catatan rilis (branch `be/gemini-model`, belum di-merge).

## Masalah
Key lomba (project `wabantu`) mendapat `404 ... models/gemini-2.5-flash is no longer available to new users` (juga `gemini-2.5-flash-lite`). Default kode + seed migration 27 memakai `gemini-2.5-flash`, jadi Gemini selalu gagal lalu fallback ke Anthropic. Model baru ditolak oleh `ModelAllowlist`.

## Pilihan model
ListModels (header `x-goog-api-key`) + uji generateContent: `gemini-3.1-flash-lite` = model Flash teks **stabil** termurah yang bisa dipanggil key ini ($0.25 input / $1.50 output per 1M token, paid tier Standard; sumber: ai.google.dev/gemini-api/docs/pricing, update 2026-10-09). `gemini-3.5-flash-lite` juga jalan tapi lebih mahal ($0.30/$2.50). Preview/latest/image/TTS tidak dipilih.

## Perubahan
- `shared/llm`: `GeminiDefaultModel = "gemini-3.1-flash-lite"`, ditambah ke `ModelAllowlist` + label katalog (hanya model ini; entri lama tetap).
- `usage/costmonitor.go`: harga `gemini-3.1-flash-lite` (sebelumnya model Gemini dihitung pakai harga Sonnet).
- Migration `29_gemini_default_model`: hanya baris `intent`/`memory` seed migration 27 yang belum diubah admin.
- Test `TestGeminiDefaultModelAllowlisted`.
