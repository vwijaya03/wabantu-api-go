# demo-seed: baca token/tenant dari envelope `data`

Catatan rilis (branch `be/fix-demo-seed`, belum di-merge).

## Masalah
`go run ./scripts/cmd/demo-seed/` gagal dengan `demo-seed: no token/tenant in auth response`.
`/api/v1/auth/register` dan `/api/v1/auth/login` mengembalikan `{"success":true,"data":{"accessToken":...,"user":{"tenant":{"id":...}}}}`,
sedangkan seed membaca `accessToken`/`user` di level atas.

## Perbaikan
- `parseAuth()` di `scripts/cmd/demo-seed/main.go` membaca envelope `data`; body level atas tetap diterima (kompatibel ke belakang).
- Error jelas jika token atau tenant id kosong, atau JSON tidak valid.
- Unit test `TestParseAuth` (envelope, level atas, dan 4 kasus gagal).

## Verifikasi
- `go vet` + `go test ./scripts/cmd/demo-seed/` PASS.
- Dijalankan ke `encore run` lokal (127.0.0.1:4000, data sintetis): login OK, tenant terbaca, produk & kontak `reseller-demo` idempoten.

Catatan: POST `/api/v1/inbox/contacts` meng-upsert, jadi saat dijalankan ulang seed tetap mencetak "reseller contact created" walau tidak ada duplikat (dicek: 1 kontak).
