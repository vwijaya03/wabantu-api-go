# Inbound coalesce — burst balloon WhatsApp

**Status:** PR [#208](https://github.com/vwijaya03/wabantu-api-go/pull/208) (belum merge)

## Masalah / Kebutuhan

Meta Cloud API satu HTTP per balloon. `PublishInboundJob` per inbound → banyak balasan untuk satu niat. Bukti Omah `b72e2bee-91a6-475d-b389-27bac4e297ad` 19 Sep 2026 23:18 WIB: `halo` / `saya mau beli` / `magi` / `bisa ga ?` / `putus putus` → 5 outbound (greeting + LLM + catalog_db + 2 LLM).

## Perubahan

- Persist balloon tetap di webhook (idempotent `external_id`).
- Subscriber `ai-auto-reply`: trailing-edge debounce 3s, max wait 15s, cap 20 id, stitch `\n`, satu `ProcessAutoReply`.
- Job lama supersede; `sendAiMessage` drop jika inbound lebih baru, lalu re-arm.
- `IsChannelRepairMeta` → diam (path `channel_meta`).
- Image / bukti transfer tidak di-debounce. Redis down = proses langsung.

## File utama

- `ai/inbound_coalesce.go`
- `ai/inbound_jobs.go`
- `ai/autoreply.go`
- `internal/buyerflow/channel_repair.go`
- `docs/WHATSAPP_AI_ROUTING.md`

## Testing

- `go test ./ai/ -count=1 -run 'TestStitch|TestAppendBurst|TestRemainingWait|TestJobSuperseded|TestOmahBurst_|TestIDsAfter|TestEffectiveInbound|TestShouldCoalesce'`
- `go test ./internal/buyerflow/ -count=1 -run 'TestIsChannelRepairMeta|TestIsGreetingLike_stitched|TestOmahBurst_'`

## Catatan deploy

Tidak ada endpoint baru / apiregistry. Butuh Redis (sudah ada). Setelah merge, burst 3s di staging.
