# Pipeline LLM-first (Pelayan Toko)

Panduan **runtime & operasi** jalur balasan AI WhatsApp yang memakai satu panggilan LLM per giliran, dengan kode yang memegang bahasa, keranjang, total harga, dan handoff. Jalur ini ada di belakang flag per tenant dan **default mati**: tenant tanpa flag berperilaku persis seperti sebelumnya.

> **Desain & keputusan:** [spec](./superpowers/specs/2026-10-17-pelayan-toko-llm-first-design.md) · [rencana Part E](./superpowers/plans/2026-10-17-pelayan-toko-E-llm-first-orchestrator.md)  
> **Catatan rilis:** [20261017_pelayan-toko-llm-first.md](../docs-development-shipped/20261017_pelayan-toko-llm-first.md)  
> **Routing WhatsApp → AI (jalur lama, tetap jadi fallback):** [WHATSAPP_AI_ROUTING.md](./WHATSAPP_AI_ROUTING.md)

---

## 1. Cara kerja satu giliran

Hook `tryLLMFirst` ada di `ProcessAutoReply` (`ai/autoreply.go`) tepat setelah gerbang `IsThirdPartyBuyerLookup`, **sebelum** interceptor lama (status pesanan, bahasa, sapaan, alur order, classifier). Gerbang yang tetap di depan: dedupe, AI mati, media/bukti transfer, profil belum lengkap, auto-resume pause.

`flag.LLMFirstMode(ctx, tenantID)` memilih mode:

| Mode | Yang terjadi |
|------|--------------|
| `off` | Hanya pengecekan flag (miss-nya di-cache 60 detik). Tidak ada query atau tulisan lain; jalur lama jalan apa adanya. |
| `shadow` | `planTurn` jalan di goroutine (`context.WithoutCancel`, timeout 20 detik, panic di-recover) pada salinan data giliran, lalu **satu baris log** `llm_first shadow`. Tidak mengunci bahasa, tidak menulis state Redis, tidak mengirim, tidak pause, tidak notifikasi, dan tidak mencatat event `ai_token` (kuota tenant tidak terpakai). Jalur lama membalas seperti biasa. |
| `on` | Pipeline ini yang menjawab. Urutan di bawah. |

Mode `on`, berurutan (`ai/llm_first.go`):

1. **Cost gate** `usage.CheckAICostLimit`. Habis: balasan `TplQuotaExhausted` (path `cost_limit`).
2. **Muat data (hanya baca):** riwayat 12 pesan, pesan keluar terakhir, katalog (loader + stok + harga tier yang sama dengan jalur lama), FAQ (50 baris), state keranjang Redis, pesanan terbuka dari DB, status pengingat dan penghitung salah paham di Redis.
3. **Bahasa:** `salesbrain.DecideLanguage` satu-satunya otoritas (`language_switch` dari model tidak pernah dipakai). Bahasa yang dikunci ditulis ke `contact.preferred_language` dan dicerminkan ke `contact.PreferredLanguage` di memori serta `SetSavedBuyerLang`, supaya `applyBuyerLanguageFromMessage` (defer di `ProcessAutoReply`) dan template lama tidak menimpanya bila giliran ini fail-open. Sapaan ambigu ("hi") dijawab satu bubble dua bahasa tanpa memanggil model.
4. **`planTurn`** (`ai/llm_first_plan.go`): prompt dari `salesbrain.BuildPrompt`, maksimal dua panggilan JSON (yang kedua membawa catatan pelanggaran), validasi balasan, operasi keranjang, dan ringkasan/total dirender kode.
5. **Efek samping** (`ai/llm_first_effects.go`) lalu **kirim** (`sendAiMessage`, path `llm_first`).

### Efek samping per intent

| Intent | Yang dilakukan kode |
|--------|---------------------|
| `order`, `amend` | `setOrderState(stateWithCart(...))` bila keranjang berubah; `clearOrderState` bila kosong. Tawaran produk lain menandai `UpsellShown`. Balasan = teks model + ringkasan + kalimat lanjut. **Sesudah kirim**, keranjang yang berubah dan tidak kosong disimpan sebagai draft awal lewat `persistDraftOrderEarly` (fungsi yang sama dengan jalur lama); id draft dipasang di `PersistedOrderID` supaya amend dan `confirm` berikutnya memperbarui baris yang sama. Keranjang tanpa draft sendiri selalu draft baru (`ForceNewOrder`), jadi tidak pernah menyatu ke draft yang diparkir. Gagal simpan: dicatat di log dan owner diberi tahu, balasan tidak terhalang. |
| `field_answer` | `applyFieldAnswer` (nilai harus muncul di pesan pembeli) lalu `setOrderState`. Data tidak valid disebut persis (template `TplFieldProblems`) dan kolomnya ditanya lagi. Setelah lengkap: ringkasan penuh (item, alamat, minta OK). |
| `confirm` | Hanya di langkah `confirm` dengan alamat lengkap: `persistDraftOrder` (fungsi yang sama dengan checkout lama). Tanpa draft yang terikat = draft baru. Balasan + `TplOrderRef`. Gagal simpan: `TplSystemBusy` + notifikasi owner, keranjang tetap. |
| `cancel` | Draft belum bayar: `cancelPersistedOrder` + `clearOrderState`, balasan template `TplOrderCancelled`. Pesanan yang sudah dibayar **tidak pernah** dibatalkan bot: notifikasi owner dulu, lalu `TplCancelPaidOrder`. Nomor `WB-…` di pesan menentukan pesanan; nomor tidak dikenal tidak membatalkan apa pun. Dengan dua pesanan terbuka atau lebih dan tanpa nomor, tidak ada yang dibatalkan (pembeli yang memilih; balasan model tetap dikirim). Keranjang yang belum tersimpan cukup dikosongkan. |
| `continue_order` | Draft terbuka terbaru (atau yang disebut nomornya) dipulihkan ke Redis dari DB; hanya kolom yang kurang yang ditanya. Pesanan yang menunggu bayar tidak dipulihkan (tidak ada yang tersisa untuk dikumpulkan). |
| `new_order` | `clearOrderState`; draft lama **tetap** di DB (diparkir, tetap muncul di pengingat) dan keranjang berikutnya menjadi draft baru. `TplNewOrderParked` bila model belum menyebut nomornya. |
| `status` | `handleCustomerOrderStatus` (jalur lama; membalas sendiri). Semua teks status dua bahasa; bahasa dipilih `ResolveBuyerLanguage(pesan, bahasa terkunci kontak)`, jadi pembeli Inggris menerima status Inggris. |
| `human_request`, atau `handoff.level = takeover` | Setelah balasan terkirim: `pauseAI(HandoffReasonHumanRequest)` lalu `notifyOwner("ai.handoff")`. Balasan memuat jam buka tenant. AI lanjut otomatis setelah 2 jam tanpa balasan staf. |
| `handoff.level = note` | `notifyOwner` saja; AI tetap melayani. |

Status pengingat pesanan terbuka (TTL 72 jam) dan penghitung salah paham beruntun (TTL 24 jam) ditulis ke Redis **sesudah** balasan terkirim.

### Fail-open

Kegagalan sebelum tulisan pertama mengembalikan `(false, nil)` dan jalur lama menangani giliran itu: gagal muat data, gagal menulis bahasa, error LLM, JSON/validasi rusak dua kali, teks yang mirip injeksi prompt (penjaganya ada di belakang hook). Pemakaian token tetap dicatat (`ai_token` = input + output + thinking) walau fallback. **Error pipeline tidak pernah memicu pause.**

---

## 2. Flag dan cara mengaturnya

Semua flag default mati dan baru ada setelah dibuat. Flag yang belum dibuat dibaca sebagai mati (dan miss itu di-cache 60 detik).

| Key | Cakupan | Efek |
|-----|---------|------|
| `ai_llm_first_kill` | `enabledGlobally=true` atau `tenantIds` | Mode `off`. Mengalahkan dua flag lain. |
| `ai_llm_first` | `tenantIds` (atau global) | Mode `on`. |
| `ai_llm_first_shadow` | `tenantIds` (atau global) | Mode `shadow`, hanya bila `ai_llm_first` tidak aktif untuk tenant itu. |

Urutan cek di `flag.LLMFirstMode` (`flag/llm_first.go`): kill, lalu `ai_llm_first`, lalu `ai_llm_first_shadow`, selain itu `off`.

Endpoint ada di `flag/flag.go`, **hanya token super admin** ([ADMIN_AUTH.md](./ADMIN_AUTH.md)). Perubahan langsung berlaku di instance yang menerima request dan paling lambat **60 detik** di instance lain (TTL cache flag).

```bash
API="<API_BASE_URL>"
# Token admin: POST /api/v1/platform-admin/login (berlaku 60 menit). Jangan simpan di repo.
TOKEN="<ADMIN_ACCESS_TOKEN>"
AUTH=(-H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json")

# Login admin (isi email dan password dari secret manager, jangan ditulis di riwayat shell bersama)
curl -sS -X POST "$API/api/v1/platform-admin/login" -H "Content-Type: application/json" \
  -d '{"email":"<ADMIN_EMAIL>","password":"<ADMIN_PASSWORD>"}'
# -> {"accessToken":"<ADMIN_ACCESS_TOKEN>","expiresAt":"..."}

# 1) Buat ketiga flag, semuanya mati
for KEY in ai_llm_first ai_llm_first_shadow ai_llm_first_kill; do
  curl -sS -X POST "$API/api/v1/flags" "${AUTH[@]}" \
    -d "{\"key\":\"$KEY\",\"enabledGlobally\":false,\"tenantIds\":[],\"description\":\"Pelayan Toko LLM-first\"}"
done

# 2) Shadow untuk satu tenant. tenantIds MENGGANTI seluruh daftar: kirim semua tenant yang harus aktif.
curl -sS -X PATCH "$API/api/v1/flags/ai_llm_first_shadow" "${AUTH[@]}" \
  -d '{"tenantIds":["<TENANT_ID>"]}'

# 3) Hidupkan untuk tenant itu (mode "on" mengalahkan "shadow")
curl -sS -X PATCH "$API/api/v1/flags/ai_llm_first" "${AUTH[@]}" \
  -d '{"tenantIds":["<TENANT_ID>"]}'

# Lihat semua flag
curl -sS "$API/api/v1/flags" "${AUTH[@]}"
```

`<TENANT_ID>` adalah id tenant (UUID), sama dengan `payload.TenantID` di log AI.

---

## 3. Rollout: shadow, canary, lalu bertahap

**Prasyarat**

- Chain fungsi `chat` di AI Provider disetel superadmin: `gemini-3.1-flash-lite`, lalu `gemini-3.6-flash`, Anthropic hanya fallback. `PUT /api/v1/admin/ai/function-config/chat` ([API_AI_CONFIG.md](./API_AI_CONFIG.md)). Chain `chat` dipakai **juga** oleh jalur lama, jadi perubahan ini berlaku untuk keduanya.
- Data tenant pilot sudah dirapikan (spec §9): deskripsi profil, `base_pricing`, KB tanpa entri ganda atau tanpa nomor rekening. Model hanya sebaik data yang dibacanya.
- Flag sudah dibuat (mati) dan kill switch diuji sekali di staging.

**Langkah**

1. **Shadow (1 sampai 3 hari, satu tenant pilot).** Aktifkan `ai_llm_first_shadow`. Pembeli tidak melihat apa pun. Baca log `llm_first shadow` (field: `intent`, `lang`, `fallback`, `why`, `attempts`, `violations`, `cartChanged`, `handoff`, `replyPreview`, `inTok`, `outTok`, `thoughtTok`) dan biaya lewat query §5. Lanjut bila fallback `invalid_output` rendah, intent masuk akal, dan biaya per pesan sesuai §6. Angka batas fallback belum ditetapkan di spec; tetapkan bersama sebelum canary.
2. **Canary (48 jam, tenant yang sama).** Aktifkan `ai_llm_first`. Pantau query §5 tiap hari dan baca sampel 50 balasan (bahasa, fakta, harga). Guardrail spec D9: bahasa salah ≤ 1%, diam tanpa batas = 0, penggantian balasan ≤ 2%, biaya ≤ Rp20 per pesan. Cek notifikasi owner (`ai.handoff`, `ai.system_error`) tidak banjir.
3. **Bertahap.** Tambahkan tenant satu per satu ke `tenantIds`. **Kuota tidak diubah di pekerjaan ini** (D2): LLM-first memakai sekitar 5 kali token per pesan, jadi tenant Trial (±32 pesan per bulan) dan Starter lebih cepat habis. Aktifkan global (`enabledGlobally=true`) hanya setelah canary bersih dan keputusan kuota diambil.

---

## 4. Kill switch dan rollback

**Kill switch (global, dalam 60 detik):**

```bash
curl -sS -X PATCH "$API/api/v1/flags/ai_llm_first_kill" "${AUTH[@]}" -d '{"enabledGlobally":true}'
# flag belum ada? buat dengan enabledGlobally=true:
curl -sS -X POST "$API/api/v1/flags" "${AUTH[@]}" \
  -d '{"key":"ai_llm_first_kill","enabledGlobally":true,"tenantIds":[],"description":"Kill switch LLM-first"}'
```

**Matikan satu tenant:** keluarkan dari `tenantIds` flag `ai_llm_first` (dan shadow), atau masukkan ke `tenantIds` flag kill (kill per tenant). Balik: kill `enabledGlobally=false`.

**Yang tersisa setelah dimatikan (aman, tidak perlu dibersihkan):**

- Kunci bahasa di `contact.preferred_language` tetap (memang tujuannya).
- Pesanan yang dikonfirmasi lewat pipeline adalah draft biasa yang sama dengan checkout lama.
- Keranjang Redis (`ai:order:<tenantId>:<conversationId>`) yang sedang di langkah `confirm`: jalur lama tidak mengenal langkah itu dan menjawab teks "lanjutkan data order" sampai state kedaluwarsa (2 jam) atau pembeli memulai pesanan baru. Hapus kunci itu bila perlu.
- Kunci `ai:openorder:remind:*` (72 jam) dan `ai:llmfirst:confused:*` (24 jam) tidak dibaca jalur lama.

**Rollback kode:** revert merge-nya. Flag di DB tidak berbahaya karena tidak ada yang membacanya. Tanpa migrasi skema, tanpa secret atau env baru, tanpa endpoint baru.

---

## 5. Metadata per balasan dan SQL pengukuran

Setiap balasan pipeline menyimpan ringkasan keputusan di `message.metadata` (JSONB), field lama `path`, `reason`, `llmUsed` tetap diisi:

| Field | Isi |
|-------|-----|
| `pipeline` | `llm_first` |
| `path` | `llm_first` (balasan template bahasa: `buyer_language`, kuota habis: `cost_limit`) |
| `intent` | Klasifikasi model: `question`, `order`, `confirm`, `cancel`, ... |
| `lang` | Bahasa balasan (`id` atau `en`) |
| `attempts` | 1, atau 2 setelah retry dengan catatan pelanggaran |
| `model` | Model yang benar-benar dipakai chain (bisa fallback) |
| `rawOutput` | Output mentah model terakhir: diredaksi (nomor panjang menjadi `[NUM]`, email `[EMAIL]`), maksimal 1000 karakter |
| `replacedReason` | Template menggantikan teks model: `cancel_template`, `cancel_paid_template`, `order_persist_failed` |
| `orderId`, `orderAction` | `create`, `cancel`, atau `cancel_request` (pesanan sudah bayar diteruskan ke owner) |

Intent `status` dibalas jalur lama (`path = order_status`), jadi tidak membawa metadata pipeline. Giliran yang fail-open dibalas jalur lama dan hanya tampak di log `llm_first fallback` (`why`: `llm_error` atau `invalid_output`).

Ganti `<tenant_schema>` dengan schema tenant (mis. dari `payload.TenantSchema`). Query hanya membaca.

Volume per pipeline dan path (7 hari):

```sql
SELECT COALESCE(metadata->>'pipeline', 'legacy') AS pipeline,
       metadata->>'path'                         AS path,
       count(*)                                  AS replies
FROM "<tenant_schema>".message
WHERE direction = 'out' AND author IN ('ai', 'system')
  AND created_at >= now() - interval '7 days'
GROUP BY 1, 2
ORDER BY replies DESC;
```

Intent dan bahasa:

```sql
SELECT metadata->>'intent' AS intent, metadata->>'lang' AS lang, count(*) AS replies
FROM "<tenant_schema>".message
WHERE direction = 'out' AND metadata->>'pipeline' = 'llm_first'
  AND created_at >= now() - interval '7 days'
GROUP BY 1, 2
ORDER BY replies DESC;
```

Retry dan penggantian teks model:

```sql
SELECT count(*) AS replies,
       round(100.0 * count(*) FILTER (WHERE metadata->>'replacedReason' IS NOT NULL) / NULLIF(count(*), 0), 1) AS replaced_pct,
       round(100.0 * count(*) FILTER (WHERE (metadata->>'attempts')::int > 1) / NULLIF(count(*), 0), 1)        AS retried_pct,
       count(*) FILTER (WHERE metadata->>'replacedReason' = 'order_persist_failed')                            AS failed_order_saves
FROM "<tenant_schema>".message
WHERE direction = 'out' AND metadata->>'pipeline' = 'llm_first'
  AND created_at >= now() - interval '7 days';
```

Pesanan yang dibuat atau dibatalkan lewat pipeline:

```sql
SELECT metadata->>'orderAction' AS action, count(*) AS replies
FROM "<tenant_schema>".message
WHERE direction = 'out' AND metadata->>'pipeline' = 'llm_first'
  AND metadata->>'orderAction' IS NOT NULL
  AND created_at >= now() - interval '7 days'
GROUP BY 1;
```

Sampel 50 balasan untuk dibaca manusia (bahasa dan fakta). Berisi isi chat pembeli: jangan disalin keluar tim.

```sql
SELECT m.created_at,
       m.metadata->>'intent' AS intent,
       m.metadata->>'lang'   AS lang,
       (SELECT i.body FROM "<tenant_schema>".message i
         WHERE i.conversation_id = m.conversation_id AND i.direction = 'in' AND i.created_at < m.created_at
         ORDER BY i.created_at DESC LIMIT 1) AS buyer_message,
       m.body AS reply
FROM "<tenant_schema>".message m
WHERE m.direction = 'out' AND m.metadata->>'pipeline' = 'llm_first'
  AND m.created_at >= now() - interval '2 days'
ORDER BY random()
LIMIT 50;
```

Token dan biaya per balasan (aktivitas AI; `outputTokens` sudah termasuk thinking). Harga per 1 juta token dan kurs ada di `usage/costmonitor.go` dan [UNIT_ECONOMICS_AND_PRICING.md](./UNIT_ECONOMICS_AND_PRICING.md); contoh ini memakai `gemini-3.1-flash-lite` (US$0,25 masuk, US$1,50 keluar) dan Rp16.500 per US$:

```sql
SELECT metadata->>'model' AS model,
       count(*)           AS replies,
       round(avg((metadata->>'inputTokens')::numeric))  AS avg_in_tokens,
       round(avg((metadata->>'outputTokens')::numeric)) AS avg_out_tokens,
       round(avg((metadata->>'inputTokens')::numeric * 0.25 + (metadata->>'outputTokens')::numeric * 1.50)
             / 1000000 * 16500, 1)                      AS avg_idr_per_reply
FROM "<tenant_schema>".usage_event
WHERE event_type = 'ai_activity'
  AND metadata->>'path' = 'llm_first' AND metadata->>'purpose' = 'inbound_autoreply'
  AND created_at >= now() - interval '7 days'
GROUP BY 1;
```

Biaya shadow per hari (purpose `llm_first_shadow`; tidak memakan kuota `ai_token`):

```sql
SELECT date_trunc('day', created_at) AS day,
       count(*)                                          AS runs,
       sum((metadata->>'inputTokens')::bigint)           AS in_tokens,
       sum((metadata->>'outputTokens')::bigint)          AS out_tokens
FROM "<tenant_schema>".usage_event
WHERE event_type = 'ai_activity' AND metadata->>'purpose' = 'llm_first_shadow'
  AND created_at >= now() - interval '7 days'
GROUP BY 1
ORDER BY 1 DESC;
```

Percakapan yang sedang di-pause AI (handoff takeover) dan sudah berapa lama:

```sql
SELECT id, handoff_reason, ai_paused_at, now() - ai_paused_at AS paused_for
FROM "<tenant_schema>".conversation
WHERE ai_handled = false AND ai_paused_at IS NOT NULL
ORDER BY ai_paused_at;
```

Pause yang dibuat AI sendiri otomatis dilepas pada giliran berikutnya setelah 2 jam tanpa balasan staf, jadi baris lama di sini normal sampai pembeli menulis lagi.

---

## 6. Estimasi biaya per pesan

Satu giliran = satu panggilan LLM (dua bila retry karena JSON rusak atau pelanggaran validasi). Rumus:

```
biaya_USD = input_token / 1.000.000 x harga_input + (output + thinking) / 1.000.000 x harga_output
biaya_IDR = biaya_USD x 16.500        (kurs asumsi repo, sesuaikan bulanan)
```

Ukuran prompt, diukur dari fixture tenant Omah (5 produk dan 10 baris FAQ, taksiran 3,5 karakter per token): prompt sistem (persona, profil, katalog, FAQ) ± 2.270 token, ditambah blok state, riwayat (maksimal 6 giliran), dan pesan ± 100 sampai 600 token. Mode `full` dipakai selama katalog + FAQ ≤ 5.000 token; lebih besar dari itu katalog dipangkas ke 12 produk terbaik (ditambah isi keranjang) dan FAQ memakai hasil retrieval.

Harga `gemini-3.1-flash-lite`: US$0,25 masuk dan US$1,50 keluar per 1 juta token (`usage/costmonitor.go`).

| Skenario | Masuk | Keluar (+thinking) | Biaya per pesan |
|----------|-------|--------------------|-----------------|
| Tenant kecil seperti Omah | 2.700 | 250 | US$0,00105 = **± Rp17** |
| Sama, dengan satu retry | 5.400 | 500 | ± Rp35 |
| Batas atas mode `full` (± 7.000 masuk) | 7.000 | 400 | US$0,00235 = ± Rp39 |

Target spec (≤ Rp20 per pesan, ≤ Rp250 per percakapan untuk tenant kecil) tercapai untuk tenant kecil; tenant dengan katalog dan FAQ mendekati batas `full` berada di atasnya. Angka keluar/thinking di atas adalah asumsi, ukur dengan query §5 setelah shadow. Caching implisit Gemini tidak diperhitungkan.

Peringatan harga:

- Bila chain jatuh ke Anthropic tanpa model eksplisit, model default adalah Sonnet (`defaultAnthropicModel`; US$3 masuk, US$15 keluar). Skenario pertama menjadi sekitar Rp195 per pesan, 11 kali lebih mahal. Setel chain `chat` ke model yang disengaja.
- `gemini-3.6-flash` belum ada di tabel harga `usage/costmonitor.go`; dashboard biaya menghitungnya dengan harga Sonnet sampai ditambahkan.

---

## 7. Catatan environment

- Tidak ada secret, env, migrasi skema, atau endpoint baru.
- `GeminiAPIKey` hanya ada di Cloud Run, jadi Gemini hanya jalan di sana. Di Encore Cloud chain jatuh ke Anthropic: mode JSON digantikan instruksi di prompt dan parse yang toleran; bila parse gagal dua kali, giliran dikembalikan ke jalur lama (fail-open).
- Batas waktu: 15 detik per panggilan LLM, dihitung untuk seluruh link chain (timeout per link berlaku di dalamnya), maksimal 2 panggilan per giliran, anggaran job 40 detik. Error LLM tidak di-retry (langsung fallback), jadi sisa anggaran cukup untuk jalur lama.
- `INLINE_INBOUND_AI` dan `RETRIEVAL_BACKGROUND_DRAIN` tetap hanya di Cloud Run.

---

## 8. Batasan saat ini

Perilaku yang sengaja atau belum ditangani, supaya tidak mengejutkan saat canary:

- **Produk bervarian (ukuran/warna)**, misalnya nama berisi celana, kaos, atau ukuran: pipeline tidak punya kolom varian, jadi menyimpan pesanannya gagal (`ReadyToPersist`) dan pembeli mendapat pesan gangguan sistem plus notifikasi owner. Tenant pilot (steak) tidak terdampak.
- **Fail-open di langkah `confirm`:** jalur lama tidak mengenal langkah itu dan membalas teks "lanjutkan data order" sampai pembeli mengulang OK atau state kedaluwarsa.
- **Kuota habis** dijawab template tanpa notifikasi owner (sama dengan jalur lama); spec §4.4 menyebut notifikasi untuk kasus ini.
- Penghitungan sesi `ai_conversation`: bila giliran fallback lalu jalur lama memanggil LLM di sesi baru, event percakapan untuk sesi itu tidak tercatat (menguntungkan tenant, bukan menagih lebih).

---

## 9. Peta kode

| File | Isi |
|------|-----|
| `ai/llm_first.go` | Hook `tryLLMFirst`, mode `on` (`runLLMFirst`), shadow, muat data, panggilan LLM, efek langsung (Redis, DB) |
| `ai/llm_first_plan.go` | `planTurn`: keputusan murni (prompt, retry, validasi, ringkasan, kalimat lanjut) |
| `ai/llm_first_effects.go` | `lfApplyIntent`: efek per intent lewat antarmuka `lfFX` |
| `ai/llm_first_orders.go` | Adaptor keranjang dan data penerima (`cartFromState`, `stateWithCart`, `applyFieldAnswer`) |
| `ai/llm_first_turn.go` | Helper murni: waktu, riwayat, pesanan terbuka, katalog top-k, input prompt |
| `ai/reply_meta.go` | `AiReplyMeta` dengan field pipeline |
| `internal/salesbrain/` | Bahasa, pesanan terbuka dan pengingat, prompt, parser keputusan, validator, template ID/EN, ringkasan dan redaksi |
| `flag/llm_first.go` | Key flag dan `LLMFirstMode` |
| `shared/llm/` | `RunChat` lewat chain fungsi, mode JSON, temperature, usage termasuk thinking |

## 10. Menjalankan tes

```bash
go test ./internal/salesbrain/ -count=1
encore test ./ai/ -run 'LLMFirst|Plan|FieldAnswer|CartFromState' -count=1
encore test ./ai/ -count=1          # seluruh paket ai
encore test ./flag/ -count=1
encore test ./internal/apitest/ -count=1
```

Tes giliran utuh (`ai/llm_first_flow_test.go`) memakai Redis palsu, model palsu, dan pengirim simulator; di sana terbukti shadow tidak menulis apa pun dan urutan kirim, pause, notifikasi terjaga. Penjaga AST (`ai/llm_first_shell_test.go`) menggagalkan tes bila hook bergeser dari tempatnya atau jalur shadow memanggil fungsi yang menulis.
