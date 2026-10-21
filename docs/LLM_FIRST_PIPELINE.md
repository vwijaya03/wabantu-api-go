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
2. **Muat data (hanya baca):** riwayat 12 pesan, pesan keluar terakhir, katalog (loader + stok + harga tier yang sama dengan jalur lama), FAQ (50 baris), state keranjang Redis, pesanan dari DB (daftar belum selesai dan seleksi pengingat, §1.3a), konfirmasi "batalkan semua" yang menunggu (Redis), status pengingat dan penghitung salah paham di Redis.
3. **Bahasa:** `salesbrain.DecideLanguage` satu-satunya otoritas (`language_switch` dari model tidak pernah dipakai). Bahasa yang dikunci ditulis ke `contact.preferred_language` dan dicerminkan ke `contact.PreferredLanguage` di memori serta `SetSavedBuyerLang`, supaya `applyBuyerLanguageFromMessage` (defer di `ProcessAutoReply`) dan template lama tidak menimpanya bila giliran ini fail-open. Sapaan ambigu ("hi") dijawab satu bubble dua bahasa tanpa memanggil model.
4. **`planTurn`** (`ai/llm_first_plan.go`): prompt dari `salesbrain.BuildPrompt`, maksimal dua panggilan JSON (yang kedua membawa catatan pelanggaran), validasi balasan, operasi keranjang, dan ringkasan/total dirender kode.
5. **Efek samping** (`ai/llm_first_effects.go`) lalu **kirim** (`sendAiMessage`, path `llm_first`).

Pemakaian token tiap giliran dicatat dua kali di mode `on`: `ai_token` (kuota plan, tidak berubah) dan `ai_token_llm_first` (khusus pengukuran, tanpa batas kuota). Mode `shadow` hanya mencatat `ai_token_llm_first`.

### Efek samping per intent

| Intent | Yang dilakukan kode |
|--------|---------------------|
| `order`, `amend` | `setOrderState(stateWithCart(...))` bila keranjang berubah; `clearOrderState` bila kosong. Tawaran produk lain menandai `UpsellShown`. Balasan = teks model + ringkasan + kalimat lanjut. **Sesudah kirim**, keranjang yang berubah dan tidak kosong disimpan sebagai draft awal lewat `persistDraftOrderEarly` (fungsi yang sama dengan jalur lama); id draft dipasang di `PersistedOrderID` supaya amend dan `confirm` berikutnya memperbarui baris yang sama. Keranjang tanpa draft sendiri selalu draft baru (`ForceNewOrder`), jadi tidak pernah menyatu ke draft yang diparkir. Gagal simpan: dicatat di log dan owner diberi tahu, balasan tidak terhalang. Varian, stok, dan harga: lihat §1.1. |
| `field_answer` | `applyFieldAnswer` (nilai harus muncul di pesan pembeli) lalu `setOrderState`. Data tidak valid disebut persis (template `TplFieldProblems`) dan kolomnya ditanya lagi. Setelah lengkap: ringkasan penuh (item, alamat, minta OK). |
| `confirm` | Hanya di langkah `confirm` dengan alamat lengkap **dan** teks pembeli lolos `IsExplicitOrderConfirm`: `persistDraftOrder`. Intent `confirm` dari model saja tidak cukup. Tanpa draft yang terikat = draft baru. Balasan + `TplOrderRef`. Gagal simpan: `TplSystemBusy` + notifikasi owner, keranjang dan draft yang terikat tetap. Bila masih ada baris yang belum punya ukuran/warna, tidak ada yang disimpan: langkah kembali ke `ask_variant` (`replacedReason = variant_missing`). |
| `cancel` | Model mengisi `cancel_scope`: `one` (default) atau `all`. **Satu pesanan:** draft belum bayar: `cancelPersistedOrder` + `clearOrderState`, balasan template `TplOrderCancelled`. Pesanan yang sudah dibayar **tidak pernah** dibatalkan bot: notifikasi owner dulu, lalu `TplCancelPaidOrder`. Nomor `WB-…` di pesan menentukan pesanan; nomor tidak dikenal tidak membatalkan apa pun. Dengan dua pesanan belum selesai atau lebih (umur berapa pun), tanpa nomor, dan tanpa keranjang hidup, tidak ada yang dibatalkan: pembeli menerima daftar pilihan `TplCancelPick` (`replacedReason = cancel_pick`), bukan balasan model. Keranjang yang belum tersimpan cukup dikosongkan. **Semua (`all`):** lihat §1.4. |
| `continue_order` | Draft terbuka terbaru (atau yang disebut nomornya) dipulihkan ke Redis dari DB; hanya kolom yang kurang yang ditanya. Pesanan yang menunggu bayar tidak dipulihkan (tidak ada yang tersisa untuk dikumpulkan). |
| `new_order` | `clearOrderState`; draft lama **tetap** di DB (diparkir, tetap muncul di pengingat) dan keranjang berikutnya menjadi draft baru. `TplNewOrderParked` bila model belum menyebut nomornya. |
| `status` | `handleCustomerOrderStatus` (jalur lama; membalas sendiri). Semua teks status dua bahasa; bahasa dipilih `ResolveBuyerLanguage(pesan, bahasa terkunci kontak)`, jadi pembeli Inggris menerima status Inggris. |
| `human_request`, atau `handoff.level = takeover` | Setelah balasan terkirim: `pauseAI(HandoffReasonHumanRequest)` lalu `notifyOwner("ai.handoff")`. Balasan memuat jam buka tenant. AI lanjut otomatis setelah 2 jam tanpa balasan staf. **Pengecualian:** bila aksi transaksional giliran itu sudah berhasil dan intent bukan `human_request`, takeover diturunkan menjadi `note` (§1.5). |
| `handoff.level = note` | `notifyOwner` saja; AI tetap melayani. |

Status pengingat pesanan (TTL 72 jam) dan penghitung salah paham beruntun (TTL 24 jam) ditulis ke Redis **sesudah** balasan terkirim.

### 1.1 Varian, stok, dan harga

**Model data varian (tidak ada tabel varian).** Tidak ada tabel `variant` atau kolom varian di katalog; `inv_sku` hanya konfigurasi stok per baris katalog (kunci `catalog_item_id`). Varian dimodelkan dari baris `business_catalog_item` itu sendiri: (1) baris per ukuran dengan akhiran nama ("Boxer Pria Mono - L", sibling = `BaseName` sama), (2) baris per warna dengan akhiran nama ("Kaos Polos Premium - Hitam"), (3) produk satu baris yang butuh ukuran/warna (kaos, celana) dengan nilai dicatat di baris keranjang (`Size`, `Color`) dan ke pesanan. Harga dan stok per varian = harga dan stok baris katalog varian itu. Yang dilihat model per baris katalog (`writeVariantInfo`): `ukuran`, `warna`, dan `varian lain: no N (label, harga, stok)`.

Katalog menyimpan varian sebagai baris terpisah per ukuran ("Boxer Pria Mono - L", "... - M"), plus produk yang butuh ukuran/warna tanpa ukuran di namanya (kaos, celana). Aturan yang sama dengan jalur lama (`buyerflow.CatalogItemNeedsVariant`, `ExtractSizeFromProductName`) menurunkan data varian per baris katalog (`lfCatalogFacts`): `Size`, `BaseName` (baris dengan `BaseName` sama adalah ukuran lain dari produk yang sama), `NeedsVariant`, dan stok dari `enrichCatalogStock`; harga dari `applyContactTierPrices`. Model tidak pernah mengisi data ini.

Model hanya mengusulkan `items: [{no, qty, size, color}]`. Kode (`salesbrain.ResolveCartOps`) yang memutuskan:

| Situasi | Perilaku |
|---------|----------|
| Satu produk, beberapa ukuran/warna | Satu baris per varian. Mengubah jumlah satu varian tidak menyentuh varian lain; jumlah 0 tanpa ukuran menghapus semua varian produk itu; jumlah baru tanpa ukuran ketika ada dua varian atau lebih ditanyakan balik (tidak ada yang berubah). |
| `size`/`color` dari model | Dipakai **hanya bila kata itu muncul utuh di pesan pembeli saat ini** (bukan riwayat alamat/nomor rumah), seperti `applyFieldAnswer`. Warna harus kata warna yang dikenal atau muncul di katalog produk itu; nilai yang tidak diketik pembeli diabaikan. |
| Stok per produk | Jumlah semua baris keranjang dengan `catalog_item_id` yang sama dibandingkan ke `StockQty` baris itu; melebihi → `TplStockLimit`, bukan gagal simpan diam-diam. |
| Ukuran yang disebut pembeli beda dengan baris yang dipilih model | Dipindah ke baris saudara dengan ukuran itu (harga baris itu). Ukuran yang tidak ada: balasan template `TplVariantNotFound` dengan pilihan yang ada. |
| Baris berukuran dengan saudara, tapi pembeli tidak menyebut ukuran | Tidak ditambahkan; template `TplAskVariantChoices` menyebut ukuran yang masih ada stoknya. |
| Produk yang butuh ukuran/warna, belum disebut | Baris tetap masuk keranjang dengan jumlahnya (`VariantMissing`), langkah menjadi `ask_variant`, pembeli ditanya **sekali**. Bila katalog tahu pilihannya (baris ukuran/warna yang masih ada stok, atau warna di deskripsi), pertanyaan menyebutnya (`TplAskVariantOptions`); bila tidak, pertanyaan terbuka (`TplAskVariant`). Draft awal dan langkah penerima/alamat/konfirmasi menunggu sampai varian terisi; jawabannya mengisi baris yang sama (bukan baris baru) dan checkout lanjut dari data yang sudah terkumpul (alamat tidak ditanya ulang). |
| Stok habis | Tidak ditambahkan. `TplVariantSoldOut` jujur + varian saudara (ukuran **atau warna**) yang masih ada stoknya (`TplVariantAlternatives`). Berlaku juga untuk produk tanpa varian. |
| Jumlah melebihi stok | Tidak ditambahkan/dinaikkan. `TplStockLimit` menyebut sisa. Baris tanpa data stok (stok tidak dilacak) tidak ditolak di sini; penjaga stok saat simpan tetap berlaku. |

Bila ada penolakan atau pertanyaan varian, teks model **diganti** kalimat template (model bisa saja menulis "sudah masuk keranjang") ditambah ringkasan keranjang bila berubah dan kalimat lanjut (`replacedReason = cart_issue`). Ringkasan menulis varian per baris ("2x Kaos Polos (ukuran M, warna hitam)"). Ukuran yang sudah ada di nama produk tidak diulang. Semua kalimat ini template ID/EN.

### 1.2 Fail-open dan fail-closed

**Fail-open (default).** Kegagalan sebelum tulisan pertama mengembalikan `(false, nil)` dan jalur lama menangani giliran itu: gagal muat data, gagal menulis bahasa, error LLM, JSON/validasi rusak dua kali, teks yang mirip injeksi prompt (penjaganya ada di belakang hook). Pemakaian token tetap dicatat (`ai_token` dan `ai_token_llm_first`) walau fallback. **Error pipeline tidak pernah memicu pause.**

**Fail-closed di langkah `confirm`.** Langkah `confirm` berarti pembeli sedang melihat ringkasan akhir dan menunggu OK. Jalur lama tidak boleh menebak apakah balasan berikutnya persetujuan, jadi:

- Bila `planTurn` fallback (error LLM atau output rusak dua kali) **dan** state Redis di langkah `confirm` dengan alamat lengkap dan minimal satu baris: pipeline **tidak** mengembalikan `(false, nil)`. Draft dan state dibiarkan apa adanya, tidak ada yang disimpan, tidak ada pause. Pembeli menerima `TplConfirmHold` + ringkasan penuh (item, alamat, `TplResumeConfirm`) sebagai pesan `system` (`replacedReason = confirm_fail_closed`), dan owner mendapat notifikasi `ai.system_error` (dedupe satu per percakapan per jam).
- Penjaga ketat itu adalah `buyerflow.IsExplicitOrderConfirm` (`internal/buyerflow/order_confirm.go`). Hasil yang diuji: error LLM di `confirm`, output tidak valid dua kali di `confirm`, jalur lama di `confirm` dengan teks bukan konfirmasi (tidak menyimpan), dan jalur lama dengan konfirmasi eksplisit (satu-satunya yang menyimpan). Tes: `TestLLMFirstFlowFailureAtConfirmKeepsTheDraftAndAsksForTheOKAgain` dan `internal/buyerflow/order_confirm_failclosed_test.go`.
- Jalur lama (`internal/buyerflow/order_fsm.go`) kini mengenal langkah `confirm` dan **tidak pernah menyelesaikan pesanan tanpa persetujuan eksplisit**: `normalizeOrderState` tidak lagi memetakan `confirm` ke `ask_address_full`. Di langkah itu pesanan hanya disimpan bila teks pembeli lolos `IsExplicitOrderConfirm` (hanya kata persetujuan: "ok", "ya benar kak", "lanjut proses", "looks good", 👍; kalimat dengan kata lain, pertanyaan, dan penolakan tidak lolos). Teks lain menjaga draft di `confirm` dan menampilkan ringkasan lagi dengan ajakan OK; data penerima/alamat yang kurang mengembalikan langkah ke `ask_recipient`/`ask_address_full`. Ini juga melindungi jalur fail-open lain (gagal muat data, gagal tulis bahasa, teks injeksi) yang jatuh ke jalur lama saat state di `confirm`.

### 1.3 Ringkasan percakapan (bergulir dan sadar sesi)

Ringkasan disimpan di tabel `conversation_summary` (satu baris per percakapan) oleh subscription Pub/Sub `summarizer` (`ai/memory.go`). Pemicunya `ai.ShouldTriggerSummary`, dipanggil webhook (`webhook/webhook.go`) pada tiap pesan masuk.

- **Bergulir.** Tiap kali jalan, prompt berisi ringkasan sebelumnya (bila ada) ditambah pesan baru sejak `updated_at`, dan model mengembalikan satu ringkasan gabungan (maksimal sekitar 5 kalimat). Peringkas diminta menyimpan preferensi, pertanyaan, dan produk yang diminati, tidak menganggap status pesanan, harga, alamat, atau pembayaran sebagai fakta final (itu ada di STATE), dan membuang hal yang sudah selesai. `message_count` kumulatif. Maksimal 100 pesan terbaru dibaca per jalan. `updated_at` diisi waktu pesan terbaru yang ikut diringkas (bukan `NOW()`), sehingga pesan yang masuk selama panggilan model tetap dihitung pemicu berikutnya.
- **Pemicu.** Sedikitnya 20 pesan sejak ringkasan terakhir diperbarui, **atau** sedikitnya 4 pesan dan jarak pesan terbaru ke pesan sebelumnya 72 jam atau lebih (pembeli kembali: kunjungan sebelumnya diringkas). Satu query ringan atas `message` ditambah satu atas `conversation_summary`.
- **Model** lewat registry dinamis `llm.FnMemory` (diatur dari pengaturan admin); tidak ada model yang ditanam di kode.
- **Dipakai LLM-first, sadar sesi** (`salesbrain.DecideSummary`, `internal/salesbrain/summary.go`): umur ringkasan dihitung dari `updated_at`.
  - sampai 72 jam: blok `RINGKASAN PERCAKAPAN` (latar percakapan ini);
  - lewat 72 jam sampai 30 hari: blok `KUNJUNGAN SEBELUMNYA (N hari lalu) — hanya latar, jangan anggap masih berlaku`;
  - lewat 30 hari, kosong, atau tidak ada baris: tidak ditampilkan.
- **Keamanan prompt.** Blok berada di prompt dinamis (antara STATE dan RIWAYAT), dipotong 600 karakter, dan email serta angka panjang (8 digit atau lebih) disamarkan dengan `RedactForLog`. Aturan di system prompt: STATE selalu menang untuk keranjang, pesanan, alamat, bahasa, dan pembayaran; ringkasan tidak boleh dikutip untuk harga atau jumlah uang. `BuildFacts` tidak membaca ringkasan, jadi harga basi dari ringkasan ditolak validator. Gagal membaca ringkasan hanya dicatat di log; balasan tetap jalan tanpa ringkasan.
- **Biaya.** Satu panggilan model memori per 20 pesan atau per kunjungan kembali. Token tetap dicatat sebagai `ai_token` dan `ai_activity` (purpose `conversation_summary`) seperti sebelumnya; tidak ada perubahan kuota. Tambahan di prompt LLM-first paling banyak sekitar 600 karakter per giliran, hanya bila ada ringkasan yang masih berlaku.
- **Bug yang diperbaiki.** (1) Penghitung membaca `created_at`, yang tidak pernah berubah setelah upsert pertama, sehingga setelah ringkasan pertama pemicu hampir selalu menyala dan pesan lama dibaca ulang; kini `updated_at`. (2) Ringkasan menimpa tanpa menggabung sehingga konteks awal hilang; kini bergulir. (3) `LIMIT 100` mengambil 100 pesan terlama; kini 100 terbaru (urutan kronologis). (4) Pembeli yang kembali setelah jeda panjang dengan sedikit pesan tidak pernah memicu ringkasan.
- **Environment.** Tanpa env, secret, atau migrasi. Di Cloud Run, peringkas hanya berjalan setelah perbaikan audience JWT Pub/Sub (PR #265/#266) ter-deploy; di Encore Cloud langsung jalan. Sebelum itu tidak ada baris ringkasan baru dan LLM-first berjalan tanpa blok ini.
- **Kunci inflight (perbaikan 21 Okt 2026).** `TryPublishSummarize` mengambil kunci Redis `ai:summarize:inflight:<convo>` (SetNX, TTL 2 menit) lalu mem-publish. Handler sebelumnya mengambil kunci YANG SAMA lagi, selalu gagal, dan mencatat "summarize skipped: already inflight", sehingga ringkasan tidak pernah ditulis. Kini **publisher memiliki kunci**; `handleSummarize` tidak mengambilnya lagi, langsung menjalankan `SummarizeConversation`, dan melepas kunci di akhir (`defer`, konteks tanpa cancel, 5 detik). Kunci yang tidak sempat dilepas (handler tidak jalan atau crash) bebas sendiri oleh TTL 2 menit, lebih panjang dari batas handler 45 detik. Pengiriman ganda (at-least-once) tidak diberi kunci kedua: `SummarizeConversation` membaca pesan setelah ringkasan terakhir dan meng-upsert satu baris per percakapan, jadi proses ulang hanya menulis ulang ringkasan yang sama.
- Tes: `internal/salesbrain/summary_test.go`, `ai/memory_test.go`, `ai/memory_inflight_test.go`.

---

---

### 1.3a Pesanan belum selesai (menjawab) vs pesanan untuk pengingat

Dua daftar dengan tujuan berbeda; keduanya dibangun dari `loadActiveOrdersForContact` (semua pesanan percakapan ini yang belum `cancelled`).

| Daftar | Isi | Dipakai untuk |
|--------|-----|---------------|
| `TurnState.OpenOrders` (+ `OpenOrdersMore`) | **Semua pesanan belum selesai, umur berapa pun**: draft belum bayar (termasuk tanpa item), menunggu bayar, bukti transfer sedang dicek, `processing`/`confirmed`/`paid`. Bukan `cancelled`, `shipped`, `completed`. Terbaru dulu, maksimal 10 (`salesbrain.MaxUnfinishedListed`); sisanya hanya dihitung ("dan N pesanan lainnya"). Dibangun `mapUnfinishedOrders` / `lfUnfinishedSnapshot`. | STATE untuk menjawab "apakah saya punya pesanan?" (blok `pesanan_belum_selesai`, yang menyatakan tegas "TIDAK ADA" atau "ADA N pesanan" beserta daftar); pilihan target `cancel` dan `continue_order`; `BuildFacts` (nomor, harga, total semua pesanan yang tampil boleh dikutip model); `lfApplyNewOrder` (menyebut draft yang diparkir, hanya yang belum dibayar). |
| `TurnState.RemindOrders` | Aturan lama: draft / menunggu bayar yang diperbarui dalam 72 jam, terbaru dulu, maksimal 3 (`salesbrain.SelectRemindOrders`, `mapRemindOrders`). | **Hanya** pengingat proaktif: `ReminderAllowed` (`RemindAllowed`), baris `pesanan_untuk_diingatkan` di prompt, dan `lfRecordBookkeeping` (menyimpan hitungan pengingat per nomor). Tidak boleh dipakai untuk menjawab pembeli. |

Aturan prompt: model tidak boleh menyatakan pembeli tidak punya pesanan kecuali blok `pesanan_belum_selesai` berkata TIDAK ADA; pesanan yang tidak ada di daftar (dibatalkan, dikirim, selesai) dicek lewat nomornya (intent `status`).

### 1.4 "Batalkan semua pesanan"

Keputusan datang dari model, bukan regex: JSON keputusan punya field `cancel_scope` (`"one"` | `"all"`, hanya dibaca untuk intent `cancel`; nilai lain dibuang oleh `ParseDecision`). Alur `scope = all` (`ai/llm_first_effects.go`):

1. **Tanya konfirmasi** (`lfAskCancelAll`), hanya bila ada dua pesanan belum selesai atau lebih dan pesan tidak menyebut nomor. Tidak ada yang dibatalkan. Kode menyimpan id pesanan di Redis `ai:cancelall:<tenant>:<convo>` (JSON array, TTL 15 menit) dan membalas `TplCancelAllConfirm` berisi daftar bernomor (pesanan yang sudah dibayar diberi tanda "diteruskan ke tim"). Gagal menyimpan: `TplSystemBusy`, tidak ada pertanyaan yang diajukan (fail-closed). Kunci sendiri, bukan `ai:order:*`, supaya alur lama tidak melihat state tanpa keranjang dan keranjang hidup tidak tertimpa.
2. **Giliran berikutnya.** STATE memuat `konfirmasi_batal_semua: MENUNGGU ...`. Pembatalan berjalan hanya bila intent model `confirm` (atau `cancel` + `all`) **dan** teks pembeli lolos `bf.IsExplicitOrderConfirm` (validator OK yang sama dengan checkout). Selain itu (menolak, bertanya hal lain, "ok tapi ...") kunci dihapus dan tidak ada yang dibatalkan.
3. **Eksekusi** (`lfApplyCancelAllConfirmed`) atas id yang tersimpan: draft belum bayar dibatalkan satu per satu (`cancelPersistedOrder`); pesanan berbayar tidak disentuh, satu notifikasi owner `notifyTitleCancelPaid` memuat semua nomornya lalu `TplCancelPaidOrder`/`TplCancelPaidMany`; id yang sudah tidak ada di daftar aktif dianggap sudah dibatalkan (retry job); pesanan yang sudah `shipped`/`completed` tidak disentuh. Balasan `TplOrderCancelled`/`TplCancelledMany` (`replacedReason = cancel_all_template`, `orderAction = cancel_all`). Error DB dikembalikan sehingga job mengulang; kunci Redis baru dihapus sesudah balasan terkirim (`After`), jadi ulangan lengkap.
4. Hanya satu pesanan belum selesai: diperlakukan sebagai `cancel` tunggal (tanpa konfirmasi).

### 1.5 Takeover setelah aksi berhasil diturunkan menjadi note

`lfApplyHandoff`: bila `handoff.level = takeover` tetapi giliran itu sudah menjalankan aksi transaksional yang berhasil (`lfOutcome.ActionRan`: cancel, permintaan batal pesanan berbayar, pertanyaan atau eksekusi batal-semua, pesanan tersimpan, keranjang berubah) **dan** intent bukan `human_request`, maka level menjadi `note`: owner mendapat `notifyTitleNoAnswer` dengan alasan "AI meminta takeover, tetapi aksi pembeli sudah berhasil diproses sistem sehingga AI tetap aktif" ditambah catatan model; AI **tidak** di-pause; kalimat jam buka **tidak** ditambahkan. Takeover tetap berlaku bila pembeli minta orang (`human_request`) atau tidak ada aksi yang berjalan (komplain, dll.). Persona juga diberi tahu agar tidak meminta takeover hanya karena pembeli mengulang permintaan batal/status. (Intent `status` dijawab handler status lama dan tidak melewati `lfApplyHandoff`.)

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
- **Jangan nyalakan ai_llm_first di Encore Cloud sebelum GeminiAPIKey tersedia.** Tanpa Gemini, chain jatuh ke Anthropic dan link tanpa model memakai **Haiku** (`DefaultHaikuAPIID`, ±US$1 masuk / US$5 keluar per 1 juta token), bukan Sonnet. Haiku tetap ±4 kali lebih mahal dari `gemini-3.1-flash-lite` dan JSON hanya lewat instruksi prompt (§7), jadi Encore tanpa Gemini cukup untuk shadow/uji, bukan untuk rollout.
- **Cloud Run: set chain chat eksplisit ke gemini-3.1-flash-lite via AI Provider.** Model chat tidak diatur lewat env atau file deploy. `deploy/cloudrun/` dan `scripts/cloudrun/deploy.sh` di branch `feat/pelayan-toko-llm-first-cr` hanya meneruskan `GeminiAPIKey`; tidak ada konfigurasi Neon/deploy yang menyebut model chat. Yang menentukan adalah baris `function-config` chat di database (migrasi 29 menyemai `gemini-3.1-flash-lite`). Sebelum canary di CR, setel baris itu **secara eksplisit** ke `gemini-3.1-flash-lite` lewat endpoint di atas dan cek hasilnya di `GET /api/v1/admin/ai/function-config`, jangan mengandalkan seed.
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
- Kunci `ai:openorder:remind:*` (72 jam), `ai:llmfirst:confused:*` (24 jam), dan `ai:cancelall:*` (15 menit, konfirmasi batalkan-semua) tidak dibaca jalur lama.

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

Event usage terpisah untuk mengukur pipeline ini per pesan. Nilainya input + output + thinking token satu giliran; metadata: `mode` (`on` atau `shadow`), `pipeline` (`llm_first` atau `llm_first_shadow`), `tenantId`, `conversationId`, `inboundMessageId`, `provider`, `model`, `intent`, `fallback`, `attempts`, `inputTokens`, `outputTokens`, `thoughtsTokens`, `costKnown`, dan `estimatedCostUsd` (input x harga masuk + (output + thinking) x harga keluar dari `pricingTable`). Model tanpa harga: `costKnown=false`, `costNote="harga belum diketahui"`, dan `estimatedCostUsd` **tidak ditulis** (bukan 0, supaya tidak terbaca gratis). Tidak ada entri di `planQuotas` untuk event ini, jadi tidak membatasi apa pun dan kuota `ai_token` plan tidak berubah (mode `on` tetap mencatat `ai_token` seperti sebelumnya untuk kuota/billing; `shadow` hanya mencatat `ai_token_llm_first`). Event tidak muncul di ringkasan kuota karena ringkasan hanya menampilkan event yang punya kuota.

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

Per pesan (satu baris per giliran; kolom biaya kosong = harga belum diketahui):

```sql
SELECT created_at,
       metadata->>'mode'                        AS mode,
       metadata->>'conversationId'              AS conversation_id,
       metadata->>'inboundMessageId'            AS inbound_id,
       metadata->>'model'                       AS model,
       (metadata->>'inputTokens')::int          AS in_tok,
       (metadata->>'outputTokens')::int         AS out_tok,
       (metadata->>'thoughtsTokens')::int       AS thought_tok,
       metadata->>'attempts'                    AS attempts,
       metadata->>'fallback'                    AS fallback,
       (metadata->>'estimatedCostUsd')::numeric AS cost_usd,
       round((metadata->>'estimatedCostUsd')::numeric * 16500, 1) AS cost_idr
FROM "<tenant_schema>".usage_event
WHERE event_type = 'ai_token_llm_first'
ORDER BY created_at DESC
LIMIT 100;
```

Per tenant (jalankan di schema tiap tenant; satu schema = satu tenant), 30 hari, dipisah mode:

```sql
SELECT metadata->>'tenantId'                                   AS tenant_id,
       metadata->>'mode'                                       AS mode,
       count(*)                                                AS turns,
       count(*) FILTER (WHERE (metadata->>'fallback')::boolean) AS fallbacks,
       sum(quantity)                                           AS tokens,
       sum((metadata->>'estimatedCostUsd')::numeric)           AS cost_usd,
       round(sum((metadata->>'estimatedCostUsd')::numeric) * 16500) AS cost_idr,
       count(*) FILTER (WHERE NOT (metadata->>'costKnown')::boolean) AS turns_price_unknown
FROM "<tenant_schema>".usage_event
WHERE event_type = 'ai_token_llm_first' AND created_at >= now() - interval '30 days'
GROUP BY 1, 2
ORDER BY 1, 2;
```

`cost_usd` hanya menjumlah giliran yang harganya diketahui; `turns_price_unknown` > 0 berarti totalnya kurang dari biaya sebenarnya. Event lama (sebelum kolom ini ada) tidak punya `costKnown`; kolom itu NULL di sana.

### 5.2 Endpoint admin pemakaian LLM-first

Padanan query §5.1 tanpa akses database: `GET /api/v1/admin/ai/llm-first/usage?tenantId=<uuid>&days=<1..90, default 7>` (`admin/ai_llm_first_usage.go`; agregasi murni `usage.AggregateLLMFirstUsage`). Hanya baca. `//encore:api auth` dengan `tag:super_admin`; `requireSuperAdmin` memeriksa role dari sesi auth (bukan dari parameter) dan hanya menerima token admin (token klien dan impersonasi: 403). Schema tenant diambil dari lookup `tenant` + `tenant_company` (tidak pernah dari input), nama schema divalidasi lagi dan di-quote, sisanya parameter SQL. `tenantId` bukan UUID atau `days` di luar 1..90 atau bukan angka: 400. Tenant tidak ada: 404.

Respons (camelCase):

```json
{ "tenantId": "...", "days": 7,
  "totals": { "messages": 0, "liveMessages": 0, "shadowMessages": 0, "fallbacks": 0,
              "inputTokens": 0, "outputTokens": 0, "thoughtsTokens": 0,
              "estimatedCostUsd": 0.0, "unknownCostMessages": 0 },
  "avgPerMessage": { "inputTokens": 0.0, "outputTokens": 0.0, "thoughtsTokens": 0.0, "costUsd": 0.0, "costIdr": 0.0 },
  "byDay": [ { "date": "2026-10-10", "mode": "on", "messages": 0, "inputTokens": 0, "outputTokens": 0,
               "thoughtsTokens": 0, "estimatedCostUsd": 0.0, "unknownCostMessages": 0 } ],
  "byModel": [ { "model": "gemini-3.1-flash-lite", "messages": 0, "estimatedCostUsd": 0.0, "costKnown": true } ],
  "idrPerUsd": 16500 }
```

- `liveMessages` = mode `on`, `shadowMessages` = mode `shadow` (event lama tanpa `mode` dibaca dari `pipeline`). `fallbacks` = event dengan `fallback=true`.
- Pesan dengan biaya tidak diketahui (`costKnown=false`, atau event lama tanpa `estimatedCostUsd`) tidak ikut jumlah `estimatedCostUsd` dan dihitung di `unknownCostMessages`. `avgPerMessage.costUsd` merata-ratakan hanya pesan berbiaya diketahui; `costIdr` = `costUsd` x `idrPerUsd`. Rata-rata token memakai semua pesan. Tanpa data, semua angka 0 dan array kosong.
- `byDay` memakai tanggal Asia/Jakarta (WIB, UTC+7), urut tanggal lalu `on` sebelum `shadow`. `byModel` urut jumlah pesan menurun; `costKnown=false` bila ada satu saja pesan model itu tanpa biaya; model kosong dilaporkan `unknown`.

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

Ukuran prompt, **diukur** dengan `salesbrain.BuildPrompt` + `salesbrain.EstimateTokens` (3,5 karakter per token) pada fixture tenant Omah di `ai/llm_first_plan_test.go` (`lfOmahProfile`, `lfOmahCatalog` 5 produk, `lfOmahKB` 10 FAQ), 2026-10-17:

| Giliran | Token masuk (taksiran) |
|---------|------------------------|
| Pesan pertama ("mau beli salmon 100 gram 2 ya"), tanpa riwayat dan state | 2.545 (prompt sistem 2.468 + blok user 77) |
| Giliran tengah checkout (state keranjang + 6 pesan riwayat) | 2.738 (blok user 271) |

Keluaran + thinking diasumsikan 250 token per panggilan (belum terukur; ukur lewat §5.1 setelah shadow). Mode `full` dipakai selama katalog + FAQ <= 5.000 token; lebih besar dari itu katalog dipangkas ke 12 produk terbaik (ditambah isi keranjang) dan FAQ memakai hasil retrieval.

Harga Standard paid tier teks (`usage/costmonitor.go`, sumber [ai.google.dev/gemini-api/docs/pricing](https://ai.google.dev/gemini-api/docs/pricing), dibaca 2026-10-17; thinking dihitung sebagai output):

| Model | Masuk / keluar per 1 juta token |
|-------|---------------------------------|
| `gemini-3.1-flash-lite` | US$0,25 / US$1,50 |
| `gemini-3.5-flash-lite` | US$0,30 / US$2,50 |
| `gemini-3.6-flash`, `gemini-3.8-flash` | US$0,75 / US$3,75 sampai 31 Des 2026; US$1,50 / US$7,50 mulai 1 Jan 2027 (**ubah dua baris tabel itu pada 1 Jan 2027**) |
| `gemini-3.1-pro-preview` | US$2,00 / US$12,00 (prompt ≤ 200 ribu token; tier di atasnya tidak dimodelkan) |

Aritmetika per pesan (USD = masuk x harga masuk / 1.000.000 + 250 x harga keluar / 1.000.000; IDR = USD x 16.500):

| Skenario | Model | Hitungan USD | USD | IDR (Rp16.500) |
|----------|-------|--------------|-----|----------------|
| Pesan pertama (2.545 masuk) | `gemini-3.1-flash-lite` (0,25 / 1,50) | 2.545 x 0,25e-6 = 0,00063625 + 250 x 1,50e-6 = 0,000375 | 0,00101125 | **± Rp16,7** |
| Pesan pertama (2.545 masuk) | `claude-haiku-4-5` (1,00 / 5,00) | 2.545 x 1e-6 = 0,002545 + 250 x 5e-6 = 0,00125 | 0,003795 | **± Rp62,6** |
| Giliran tengah (2.738 masuk) | `gemini-3.1-flash-lite` | 2.738 x 0,25e-6 = 0,0006845 + 0,000375 | 0,0010595 | ± Rp17,5 |
| Giliran tengah (2.738 masuk) | `claude-haiku-4-5` | 2.738 x 1e-6 = 0,002738 + 0,00125 | 0,003988 | ± Rp65,8 |
| Pesan pertama + satu retry (2 panggilan) | `gemini-3.1-flash-lite` | 2 x 0,00101125 | 0,0020225 | ± Rp33,4 |
| Pesan pertama + satu retry (2 panggilan) | `claude-haiku-4-5` | 2 x 0,003795 | 0,00759 | ± Rp125,2 |

Haiku jadi sekitar 3,75 kali `gemini-3.1-flash-lite` pada ukuran prompt ini. Target spec (<= Rp20 per pesan) tercapai di `gemini-3.1-flash-lite` untuk tenant kecil, dan tidak tercapai di Haiku; tenant dengan katalog dan FAQ mendekati batas `full` (± 7.000 masuk) berada di atasnya di kedua model. Caching implisit Gemini tidak diperhitungkan.

Peringatan harga:

- Model yang tidak ada di `pricingTable` **tidak lagi dihitung dengan harga Sonnet**: `EstimateTokenCost` mengembalikan 0 dan mencatat warning (`EstimateTokenCostKnown` memberi flag `known=false`; `GetTenantAICostEstimate` mendaftarkannya di `unpricedModels`). Biaya 0 di dashboard berarti "harga belum diketahui", bukan gratis. Tes `TestModelAllowlistHasPricing` menggagalkan build bila model allowlist belum punya harga.
- `GetTenantAICostEstimate` kini menghitung per model dari aktivitas AI (`ai_activity`), bukan harga Sonnet tetap atas total `ai_token`.
- Bila chain jatuh ke Anthropic tanpa model eksplisit, link itu memakai **Haiku** (US$1 masuk, US$5 keluar), bukan Sonnet. Pesan pertama menjadi sekitar Rp63 per pesan (± 3,75 kali `gemini-3.1-flash-lite`, lihat tabel di atas). Setel chain `chat` ke model yang disengaja.

---

## 7. Catatan environment

**Chain default `chat` (kode).** Tanpa baris `function-config` tenant atau global, chain `chat` adalah `gemini-3.1-flash-lite` lalu `claude-haiku-4-5`. Link Anthropic tanpa model di jalur LLM-first memakai `DefaultHaikuAPIID()` lewat `lfAnthropicConfig` (`ai/llm_first.go`), bukan `defaultAnthropicModel` (Sonnet); tesnya `TestLLMFirstChatDefaultsToHaikuNotSonnet`. Tenant yang belum dikonfigurasi tidak pernah membayar harga Sonnet per pesan.

- Tidak ada secret, env, atau migrasi skema. Satu endpoint admin baca-saja (§5.2). Event usage `ai_token_llm_first` memakai tabel `usage_event` yang sudah ada (tanpa kolom atau constraint baru).
- `GeminiAPIKey` hanya ada di Cloud Run, jadi Gemini hanya jalan di sana. Di Encore Cloud chain jatuh ke Anthropic (Haiku bila link tanpa model): mode JSON digantikan instruksi di prompt dan parse yang toleran; bila parse gagal dua kali, giliran dikembalikan ke jalur lama (fail-open), atau ditahan di ringkasan konfirmasi bila state di langkah `confirm` (§1.2). Jangan hidupkan `ai_llm_first` di Encore sebelum ada kunci Gemini (§3).
- Batas waktu: 15 detik per panggilan LLM, dihitung untuk seluruh link chain (timeout per link berlaku di dalamnya), maksimal 2 panggilan per giliran, anggaran job 40 detik. Error LLM tidak di-retry (langsung fallback), jadi sisa anggaran cukup untuk jalur lama.
- `INLINE_INBOUND_AI` dan `RETRIEVAL_BACKGROUND_DRAIN` tetap hanya di Cloud Run.

---

## 8. Batasan saat ini

Perilaku yang sengaja atau belum ditangani, supaya tidak mengejutkan saat canary:

- **Varian** didukung (§1.1) tetapi hanya sejauh data katalog: ukuran dikenali dari akhiran nama ("- L", "- XL", angka 2-3 digit), warna dari akhiran nama atau baris "Warna: ..." di deskripsi, dan produk yang butuh varian dari kata kunci nama (kaos, celana, ...), sama dengan jalur lama. Stok produk satu baris dengan banyak warna/ukuran adalah satu kolam (tidak ada stok per ukuran). Bila katalog tidak menyebut pilihan sama sekali, pembeli ditanya bebas dan nilai apa pun yang ia ketik diterima. Pembeli yang memilih baris berukuran lewat ucapan tanpa menyebut ukuran ("yang pertama") ditanya ukurannya.
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
| `ai/memory.go` | Peringkas percakapan bergulir (`SummarizeConversation`), pemicu `ShouldTriggerSummary`, `getSummaryWithTime` (§1.3) |
| `ai/llm_first_turn.go` | Helper murni: waktu, riwayat, daftar pesanan belum selesai (`mapUnfinishedOrders`) dan seleksi pengingat (`mapRemindOrders`), katalog top-k, input prompt |
| `ai/reply_meta.go` | `AiReplyMeta` dengan field pipeline |
| `internal/salesbrain/` | Bahasa, pesanan terbuka dan pengingat, prompt, parser keputusan, validator, template ID/EN, ringkasan dan redaksi; `variant.go`: `ResolveCartOps` (varian, stok, harga) dan kalimat penolakan |
| `internal/buyerflow/order_confirm.go` | `IsExplicitOrderConfirm` dan ringkasan ulang; langkah `confirm` fail-closed ada di `order_fsm.go` |
| `usage/aitracker.go` | `RecordLLMFirstTokens` (event `ai_token_llm_first`) |
| `usage/llmfirst_admin_usage.go`, `admin/ai_llm_first_usage.go` | Agregasi pemakaian per tenant (`AggregateLLMFirstUsage`) dan endpoint admin §5.2 |
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

## 11. Insiden 18 Okt

Tenant omah_apparel, mode `on`, model gemini-3.1-flash-lite, 18 Okt 00:17-00:21 WIB. Model mengklaim hal yang tidak dikerjakan kode, dan alamat yang jelas-jelas ditulis pembeli tidak tersimpan. Lima pagar sekarang berlaku; setiap pagar punya tes dari teks insiden di `ai/llm_first_incident_test.go` dan `internal/salesbrain/*_test.go`.

| # | Gejala di produksi | Pagar |
|---|--------------------|-------|
| 1 | "Jl. Taman Setiabudi II No. 28, South Jakarta, DKI Jakarta, 10197" ditolak: model mengisi `district` "South Jakarta", `city` "DKI Jakarta", `province` kosong, sehingga `ShippingComplete` false dan pembeli mengulang alamat dua kali. | `applyFieldAnswer` menjalankan `bf.MergeShippingText` lebih dulu pada salinan state (jalan, kota, provinsi, kode pos, nama, HP). Field model hanya mengisi yang masih kosong. Bila `city` model ternyata nama provinsi atau alias (`bf.CanonicalProvince`), ia dibaca sebagai provinsi dan `district` menggantikan kota yang kosong. `district` yang sama dengan kota yang dikenali parser diabaikan. Street dari parser yang hanya menyalin seluruh pesan, atau potongan sebelum nama kota yang tidak mirip jalan, tidak diambil. |
| 2 | Balasan model "alamat sudah diperbarui" / "pesanan siap dibayar" padahal data belum lengkap. | `lfApplyFields`: selama kolom tertunda belum lengkap (bukan `confirm`), balasan model dibuang dan diganti balasan kode dalam bahasa terkunci: apa yang tersimpan (`TplFieldSaved`), data tidak valid, apa tepatnya yang kurang (`TplFieldMissing`), lalu kalimat lanjut. Balasan model hanya dipakai bila data benar-benar lengkap, diikuti ringkasan kode dan permintaan OK. `lfApplyConfirm`: intent `confirm` di luar step confirm (atau data belum lengkap) menjadi kalimat lanjut untuk kolom tertunda. Metadata: `field_incomplete`, `confirm_not_ready`. |
| 3 | "Pesanan siap dibayar, transfer ke BCA/Mandiri/BNI, kirim bukti transfer" sebelum ada alamat dan konfirmasi. | `salesbrain.Validate` menambah kode `premature_payment`: balasan memuat nomor rekening dari FAQ atau frasa transfer / payment proof / bukti transfer / ready for payment / siap dibayar / proceed with the payment, sementara tidak ada pesanan menunggu bayar, pesan pembeli tidak bertanya soal pembayaran (bayar, transfer, rekening, payment, pay, QRIS, account), dan intent bukan `confirm` di step confirm. Memicu retry normal dengan `ViolationNote`; gagal dua kali berarti fallback ke alur lama. Persona juga melarangnya. |
| 4 | Pukul 00:18 WIB model bilang "sudah lewat jam 12:00, dikirim hari kerja berikutnya". | Kode menulis `pengiriman_jika_bayar_sekarang` di STATE (`salesbrain.ShipIfPaidNow`): sebelum 08:00 "hari ini bila dibayar antara 08:00-12:00", 08:00-12:00 "hari ini", setelahnya atau di akhir pekan "hari kerja berikutnya". Jendela dibaca dari FAQ tenant (rentang "08:00 hingga 12:00" atau "cut off ... 14.00" pada jawaban tentang pengiriman); bila tidak terbaca, baris dihilangkan. Persona menyuruh model memakai nilai itu, bukan menghitung jam sendiri. Libur nasional tidak dihitung. |
| 5 | Hampir setiap balasan ditutup "Mau ditambahkan ke pesanan?". | Persona: tawaran menambah pesanan paling banyak sekali per topik, jangan menutup tiap jawaban dengan tawaran; satu ajakan di akhir hanya bila perlu. Hanya dijaga tes teks prompt, bukan validator. |

Catatan perilaku: ketika parser mengenali kota (mis. "Surabaya", "South Jakarta"), nama kota dan provinsi memakai bentuk kanonik parser ("Jakarta Selatan", "DKI Jakarta"), bukan huruf model. Label "Address:" di awal baris alamat satu baris dibuang oleh `parseCommaAddress`.

## 11.1 Insiden 21 Okt 2026

Tenant omah_apparel, mode `on`, 21 Okt 2026 sekitar 19:40-19:43 WIB (revisi Cloud Run dengan semua PR terbaru). Pembeli berbahasa Inggris punya tiga pesanan: WB-BBDB5FD4 (`draft`, belum dibayar, terakhir diubah 18 Okt), WB-7D23AE49 (belum dibayar, dari 18 Okt, belum dibatalkan), dan WB-79D3E7E5 (baru, dibuat 19:20).

| Waktu | Pesan pembeli | Yang terjadi | Penyebab |
|-------|---------------|--------------|----------|
| 19:40:53 | "i want cancel all my pending orders" | Hanya WB-79D3E7E5 dibatalkan ("Done, order WB-79D3E7E5 is cancelled…", `replacedReason = cancel_template`). | `SelectOpenOrders` hanya meloloskan draft berumur kurang dari 72 jam, jadi `OpenOrders` berisi satu pesanan dan `lfPickCancelTarget` memilihnya; tidak ada konsep "semua". |
| 19:41:15 | "do i have pending orders ?" | Model: "You currently have no pending orders with us." SALAH. | STATE `pesanan_terbuka` = "tidak ada" karena daftar yang sama dipakai untuk menjawab dan untuk pengingat. |
| 19:41:47 | "how about this one WB-BBDB5FD4 ?" | Balasan status deterministik: "awaiting shop confirmation". | Jalur `status` membaca DB langsung, bertentangan dengan jawaban model sebelumnya. |
| 19:42:06 | "you say i dont have any pending orders" | Model takeover (bingung). | Akibat dua baris di atas. |
| 19:43:40 | "i want cancel all my pending orders" (sesaat setelah mengetik WB-7D23AE49) | WB-7D23AE49 dibatalkan, tetapi model juga mengirim `handoff {level: takeover, reason: "Customer repeatedly requested to cancel orders that are not present in the current system state."}`; balasan = template batal + "They usually reply during opening hours (08:00 - 19:00 GMT +7)." dan percakapan menjadi `ai_handled=false` (AI berhenti) padahal aksi berhasil. | `lfApplyHandoff` memberlakukan takeover tanpa melihat bahwa aksi giliran itu sudah berhasil. |
| semua pemicu | (log peringkas) | "summarize publish skipped: inflight" / "summarize skipped: already inflight"; tidak ada ringkasan baru sejak perbaikan push. | Kunci inflight diambil dua kali (publisher lalu handler). |

Empat perbaikan (PR `fix/llm-first-open-orders-cancel`, tes di `ai/llm_first_openorders_test.go`, `internal/salesbrain/*_test.go`, `ai/memory_inflight_test.go`):

1. Daftar pesanan belum selesai tanpa batas umur untuk menjawab; aturan 72 jam / 3 pesanan hanya untuk pengingat (§1.3a).
2. `cancel_scope = all`: daftar + konfirmasi + pembatalan seluruhnya; tanpa nomor dan dengan beberapa pesanan, pembeli diberi daftar pilihan, tidak pernah ditebak (§1.4).
3. Takeover setelah aksi berhasil menjadi note; AI tetap aktif (§1.5).
4. Peringkas berjalan: publisher memiliki kunci inflight, handler melepasnya (§1.3).

Tidak ada perubahan env, secret, atau infrastruktur. Bukti di atas hanya memuat nomor pesanan; tidak ada nomor telepon atau data pribadi.

---

## 12. Latensi balasan

### Baseline terukur (tenant omah_apparel, 3 hari, pesan masuk sampai balasan keluar)

| Jalur | p50 | p90 | Maks |
|---|---|---|---|
| `llm_first` | 3,4 detik | 5,7 detik | 15,8 detik |
| legacy `llm` | 7,1 detik | - | - |

Outlier: "Halo" 15,8 detik (pesan pertama pagi, cold start) dan "do you sell airpods ?" 14,9 detik. Sumber: pengukuran produksi dari owner, bukan dari repo.

### Perubahan

1. **Debounce adaptif** (`ai/inbound_coalesce.go`, `adaptiveQuiet`, fungsi murni dengan tes tabel). Jendela diam dasar turun dari 3 detik ke **1,2 detik**. Jendela naik ke 3 detik hanya bila pesan terakhir tampak belum selesai:
   - berakhir `,` `:` `-` atau kata `dan` / `and`;
   - sapaan tunggal (`halo`, `hi kak`, `kak`, `selamat pagi`, ...) yang biasanya disusul pertanyaan;
   - sangat pendek (3 karakter atau kurang);
   - burst berjalan: pesan ke-2 atau lebih dalam 2 detik dari pesan sebelumnya.

   Batas tunggu total turun dari 15 ke **8 detik**. Awal burst di-reset bila jeda antar pesan lebih dari 3 detik (sebelumnya kunci burst bisa basi selama chat aktif). Watermark dan pembuangan balasan yang disusul tidak berubah.
2. **Tanda dibaca dan indikator mengetik** (`ai/inbound_ack.go`, `whatsapp.MarkReadWithTyping`). Begitu job AI menerima pesan, panggilan async best-effort ke Cloud API (timeout 4 detik, hanya log) menandai pesan dibaca dan menampilkan "sedang mengetik" (hilang saat kita membalas atau setelah 25 detik). Dilewati untuk simulator dan percakapan yang tidak dijawab AI. Sumber format: https://developers.facebook.com/docs/whatsapp/cloud-api/typing-indicators
3. **Pencatatan latensi**: `message.metadata` balasan AI kini memuat `latencyMs` (created_at pesan masuk sampai awal pengiriman) dan `coalesceWaitMs` (bagian yang dihabiskan di jendela diam).
4. **Cloud Run**: `--cpu-boost` di `deploy.sh`. `--min-instances` dan CPU throttling tidak diubah; lihat `docs/DEPLOY_DUAL_ENV.md` bagian 7.

### SQL: p50/p90 per jalur

Ganti `<tenant_schema>`. Hanya membaca; baris lama sebelum rilis ini tidak punya `latencyMs`.

```sql
SELECT COALESCE(metadata->>'pipeline', 'legacy') || '/' || COALESCE(metadata->>'path', '?') AS jalur,
       count(*)                                                             AS balasan,
       percentile_cont(0.5) WITHIN GROUP (ORDER BY (metadata->>'latencyMs')::numeric) / 1000 AS p50_detik,
       percentile_cont(0.9) WITHIN GROUP (ORDER BY (metadata->>'latencyMs')::numeric) / 1000 AS p90_detik,
       max((metadata->>'latencyMs')::numeric) / 1000                        AS maks_detik,
       avg((metadata->>'coalesceWaitMs')::numeric) / 1000                   AS rata_tunggu_diam_detik
FROM "<tenant_schema>".message
WHERE direction = 'out' AND author IN ('ai', 'system')
  AND metadata ? 'latencyMs'
  AND created_at > now() - interval '3 days'
GROUP BY 1
ORDER BY balasan DESC;
```

### Praktik terbaik

- Ukur dulu (SQL di atas), ubah satu hal per rilis, bandingkan p50 dan p90, bukan rata-rata.
- Jendela diam adalah harga koalesensi: makin pendek makin cepat tetapi makin sering memecah satu pikiran buyer menjadi dua balasan. Naikkan lagi bila `coalesceWaitMs` rendah tetapi keluhan balasan terpotong naik.
- Beri umpan balik instan (tanda dibaca + mengetik) agar waktu tunggu terasa lebih pendek.
- Outlier 15 detik hampir selalu cold start atau CPU di-throttle setelah respons, bukan LLM; perbaikannya di setelan Cloud Run (owner), bukan di kode.
