---
name: wabantu-docs
description: Use when answering WABantu architecture or documentation questions, writing or updating markdown under api-go/docs, api-go/docs-development-shipped, web-frontend/docs, or web-frontend/docs-development-shipped, checking whether a feature or plan is already shipped, distinguishing roadmap vs rilis, or when the user mentions indeks docs, shipped notes, docs-development-shipped, or "sudah didevelop".
---

# WABantu Docs

**`docs/` = spec / riset / roadmap** (boleh belum di-build). **`docs-development-shipped/` = catatan implementasi.** File di folder shipped **bukan** otomatis rilis — baca baris **Status**.

## Wajib sebelum jawab, plan, atau nulis ulang fitur

1. Buka indeks `api-go/docs/README.md`. Untuk UI, buka juga `web-frontend/docs-development-shipped/README.md`.
2. Grep **kedua** folder (`docs/` dan `docs-development-shipped/`) dengan kata topik (Bahasa Indonesia + English).
3. Baca file yang cocok. Kutip **Status** + path. Baru boleh grep kode — untuk konfirmasi, bukan pengganti indeks.
4. Kalau sudah ada catatan shipped dengan Status rilis/merged: **jangan** bikin plan implementasi baru. Perluas atau perbaiki yang ada.

## Keputusan

| Temuan | Tindakan |
|--------|----------|
| Shipped, Status rilis / merged | Anggap sudah di-develop. Tunjuk file + tabel File kunci. |
| Shipped, Status **Planned** / belum merge | Jangan klaim sudah rilis. Jebakan: `ai-image-context.md`. |
| Hanya di `docs/` | Spec/roadmap. Cek kode sebelum bilang "sudah ada". |
| Tidak ada di keduanya | Kerja baru. Setelah merge, tulis catatan shipped. |

## Catatan shipped baru

Nama file: `YYYY-MM-DD_HHMMSS_slug.md` (waktu UTC+7). Satu batch rilis per file.

Isi: Masalah / Kebutuhan · Perubahan · File utama · Testing · Catatan deploy. Jangan copy-paste spec penuh.

Update indeks: `api-go/docs/README.md` (tabel "Kalau ditanya") **dan** `api-go/docs-development-shipped/README.md`. Mirror UI di `web-frontend/docs-development-shipped/` jika ada permukaan frontend.

## Jangan

| Excuse | Reality |
|--------|---------|
| "Kode sudah kelihatan, docs skip" | Indeks README adalah peta; tanpa Status gampang salah klaim rilis |
| "Folder shipped = sudah produksi" | Baca Status; `docs-development-shipped/ai-image-context.md` masih planned |
| "Tulis plan baru lebih cepat" | Cek shipped dulu (payment proof, recipient policy, RAG outbox, inbox S3, stock guard, order lookup) |
| "`AI_TRIAGE_LOOP_NEXT_DEV.md` runbook operator" | Itu spec loop; operator live → skill `wabantu-ai-triage` |

Jangan commit `web-frontend/public/generated-docs/docs-index.json` atau `api-go/shared/retrieval/budget_config.go` sebagai "docs".
