# L-2: test isolasi tenant pada tool LLM (PR #216)

- Temuan Security L-2: belum terbukti bahwa argumen tool LLM tidak bisa membawa tenantId/schema.
- Hasil telusur: hanya ada 2 tool (`search_catalog`, `get_product`). Executor terikat ke snapshot katalog tenant job dan tidak memegang DB. Tidak ada celah, jadi tidak ada perubahan kode produksi.
- Test baru: `ai/tool_tenant_isolation_test.go` (definisi tool tanpa param tenant/schema; argumen injeksi tidak membocorkan data tenant lain; tool tidak dikenal ditolak) dan `shared/llm/tools_tenant_test.go` (loop Gemini, RunToolChat memakai tenant pemanggil).
- Batasan: test paket `ai` butuh `encore test` (belum dijalankan, tanpa login/Docker).
