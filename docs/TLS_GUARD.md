# TLS guard: tolak CA eMudhra yang bobol (GO-2026-6809)

Package: `shared/tlsguard` (+ `shared/tlsguard/startup`). Shipped: [2026-10-24_113726_tls-emudhra-guard.md](../docs-development-shipped/2026-10-24_113726_tls-emudhra-guard.md).

## Kenapa
GO-2026-6809 / CVE-2026-111943: 8 intermediate eMudhra (DV/OV/EV TLS + 2 code-signing, G3B/G3C) kuncinya bocor. Go memperbaikinya di go1.26.10 / go1.27.3 (CL 851047) dengan denylist hash SHA-256 SPKI di `crypto/x509`. Toolchain Encore Cloud belum terverifikasi, jadi app memasang filter yang sama sendiri.

## Perilaku (sama dengan patch Go)
- Patch Go (`src/crypto/x509/verify.go`, `distrustedSPKIHashes` + `containsDistrustedSPKIHash`): `Certificate.Verify` membuang setiap candidate chain yang memuat sertifikat (posisi mana pun) dengan SPKI di denylist; kalau tidak ada chain tersisa → `CertificateInvalidError{NoValidChains}`. Satu chain bersih cukup untuk lolos.
- `tlsguard.VerifyConnection` menerapkan aturan itu ke `tls.ConnectionState.VerifiedChains`: tolak hanya jika **semua** verified chain memuat hash terlarang (termasuk cross-sign Sectigo "AAA Certificate Services", SPKI-nya sama).
- `VerifiedChains` kosong (verifikasi dimatikan / InsecureSkipVerify; di repo tidak ada): selalu `Warn`, tidak dianggap bersih. Mode `enforce` menolak hanya kalau sertifikat yang dikirim peer sendiri memuat SPKI terlarang (lebih ketat dari Go, karena patch Go ada di dalam `Verify` dan tidak berlaku di sini); peer tanpa verifikasi yang bukan eMudhra tidak ditolak.
- Tidak mengubah RootCAs, MinVersion, ServerName, InsecureSkipVerify, Proxy, HTTP/2, timeout.

## Mode `TLSGUARD_MODE` (dibaca sekali per proses)
| nilai | perilaku |
|---|---|
| `log` (default, juga untuk nilai kosong/tidak dikenal) | `rlog.Warn` (host, CA, spki) lalu koneksi **tetap jalan** |
| `enforce` | handshake ditolak (`ErrDistrustedChain`) |
| `off` | tidak ada apa-apa; DefaultTransport tidak di-hook |

Default `log` = aman di-deploy tanpa config. **`enforce` adalah langkah terpisah yang butuh izin Viko**, setelah ±24 jam log bersih (tidak ada `tlsguard: all verified chains use a distrusted eMudhra intermediate`). Tidak ada perubahan secret/env Encore Cloud di PR ini.

## Yang di-hook
| klien | cara |
|---|---|
| `http.DefaultTransport` (→ `http.DefaultClient`, semua `&http.Client{}` tanpa Transport, SDK Anthropic, Gemini, Gemini embedder, Pinecone, Meta Graph (whatsapp/whatsappapi), payment, shipping, webhook keluar, Firestore REST + Google OAuth, Identity Toolkit, GitHub triage) | `InstallDefaultTransport()`: `Clone()` sekali, set `VerifyConnection`, pasang sebagai `http.DefaultTransport` (idempoten) |
| `shared/retrieval/openai_embedder.go:28` | `HardenTransport` pada transport sendiri |
| `business/import.go:195` | `HardenTransport` (MinVersion TLS1.2 tetap) |
| `shared/redisurl/redisurl.go:31` | `HardenTLSConfig(opt.TLSConfig)` untuk `rediss://` (ServerName/MinVersion go-redis tetap; `redis://` tetap tanpa TLS) |
| `shared/mediastorage/s3.go:65-70` | `config.WithHTTPClient(awshttp.NewBuildableClient().WithTransportOptions(...))` = transport default AWS + guard |
| `shared/sentry/sentry.go:27` | `HTTPTransport` = setara default sentry-go (ProxyFromEnvironment, root sistem) + guard |

Wiring: tiap service Encore (29 dir, file `tlsguard.go`) blank-import `shared/tlsguard/startup`, jadi hook ada di setiap proses apa pun pengelompokan service saat deploy.

## Yang TIDAK di-hook (jangan klaim semua koneksi terlindungi)
- Postgres Encore `sqldb` (runtime Encore, CA infra Encore).
- Klien internal runtime Encore yang membuat transport sendiri (tidak diaudit; yang memakai `http.DefaultClient` setelah init ikut ter-hook [INFERRED]).
- Kode yang menyimpan `http.DefaultTransport` lama sebelum init `startup` jalan (tidak ditemukan di app code; `ai/simulator_route_test.go` hanya test).
- gRPC / Google Cloud SDK: tidak ada di `go.mod`/`go.sum` saat ini. Kalau nanti ditambah (Firestore SDK, genai, dll.), transport-nya harus di-hook terpisah.
- Script lokal (`scripts/cmd/...`), proses non-Encore.
- Frontend / Vercel (Node) – di luar repo ini.

## Self-check startup (log saja)
Satu baris `tlsguard self-check` saat start (goroutine, timeout 5 s, `recover`, tanpa network, fixture di-`go:embed`):
- `go_version` = `runtime.Version()`.
- `emudhra_check`: `x509.Verify` stdlib atas rantai uji eMudhra DV G3B dengan root (emSign ECC Root CA - G3 + AAA) **dipercaya eksplisit**, waktu tetap 2026-10-23 UTC. `PATCHED` = ditolak dengan `NoValidChains` (denylist ada); `VULNERABLE` = diterima; `UNKNOWN` = hasil lain (error lain/timeout/panic).
- `emudhra_sysroot_emsign_ecc_g3`: apakah bundle root sistem masih mempercayai emSign ECC Root CA - G3 (`TRUSTED`/`NOT_TRUSTED`/`UNKNOWN`).
- `tlsguard`: `ACTIVE` (DefaultTransport ter-hook, mode log/enforce, guard mengenali rantai fixture), `OFF`, atau `INACTIVE`.
- `tlsguard_mode`.

Contoh lokal: `go test` (go1.27.3) → `emudhra_check=PATCHED`; `encore test` (CLI v1.58.6, `go1.27.0-encore`) → `emudhra_check=VULNERABLE`, `tlsguard=ACTIVE`.

## Fixture
`shared/tlsguard/fixtures/README.md` (embed) dan `shared/tlsguard/testdata/README.md` (14 chain dari https://emudhra.we-have-a-problem.com/chains.zip, chain Google sebagai kontrol). Sertifikat publik saja, tanpa private key.
