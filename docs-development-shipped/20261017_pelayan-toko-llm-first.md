# Pelayan Toko LLM-first

**Status:** branch `feat/pelayan-toko-llm-first` (belum di-push, belum ada PR; pekerjaan ini berakhir di PR tanpa merge). Jalur baru **default mati** di belakang flag per tenant; perbaikan jalur lama (Part C dan D) langsung aktif untuk semua tenant.

Panduan operasi dan SQL pengukuran: [`docs/LLM_FIRST_PIPELINE.md`](../docs/LLM_FIRST_PIPELINE.md). Desain: [`docs/superpowers/specs/2026-10-17-pelayan-toko-llm-first-design.md`](../docs/superpowers/specs/2026-10-17-pelayan-toko-llm-first-design.md).

## Masalah / Kebutuhan

Insiden tenant `omah_apparel` (16 sampai 17 Okt 2026):

- 74 dari 98 balasan otomatis dibuat ±25 interceptor regex di `ProcessAutoReply` sebelum LLM sempat dipanggil.
- 8 dari 24 balasan LLM diganti kalimat baku Indonesia oleh validator harga.
- LLM tidak pernah melihat deskripsi produk.
- Bahasa pembeli berganti sendiri (jawaban "1" atas "How many pcs?" dibaca sebagai pilihan bahasa).
- AI di-pause sampai 6 jam tanpa notifikasi ke owner.

Target: AI bersikap seperti pelayan toko yang menjawab dari katalog (termasuk deskripsi) dan FAQ, kunci bahasa, sadar pesanan yang belum selesai, handoff yang jujur, biaya ≤ Rp20 per pesan untuk tenant kecil.

## Perubahan

**Part A, `internal/salesbrain` (Go murni, `go test`):** kebijakan kunci bahasa, pemilihan pesanan terbuka dan batas pengingat, template sistem dan transaksi ID/EN, prompt persona ringkas dengan blok katalog dan FAQ, parser keputusan JSON, validator balasan (nominal, rekening, bahasa, panjang), operasi keranjang, ringkasan yang dirender kode, redaksi log.

**Part B, `shared/llm`:** chat Gemini mendukung mode JSON, temperature, `finishReason`, dan token thinking; `llm.RunChat` menjalankan chain fungsi tenant dengan fallback.

**Part C, `internal/buyerflow` (jalur lama):** kunci bahasa memakai kebijakan salesbrain, frasa batal seluruh pesanan EN/ID, minta bicara dengan admin/owner/orang, salam waktu mengikuti kata waktu pembeli, FAQ bayar dan kirim tidak pernah kosong dan mengutamakan rekening, baris pesanan ganda tidak hilang, upsell maksimal satu per pesanan, angka HP dan alamat tidak dibaca sebagai revisi jumlah, pertanyaan saat checkout tidak dijadikan nama penerima.

**Part D, `ai` (jalur lama):** deskripsi produk masuk ke konteks LLM, tool, dan pencarian katalog; grounding tidak lagi mengganti balasan dengan kalimat baku Indonesia; tidak ada pause tanpa batas (handoff jujur, auto-resume 2 jam, notifikasi owner); batal seluruh pesanan didahulukan dari amend; balasan kosong tidak pernah dikirim; riwayat ke LLM tanpa pesan ganda; jawaban pilihan bahasa hanya dibaca setelah bot bertanya.

**Part E, pipeline LLM-first:**

- `flag`: `ai_llm_first`, `ai_llm_first_shadow`, `ai_llm_first_kill`, `LLMFirstMode`, dan cache untuk flag yang belum dibuat.
- `ai/llm_first_plan.go`: `planTurn`, keputusan murni dengan retry dan validasi.
- `ai/llm_first_orders.go`: adaptor keranjang dan data penerima.
- `ai/llm_first.go` dan `ai/llm_first_effects.go`: cangkang I/O. Hook `tryLLMFirst` di `ProcessAutoReply` tepat setelah gerbang `IsThirdPartyBuyerLookup`; tenant tanpa flag hanya membayar satu pengecekan flag. Mode shadow hanya mencatat log dan aktivitas AI (purpose `llm_first_shadow`). Mode on memuat data, mengunci bahasa, memanggil `planTurn`, menjalankan efek per intent (keranjang, data penerima, simpan pesanan, batal, lanjut, pesanan baru, status, handoff), lalu mengirim. Error sebelum tulisan pertama mengembalikan giliran ke jalur lama.
- `AiReplyMeta` mendapat `pipeline`, `intent`, `lang`, `replacedReason`, `rawOutput` (diredaksi, ≤ 1000 karakter), `attempts`; path baru `llm_first`.
- `internal/salesbrain` mendapat tiga template dua bahasa untuk nomor pesanan, data tidak valid, dan ringkasan pengiriman.

## File utama

- `ai/llm_first.go`, `ai/llm_first_effects.go`, `ai/llm_first_turn.go`, `ai/llm_first_plan.go`, `ai/llm_first_orders.go`
- `ai/autoreply.go` (hook), `ai/reply_meta.go`, `ai/buyerflow_bridge.go`, `ai/triage.go`
- `internal/salesbrain/`
- `flag/llm_first.go`, `flag/flag.go`
- `shared/llm/run_chat.go`, `shared/llm/gemini.go`
- `docs/LLM_FIRST_PIPELINE.md`

## Testing

- `go test ./internal/salesbrain/ ./internal/buyerflow/ -count=1`
- `encore test ./ai/ -count=1` (seluruh paket `ai`)
- `encore test ./flag/ -count=1`
- `encore test ./internal/apitest/ -count=1` (apiregistry tetap lulus, tidak ada endpoint baru)
- Tes giliran utuh `ai/llm_first_flow_test.go` (Redis palsu, model palsu, pengirim simulator) membuktikan shadow tidak menulis; `ai/llm_first_shell_test.go` menjaga letak dan ukuran hook serta jalur shadow.

## Catatan deploy

- Tidak ada migrasi skema, secret, env, atau endpoint baru. Merge tidak mengubah perilaku jalur baru; Part C dan D langsung aktif.
- Aktivasi lewat `POST/PATCH /api/v1/flags` (super admin): shadow, canary satu tenant, lalu bertahap. Kill switch `ai_llm_first_kill` (`enabledGlobally=true`) mematikan jalur baru global dalam ≤ 60 detik. Langkah dan contoh `curl` dengan token placeholder ada di `docs/LLM_FIRST_PIPELINE.md`.
- Chain `chat` di AI Provider disetel superadmin (disarankan `gemini-3.1-flash-lite`, lalu `gemini-3.6-flash`). `GeminiAPIKey` hanya ada di Cloud Run; di Encore Cloud chain jatuh ke Anthropic dan JSON diminta lewat prompt (default Sonnet bila link tanpa model: biaya jauh lebih tinggi).
- Kuota tidak diubah: LLM-first memakai ±5 kali token per pesan, jadi tenant Trial dan Starter lebih cepat habis kuota bila flag diaktifkan untuk mereka.
- Batasan yang diketahui (status memakai jalur lama berbahasa Indonesia, draft belum disimpan selama belanja, produk bervarian): `docs/LLM_FIRST_PIPELINE.md` §8.
