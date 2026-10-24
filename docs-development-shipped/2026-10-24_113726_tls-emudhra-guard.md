# fix(tls): tolak CA eMudhra yang bobol + self-check startup

**Tanggal:** 2026-10-24 11:37 WIB · **Branch:** `fix/tls-emudhra-guard` dari `origin/master` 3f66543 · Draft PR ke master (belum di-push saat ditulis).

## Ringkas
- Paket baru `shared/tlsguard`: denylist 8 SPKI SHA-256 disalin dari Go CL 851047 (commit 7f86f957633e, identik dengan tag go1.27.3 2aae1000ea1d dan go1.26.10), `VerifyConnection` dengan semantik patch Go, `HardenTLSConfig`/`HardenTransport` (chain VerifyConnection lama), `InstallDefaultTransport` (Clone + idempoten), self-check startup.
- `shared/tlsguard/startup` di-blank-import oleh 29 service (`<svc>/tlsguard.go`).
- 5 transport sendiri di-harden: openai_embedder, business/import, redisurl, mediastorage/s3, sentry.
- `TLSGUARD_MODE=off|log|enforce`, default `log`. `enforce` butuh izin Viko terpisah setelah ±24 jam log bersih.
- Detail & daftar yang tidak di-hook: [docs/TLS_GUARD.md](../docs/TLS_GUARD.md).

## Verifikasi
- Hash: diff otomatis vs `go.googlesource.com/go/+/refs/tags/go1.27.3/src/crypto/x509/verify.go`, vs isi file CL 851047 current revision, vs `/usr/local/go` (go1.27.3) → identik. 6 hash TLS juga dihitung ulang dari 14 chain `chains.zip` (test `TestFixtureIntermediatesAreDenylisted`).
- `encore test ./...` (salinan unlinked) hijau; `go vet` bersih; staticcheck/golangci-lint/govulncheck tanpa temuan baru vs master; go directive tetap `1.26.3`.
- Probe handshake nyata mode enforce: lihat PR body (`/workspace/wabantu/audit/pr-body-tls-emudhra-guard.md`).
