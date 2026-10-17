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
| `shadow` | `planTurn` jalan di goroutine (`context.WithoutCancel`, timeout 20 detik, panic di-recover) pada salinan data giliran, lalu **satu baris log** `llm_first shadow`. Tidak mengunci bahasa, tidak menulis state Redis, tidak mengirim, tidak pause, tidak notifikasi, dan tidak mencatat event `ai_token` (kuota tenant tidak terpakai). Yang dicatat hanya aktivitas AI (purpose `llm_first_shadow`) dan event pengukuran `ai_token_llm_first` (pipeline `llm_first_shadow`, lihat §5.1) supaya biaya per pesan terukur selama shadow. Jalur lama membalas seperti biasa. |
| `on` | Pipeline ini yang menjawab. Urutan di bawah. |

Mode `on`, berurutan (`ai/llm_first.go`):

1. **Cost gate** `usage.CheckAICostLimit`. Habis: balasan `TplQuotaExhausted` (path `cost_limit`).
2. **Muat data (hanya baca):** riwayat 12 pesan, pesan keluar terakhir, katalog (loader + stok + harga tier yang sama dengan jalur lama), FAQ (50 baris), state keranjang Redis, pesanan terbuka dari DB, status pengingat dan penghitung salah paham di Redis.
3. **Bahasa:** `salesbrain.DecideLanguage` satu-satunya otoritas (`language_switch` dari model tidak pernah dipakai). Bahasa yang dikunci ditulis ke `contact.preferred_language` dan dicerminkan ke `contact.PreferredLanguage` di memori serta `SetSavedBuyerLang`, supaya `applyBuyerLanguageFromMessage` (defer di `ProcessAutoReply`) dan template lama tidak menimpanya bila giliran ini fail-open. Sapaan ambigu ("hi") dijawab satu bubble dua bahasa tanpa memanggil model.
4. **`planTurn`** (`ai/llm_first_plan.go`): prompt dari `salesbrain.BuildPrompt`, maksimal dua panggilan JSON (yang kedua membawa catatan pelanggaran), validasi balasan, operasi keranjang, dan ringkasan/total dirender kode.
5. **Efek samping** (`ai/llm_first_effects.go`) lalu **kirim** (`sendAiMessage`, path `llm_first`).

Pemakaian token tiap giliran dicatat dua kali di mode `on`: `ai_token` (kuota plan, tidak berubah) dan `ai_token_llm_first` (khusus pengukuran, tanpa batas kuota). Mode `shadow` hanya mencatat `ai_token_llm_first`.

### Efek samping per intent

| Intent | Yang dilakukan kode |
|--------|---------------------|
| `order`, `amend` | `setOrderState(stateWithCart(...))` bila keranjang berubah; `clearOrderState` bila kosong. Tawaran produk lain menandai `UpsellShown`. Balasan = teks model + ringkasan + kalimat lanjut. **Sesudah kirim**, keranjang yang berubah dan tidak kosong disimpan sebagai draft awal lewat `persistDraftOrderEarly` (fungsi yang sama dengan jalur lama); id draft dipasang di `PersistedOrderID` supaya amend dan `confirm` berikutnya memperbarui baris yang sama. Keranjang tanpa draft sendiri selalu draft baru (`ForceNewOrder`), jadi tidak pernah menyatu ke draft yang diparkir. Gagal simpan: dicatat di log dan owner diberi tahu, balasan tidak terhalang. Varian, stok, dan harga: lihat §1.1. |
| `field_answer` | `applyFieldAnswer` (nilai harus muncul di pesan pembeli) lalu `setOrderState`. Data tidak valid disebut persis (template `TplFieldProblems`) dan kolomnya ditanya lagi. Setelah lengkap: ringkasan penuh (item, alamat, minta OK). |
| `confirm` | Hanya di langkah `confirm` dengan alamat lengkap: `persistDraftOrder` (fungsi yang sama dengan checkout lama). Tanpa draft yang terikat = draft baru. Balasan + `TplOrderRef`. Gagal simpan: `TplSystemBusy` + notifikasi owner, keranjang dan draft yang terikat tetap. Bila masih ada baris yang belum punya ukuran/warna, tidak ada yang disimpan dan tidak ada notifikasi: langkah kembali ke `ask_variant` dan pembeli ditanya (`replacedReason = variant_missing`). |
| `cancel` | Draft belum bayar: `cancelPersistedOrder` + `clearOrderState`, balasan template `TplOrderCancelled`. Pesanan yang sudah dibayar **tidak pernah** dibatalkan bot: notifikasi owner dulu, lalu `TplCancelPaidOrder`. Nomor `WB-…` di pesan menentukan pesanan; nomor tidak dikenal tidak membatalkan apa pun. Dengan dua pesanan terbuka atau lebih dan tanpa nomor, tidak ada yang dibatalkan (pembeli yang memilih; balasan model tetap dikirim). Keranjang yang belum tersimpan cukup dikosongkan. |
| `continue_order` | Draft terbuka terbaru (atau yang disebut nomornya) dipulihkan ke Redis dari DB; hanya kolom yang kurang yang ditanya. Pesanan yang menunggu bayar tidak dipulihkan (tidak ada yang tersisa untuk dikumpulkan). |
| `new_order` | `clearOrderState`; draft lama **tetap** di DB (diparkir, tetap muncul di pengingat) dan keranjang berikutnya menjadi draft baru. `TplNewOrderParked` bila model belum menyebut nomornya. |
| `status` | `handleCustomerOrderStatus` (jalur lama; membalas sendiri). Semua teks status dua bahasa; bahasa dipilih `ResolveBuyerLanguage(pesan, bahasa terkunci kontak)`, jadi pembeli Inggris menerima status Inggris. |
| `human_request`, atau `handoff.level = takeover` | Setelah balasan terkirim: `pauseAI(HandoffReasonHumanRequest)` lalu `notifyOwner("ai.handoff")`. Balasan memuat jam buka tenant. AI lanjut otomatis setelah 2 jam tanpa balasan staf. |
| `handoff.level = note` | `notifyOwner` saja; AI tetap melayani. |

Status pengingat pesanan terbuka (TTL 72 jam) dan penghitung salah paham beruntun (TTL 24 jam) ditulis ke Redis **sesudah** balasan terkirim.

### 1.1 Varian, stok, dan harga

Katalog menyimpan varian sebagai baris terpisah per ukuran ("Boxer Pria Mono - L", "... - M"), plus produk yang butuh ukuran/warna tanpa ukuran di namanya (kaos, celana). Aturan yang sama dengan jalur lama (`buyerflow.CatalogItemNeedsVariant`, `ExtractSizeFromProductName`) menurunkan data varian per baris katalog (`lfCatalogFacts`): `Size`, `BaseName` (baris dengan `BaseName` sama adalah ukuran lain dari produk yang sama), `NeedsVariant`, dan stok dari `enrichCatalogStock`; harga dari `applyContactTierPrices`. Model tidak pernah mengisi data ini.

Model hanya mengusulkan `items: [{no, qty, size, color}]`. Kode (`salesbrain.ResolveCartOps`) yang memutuskan:

| Situasi | Perilaku |
|---------|----------|
| Satu produk, beberapa ukuran/warna | Satu baris per varian. Mengubah jumlah satu varian tidak menyentuh varian lain; jumlah 0 tanpa ukuran menghapus semua varian produk itu; jumlah baru tanpa ukuran ketika ada dua varian atau lebih ditanyakan balik (tidak ada yang berubah). |
| `size`/`color` dari model | Dipakai **hanya bila kata itu muncul utuh di pesan pembeli saat ini** (bukan riwayat alamat/nomor rumah), seperti `applyFieldAnswer`. Warna harus kata warna yang dikenal atau muncul di katalog produk itu; nilai yang tidak diketik pembeli diabaikan. |
| Stok per produk | Jumlah semua baris keranjang dengan `catalog_item_id` yang sama dibandingkan ke `StockQty` baris itu; melebihi → `TplStockLimit`, bukan gagal simpan diam-diam. |
| Ukuran yang disebut pembeli beda dengan baris yang dipilih model | Dipindah ke baris saudara dengan ukuran itu (harga baris itu). Ukuran yang tidak ada: balasan template `TplVariantNotFound` dengan pilihan yang ada. |
| Baris berukuran dengan saudara, tapi pembeli tidak menyebut ukuran | Tidak ditambahkan; template `TplAskVariantChoices` menyebut ukuran yang masih ada stoknya. |
| Produk yang butuh ukuran/warna, belum disebut | Baris tetap masuk keranjang dengan jumlahnya (`VariantMissing`), langkah menjadi `ask_variant`, pembeli ditanya **sekali** (`TplAskVariant`). Draft awal dan langkah penerima/alamat/konfirmasi menunggu sampai varian terisi; jawabannya mengisi baris yang sama (bukan baris baru) dan checkout lanjut dari data yang sudah terkumpul (alamat tidak ditanya ulang). |
| Stok habis | Tidak ditambahkan. `TplVariantSoldOut` jujur + ukuran saudara yang masih ada (`TplVariantAlternatives`). Berlaku juga untuk produk tanpa varian. |
| Jumlah melebihi stok | Tidak ditambahkan/dinaikkan. `TplStockLimit` menyebut sisa. Baris tanpa data stok (stok tidak dilacak) tidak ditolak di sini; penjaga stok saat simpan tetap berlaku. |

Bila ada penolakan atau pertanyaan varian, teks model **diganti** kalimat template (model bisa saja menulis "sudah masuk keranjang") ditambah ringkasan keranjang bila berubah dan kalimat lanjut (`replacedReason = cart_issue`). Ringkasan menulis varian per baris ("2x Kaos Polos (ukuran M, warna hitam)"). Ukuran yang sudah ada di nama produk tidak diulang. Semua kalimat ini template ID/EN.

### 1.2 Fail-open dan fail-closed

**Fail-open (default).** Kegagalan sebelum tulisan pertama mengembalikan `(false, nil)` dan jalur lama menangani giliran itu: gagal muat data, gagal menulis bahasa, error LLM, JSON/validasi rusak dua kali, teks yang mirip injeksi prompt (penjaganya ada di belakang hook). Pemakaian token tetap dicatat (`ai_token` dan `ai_token_llm_first`) walau fallback. **Error pipeline tidak pernah memicu pause.**

**Fail-closed di langkah `confirm`.** Langkah `confirm` berarti pembeli sedang melihat ringkasan akhir dan menunggu OK. Jalur lama tidak boleh menebak apakah balasan berikutnya persetujuan, jadi:

- Bila `planTurn` fallback (error LLM atau output rusak dua kali) **dan** state Redis di langkah `confirm` dengan alamat lengkap dan minimal satu baris: pipeline **tidak** mengembalikan `(false, nil)`. Draft dan state dibiarkan apa adanya, tidak ada yang disimpan, tidak ada pause. Pembeli menerima `TplConfirmHold` + ringkasan penuh (item, alamat, `TplResumeConfirm`) sebagai pesan `system` (`replacedReason = confirm_fail_closed`), dan owner mendapat notifikasi `ai.system_error` (dedupe satu per percakapan per jam).
- Jalur lama (`internal/buyerflow/order_fsm.go`) kini mengenal langkah `confirm` dan **tidak pernah menyelesaikan pesanan tanpa persetujuan eksplisit**: `normalizeOrderState` tidak lagi memetakan `confirm` ke `ask_address_full`. Di langkah itu pesanan hanya disimpan bila teks pembeli lolos `IsExplicitOrderConfirm` (hanya kata persetujuan: "ok", "ya benar kak", "lanjut proses", "looks good", 👍; kalimat dengan kata lain, pertanyaan, dan penolakan tidak lolos). Teks lain menjaga draft di `confirm` dan menampilkan ringkasan lagi dengan ajakan OK; data penerima/alamat yang kurang mengembalikan langkah ke `ask_recipient`/`ask_address_full`. Ini juga melindungi jalur fail-open lain (gagal muat data, gagal tulis bahasa, teks injeksi) yang jatuh ke jalur lama saat state di `confirm`.

---

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

- Chain fungsi `chat` di AI Provider disetel superadmin: `gemini-3.1-flash-lite`, lalu `gemini-3.6-flash`, Anthropic hanya fallback. `PUT /api/v1/admin/ai/function-config/chat` ([API_AI_CONFIG.md](./API_AI_CONFIG.md)). Chain `chat` dipakai **juga** oleh jalur lama, jadi perubahan ini berlaku untuk keduanya. Pipeline selalu memakai chain `chat` tenant (baris tenant, lalu baris global, lalu default kode lewat `LLMRegistry`), tidak pernah model yang dipasang di kode.
- **Jangan hidupkan `ai_llm_first` di Encore Cloud sebelum ada kunci Gemini.** Tanpa Gemini, chain jatuh ke Anthropic dan link tanpa model memakai **Haiku** (`DefaultHaikuAPIID`, ±US$1 masuk / US$5 keluar per 1 juta token), bukan Sonnet. Haiku tetap ±4 kali lebih mahal dari `gemini-3.1-flash-lite` dan JSON hanya lewat instruksi prompt (§7), jadi Encore tanpa Gemini cukup untuk shadow/uji, bukan untuk rollout.
- **Cloud Run (CR):** model chat tidak diatur lewat env atau file deploy. `deploy/cloudrun/` dan `scripts/cloudrun/deploy.sh` di branch `feat/pelayan-toko-llm-first-cr` hanya meneruskan `GeminiAPIKey`; tidak ada konfigurasi Neon/deploy yang menyebut model chat. Yang menentukan adalah baris `function-config` chat di database (migrasi 29 menyemai `gemini-3.1-flash-lite`). Sebelum canary di CR, setel baris itu **secara eksplisit** ke `gemini-3.1-flash-lite` lewat endpoint di atas dan cek hasilnya di `GET /api/v1/admin/ai/function-config`, jangan mengandalkan seed.
- Data tenant pilot sudah dirapikan (spec §9): deskripsi profil, `base_pricing`, KB tanpa entri ganda atau tanpa nomor rekening. Model hanya sebaik data yang dibacanya.
- Flag sudah dibuat (mati) dan kill switch diuji sekali di staging.

**Langkah**

1. **Shadow (1 sampai 3 hari, satu tenant pilot).** Aktifkan `ai_llm_first_shadow`. Pembeli tidak melihat apa pun. Baca log `llm_first shadow` (field: `intent`, `lang`, `fallback`, `why`, `attempts`, `violations`, `cartChanged`, `handoff`, `replyPreview`, `inTok`, `outTok`, `thoughtTok`) dan biaya lewat query §5. Lanjut bila fallback `invalid_output` rendah, intent masuk akal, dan biaya per pesan sesuai §6 (ukur dengan event `ai_token_llm_first`, §5.1). Angka batas fallback belum ditetapkan di spec; tetapkan bersama sebelum canary.
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
- Keranjang Redis (`ai:order:<tenantId>:<conversationId>`) yang sedang di langkah `confirm`: jalur lama menampilkan ringkasan lagi dan hanya menyimpan pesanan bila pembeli membalas persetujuan eksplisit (OK, ya benar, lanjut proses). State kedaluwarsa setelah 2 jam. Hapus kunci itu bila perlu.
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
| `replacedReason` | Template menggantikan teks model: `cancel_template`, `cancel_paid_template`, `order_persist_failed`, `cart_issue` (varian kurang, stok habis atau kurang), `variant_missing` (konfirmasi saat varian belum lengkap), `confirm_fail_closed` (giliran gagal di langkah konfirmasi, ringkasan dikirim ulang) |
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

Biaya shadow per hari (purpose `llm_first_shadow`; tidak memakan kuota `ai_token`; padanannya per giliran ada di §5.1):

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

### 5.1 Event `ai_token_llm_first`

Event usage terpisah untuk mengukur pipeline ini per pesan. Nilainya input + output + thinking token satu giliran; metadata: `pipeline` (`llm_first` atau `llm_first_shadow`), `conversationId`, `provider`, `model`, `intent`, `fallback`, `attempts`, `inputTokens`, `outputTokens`, `thoughtsTokens`. Tidak ada entri di `planQuotas` untuk event ini, jadi tidak membatasi apa pun dan kuota `ai_token` plan tidak berubah (mode `on` tetap mencatat `ai_token` seperti sebelumnya untuk kuota/billing; `shadow` hanya mencatat `ai_token_llm_first`). Event tidak muncul di ringkasan kuota karena ringkasan hanya menampilkan event yang punya kuota.

Biaya dan fallback per pipeline dan model (7 hari). Harga per 1 juta token dari `usage/costmonitor.go`; kolom `unpriced` menandai model yang belum punya harga (biaya tidak dihitung, tidak diganti harga model lain):

```sql
SELECT metadata->>'pipeline' AS pipeline, metadata->>'model' AS model,
       count(*)                                                      AS turns,
       count(*) FILTER (WHERE (metadata->>'fallback')::boolean)       AS fallbacks,
       round(avg(quantity))                                          AS avg_tokens_per_turn,
       sum((metadata->>'inputTokens')::bigint)                       AS in_tokens,
       sum((metadata->>'outputTokens')::bigint + (metadata->>'thoughtsTokens')::bigint) AS out_tokens
FROM "<tenant_schema>".usage_event
WHERE event_type = 'ai_token_llm_first' AND created_at >= now() - interval '7 days'
GROUP BY 1, 2
ORDER BY 1, 2;
```

Hitung rupiah dengan rumus §6 dari `in_tokens` dan `out_tokens`.

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

Harga Standard paid tier teks (`usage/costmonitor.go`, sumber [ai.google.dev/gemini-api/docs/pricing](https://ai.google.dev/gemini-api/docs/pricing), dibaca 2026-10-17; thinking dihitung sebagai output):

| Model | Masuk / keluar per 1 juta token |
|-------|---------------------------------|
| `gemini-3.1-flash-lite` | US$0,25 / US$1,50 |
| `gemini-3.5-flash-lite` | US$0,30 / US$2,50 |
| `gemini-3.6-flash`, `gemini-3.8-flash` | US$0,75 / US$3,75 sampai 31 Des 2026; US$1,50 / US$7,50 mulai 1 Jan 2027 (**ubah dua baris tabel itu pada 1 Jan 2027**) |
| `gemini-3.1-pro-preview` | US$2,00 / US$12,00 (prompt ≤ 200 ribu token; tier di atasnya tidak dimodelkan) |

Contoh di bawah memakai `gemini-3.1-flash-lite`:

| Skenario | Masuk | Keluar (+thinking) | Biaya per pesan |
|----------|-------|--------------------|-----------------|
| Tenant kecil seperti Omah | 2.700 | 250 | US$0,00105 = **± Rp17** |
| Sama, dengan satu retry | 5.400 | 500 | ± Rp35 |
| Batas atas mode `full` (± 7.000 masuk) | 7.000 | 400 | US$0,00235 = ± Rp39 |

Target spec (≤ Rp20 per pesan, ≤ Rp250 per percakapan untuk tenant kecil) tercapai untuk tenant kecil; tenant dengan katalog dan FAQ mendekati batas `full` berada di atasnya. Angka keluar/thinking di atas adalah asumsi, ukur dengan §5.1 setelah shadow. Caching implisit Gemini tidak diperhitungkan.

Peringatan harga:

- Model yang tidak ada di `pricingTable` **tidak lagi dihitung dengan harga Sonnet**: `EstimateTokenCost` mengembalikan 0 dan mencatat warning (`EstimateTokenCostKnown` memberi flag `known=false`; `GetTenantAICostEstimate` mendaftarkannya di `unpricedModels`). Biaya 0 di dashboard berarti "harga belum diketahui", bukan gratis. Tes `TestModelAllowlistHasPricing` menggagalkan build bila model allowlist belum punya harga.
- `GetTenantAICostEstimate` kini menghitung per model dari aktivitas AI (`ai_activity`), bukan harga Sonnet tetap atas total `ai_token`.
- Bila chain jatuh ke Anthropic tanpa model eksplisit, link itu memakai **Haiku** (US$1 masuk, US$5 keluar), bukan Sonnet. Skenario pertama menjadi sekitar Rp65 per pesan (± 4 kali `gemini-3.1-flash-lite`). Setel chain `chat` ke model yang disengaja.

---

## 7. Catatan environment

- Tidak ada secret, env, migrasi skema, atau endpoint baru. Event usage `ai_token_llm_first` memakai tabel `usage_event` yang sudah ada (tanpa kolom atau constraint baru).
- `GeminiAPIKey` hanya ada di Cloud Run, jadi Gemini hanya jalan di sana. Di Encore Cloud chain jatuh ke Anthropic (Haiku bila link tanpa model): mode JSON digantikan instruksi di prompt dan parse yang toleran; bila parse gagal dua kali, giliran dikembalikan ke jalur lama (fail-open), atau ditahan di ringkasan konfirmasi bila state di langkah `confirm` (§1.2). Jangan hidupkan `ai_llm_first` di Encore sebelum ada kunci Gemini (§3).
- Batas waktu: 15 detik per panggilan LLM, dihitung untuk seluruh link chain (timeout per link berlaku di dalamnya), maksimal 2 panggilan per giliran, anggaran job 40 detik. Error LLM tidak di-retry (langsung fallback), jadi sisa anggaran cukup untuk jalur lama.
- `INLINE_INBOUND_AI` dan `RETRIEVAL_BACKGROUND_DRAIN` tetap hanya di Cloud Run.

---

## 8. Batasan saat ini

Perilaku yang sengaja atau belum ditangani, supaya tidak mengejutkan saat canary:

- **Varian** didukung (§1.1) tetapi hanya sejauh data katalog: ukuran dikenali dari akhiran nama ("- L", "- XL", angka 2-3 digit) dan produk yang butuh varian dari kata kunci nama (kaos, celana, ...), sama dengan jalur lama. Pilihan ukuran/warna untuk produk tanpa ukuran di nama tidak diketahui (katalog tidak menyimpannya), jadi pembeli ditanya bebas dan nilai apa pun yang ia ketik diterima. Pembeli yang memilih baris berukuran lewat ucapan tanpa menyebut ukuran ("yang pertama") ditanya ukurannya.
- **Stok:** penolakan di tahap keranjang memakai total stok semua gudang; pembagian per gudang tetap diputuskan penjaga stok saat simpan.
- **Konfirmasi tertahan:** selama LLM gagal di langkah `confirm`, pembeli hanya melihat ringkasan yang sama berulang (owner diberi tahu sekali per jam). Perubahan isi pesanan baru bisa dilakukan lagi setelah LLM pulih.
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
| `internal/salesbrain/` | Bahasa, pesanan terbuka dan pengingat, prompt, parser keputusan, validator, template ID/EN, ringkasan dan redaksi; `variant.go`: `ResolveCartOps` (varian, stok, harga) dan kalimat penolakan |
| `internal/buyerflow/order_confirm.go` | `IsExplicitOrderConfirm` dan ringkasan ulang; langkah `confirm` fail-closed ada di `order_fsm.go` |
| `usage/aitracker.go` | `RecordLLMFirstTokens` (event `ai_token_llm_first`) |
| `usage/costmonitor.go` | Tabel harga model dan `EstimateTokenCost(Known)` |
| `flag/llm_first.go` | Key flag dan `LLMFirstMode` |
| `shared/llm/` | `RunChat` lewat chain fungsi, mode JSON, temperature, usage termasuk thinking |

## 10. Menjalankan tes

```bash
go test ./internal/salesbrain/ ./internal/buyerflow/ -race -count=1
encore test ./ai/ -run 'LLMFirst|Plan|FieldAnswer|CartFromState' -count=1
encore test ./ai/ -count=1          # seluruh paket ai
encore test ./flag/ -count=1
encore test ./internal/apitest/ -count=1
encore test ./shared/llm/ ./usage/ -count=1
```

`encore test -race` crash (SIGSEGV di runtime Encore saat inisialisasi trace) di mesin dev tertentu, juga di commit sebelum perubahan ini; pakai `go test -race` untuk `internal/buyerflow` dan `internal/salesbrain` (paket yang tidak butuh infrastruktur Encore).

Tes giliran utuh (`ai/llm_first_flow_test.go`) memakai Redis palsu, model palsu, dan pengirim simulator; di sana terbukti shadow tidak menulis apa pun dan urutan kirim, pause, notifikasi terjaga. Penjaga AST (`ai/llm_first_shell_test.go`) menggagalkan tes bila hook bergeser dari tempatnya atau jalur shadow memanggil fungsi yang menulis.
