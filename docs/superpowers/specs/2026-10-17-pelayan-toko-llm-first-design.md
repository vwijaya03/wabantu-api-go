# Pelayan Toko LLM-first untuk AI WhatsApp — desain

Tanggal: 2026-10-17 · Pemilik keputusan: Viko · Status: menunggu review spec

## 1. Latar dan tujuan

Insiden tenant `omah_apparel` (16–17 Okt) menunjukkan:
- 74 dari 98 balasan otomatis dibuat ±25 interceptor regex di `ai/autoreply.go` `ProcessAutoReply`, sebelum LLM sempat dipanggil.
- 8 dari 24 balasan LLM diganti kalimat baku Indonesia oleh validator harga.
- LLM tidak pernah melihat deskripsi produk.
- Bahasa pembeli berganti sendiri.
- AI di-pause sampai 6 jam tanpa notifikasi.

Tujuan:
1. AI bersikap seperti pelayan toko: menjawab yang ditanya dari katalog (termasuk deskripsi) dan KB, lalu memandu sampai pesanan selesai.
2. Biaya rendah: target ≤ Rp20 per pesan dan ≤ Rp250 per percakapan untuk tenant kecil.
3. **Kunci bahasa.** Bahasa pembeli diketahui setiap giliran. Begitu terkunci, bahasa tidak berganti tanpa permintaan eksplisit pembeli.
4. **Kesadaran pesanan terbuka.** Pembeli yang punya draft atau pesanan yang belum dibayar diberi tahu, lalu ditanya mau lanjut, buat baru, atau batal.
5. Semua bug temuan investigasi diperbaiki, termasuk di jalur lama, karena jalur lama tetap menjadi fallback.

Non-tujuan hari ini:
- menghapus kode legacy;
- meter kuota "balasan AI";
- push WhatsApp ke owner;
- caching eksplisit Gemini;
- arsitektur classifier→responder (B2);
- bahasa selain ID/EN.

## 2. Keputusan (default yang diambil; Viko boleh koreksi)

| # | Keputusan | Pilihan |
|---|---|---|
| D1 | Urutan | Semua dikerjakan hari ini di belakang flag per tenant. Perbaikan bug legacy aktif tanpa flag. |
| D2 | Kuota | **Tidak diubah di pekerjaan ini; didiskusikan terpisah.** Catatan: LLM-first memakai ±5× token per pesan, jadi tenant Trial/Starter lebih cepat habis kuota (Trial ±32 pesan/bulan) bila flag diaktifkan untuk mereka. |
| D3 | Model | Chain `chat` disarankan `gemini-3.1-flash-lite` → `gemini-3.6-flash` (diatur superadmin di AI Provider). Anthropic hanya fallback. Family 2.5 sudah 404 untuk key baru (`shared/llm/gemini.go:24`). |
| D4 | Pesanan terbuka | Hanya draft yang belum selesai dan pesanan menunggu bayar, berumur ≤ 72 jam. Pesanan paid/processing/shipped/completed hanya disebut bila ditanya. Pengingat maksimal 1 kali per sesi (sesi baru = jeda ≥ 30 menit) dan maksimal 2 kali proaktif per pesanan. "Pesanan baru" tidak membatalkan yang lama. |
| D5 | Bahasa | Diinfer dari pesan jelas pertama. Bot bertanya hanya bila ambigu, tanpa opsi angka. Default tenant `id`. Ganti hanya atas permintaan eksplisit. |
| D6 | Handoff | Default soft (owner diberi notifikasi in-app, AI tetap melayani). Hard pause hanya untuk permintaan manusia, komplain berat, atau salah paham 2× berturut-turut; auto-resume 2 jam tanpa balasan staf. Kegagalan sistem tidak pernah memicu pause. |
| D7 | Privasi/log | Blok state di prompt tidak menggemakan nomor HP atau alamat tersimpan (cukup flag "sudah ada"). Ringkasan keputusan ≤2 KB disimpan di `message.metadata`, termasuk output mentah terpotong dan diredaksi. Tanpa tabel baru. |
| D8 | Persona | Pelayan Toko diringkas (±1.450 token) dengan mode "ringan": langsung bertindak bila produk dan qty jelas. Nama asisten: "asisten {nama toko}". |
| D9 | Ukuran | North Star inisiatif = Correct-Answer Rate. Guardrail: bahasa salah ≤1%, diam tanpa batas = 0, penggantian balasan ≤2%, biaya ≤ Rp20 per pesan. |

## 3. Arsitektur

### 3.1 Alur satu giliran (flag `ai_llm_first` aktif untuk tenant)

```
inbound (setelah debounce yang ada)
 → gerbang lama yang tetap: dedupe, AI-off, media/bukti transfer, profil belum lengkap
 → auto-resume pause bila TTL lewat (§4.4)
 → salesbrain.DecideLanguage   (kode; kunci bahasa)
 → muat state: keranjang Redis + draft DB + pesanan terbuka + pengingat (Redis)
 → salesbrain.BuildPrompt      (prefix stabil: persona → profil → katalog+deskripsi → KB; lalu STATE + riwayat 6)
 → 1 panggilan LLM, JSON mode, maxOutputTokens 300, temperature 0.3
 → salesbrain.ParseDecision + salesbrain.Validate
        gagal → ulang 1× dengan catatan pelanggaran → gagal lagi → fallback ke jalur lama (fail-open)
 → adapter pesanan (kode): terapkan intent/items ke OrderState; ringkasan dan total dirender kode
 → kirim (tolak body kosong) + metadata keputusan
```

Fail-open berarti setiap error sebelum efek samping (state belum disentuh, belum terkirim) mengembalikan giliran ke `ProcessAutoReply` lama. Tidak ada pause.

### 3.2 Unit

**`internal/salesbrain/`** — Go murni, tanpa Encore, bisa dites dengan `go test`.

| File | Tanggung jawab | API inti |
|---|---|---|
| `types.go` | Tipe bersama | `Lang`, `Intent`, `TurnState`, `Decision`, `ItemOp`, `Handoff`, `OpenOrder` |
| `language.go` | Kunci bahasa (§4.1) | `DecideLanguage(saved string, tenantDefault Lang, msg string, awaitingChoice bool) LanguageDecision`, `ParseExplicitSwitch`, `ClearLanguage` |
| `openorder.go` | Pesanan terbuka dan pengingat (§4.2) | `SelectOpenOrders(snaps, now)`, `ReminderAllowed(rs, now, lastBuyerAt, ctx)`, `DescribeOrder(o, lang)` |
| `prompt.go` | Persona ringkas + kontrak keluaran | `BuildPrompt(PromptInput) (system, user string)` dengan blok statis di depan |
| `context.go` | Blok katalog (nama, harga tier, stok, deskripsi ≤160 karakter) dan KB | Mode `full` bila estimasi ≤5.000 token, selain itu `topk` (12 produk dari retrieval yang sudah ada, KB top-8 + kategori pembayaran/pengiriman/retur selalu ikut) |
| `output.go` | Parse JSON keputusan | `ParseDecision(raw) (Decision, error)`; skema `{"reply","intent","items":[{"no","qty"}],"language_switch","handoff":{"level","reason"}}` |
| `validate.go` | Validator v2 | Lihat di bawah tabel |
| `templates.go` | Teks sistem/transaksi | Semua dalam ID+EN; tes enumerasi gagal bila salah satu kosong |

Aturan `Validate(Decision, Facts, Lang) []Violation`:
- Setiap nominal Rp harus ada di himpunan izin: harga katalog, baris dan total keranjang, nominal di KB, dan qty × harga.
- Setiap nomor rekening harus ada di KB.
- Balasan tidak boleh kosong, maksimal 700 karakter.
- Tidak boleh memuat kode internal katalog.
- Bahasa balasan harus sama dengan bahasa terkunci (heuristik kata fungsi ID/EN).

**`ai/` (Encore)**
- `llm_first.go`: orkestrator, dengan hook ≤10 baris di awal `ProcessAutoReply`.
- `llm_first_orders.go`: adapter `Decision` → operasi `OrderState` lewat fungsi buyerflow/order yang sudah ada.
- `llm_first_meta.go`: metadata keputusan.

**`shared/llm/`**
- Chat mendukung `responseMimeType: application/json`, `temperature`, dan `maxOutputTokens`.
- `finishReason` serta `thoughtsTokenCount` dan `cachedContentTokenCount` diparse ke usage.

### 3.3 Data dan konfigurasi (tanpa migrasi skema, tanpa secret baru)

- **Bahasa:** `contact.preferred_language` diisi hanya saat terkunci (pesan jelas atau pilihan eksplisit). Kosong = belum terkunci, sehingga tebakan default dipakai tanpa ditulis. `savedBuyerLangStack` global diganti parameter bahasa eksplisit di builder buyerflow yang dipakai jalur baru.
- **Pengingat pesanan terbuka:** di Redis `ai:openorder:remind:{tenant}:{convo}` dengan field `lastAt`, `count`, `orderRef`, TTL 72 jam.
- **Pause:** `conversation.ai_paused_at` dan `handoff_reason` yang sudah ada. Auto-resume dihitung saat giliran mulai.
- **Flag** (memakai `flag.IsEnabled`, cache 60 detik):
  - `ai_llm_first` per tenant;
  - `ai_llm_first_shadow` per tenant, menjalankan pipeline baru secara dry-run setelah balasan lama dan hanya mencatat log;
  - `ai_llm_first_kill` global, mengalahkan semua.
- **Metadata per balasan:** `pipeline`, `authoredBy`, `intent`, `lang{locked,reply,switch}`, `replaced{reason}`, `llm{model,attempts,inTok,outTok,thoughtTok,latencyMs,finishReason}`, `rawOutput` (≤1 KB, diredaksi), `handoff{mode}`. Field lama `path`, `reason`, dan `llmUsed` tetap diisi demi kompatibilitas dashboard.

## 4. Aturan perilaku

### 4.1 Kunci bahasa
- **Belum terkunci:**
  - pesan jelas satu bahasa (≥4 kata isi, ≥70% satu bahasa) → kunci;
  - salam pendek khas Indonesia → pakai `id` tanpa mengunci;
  - salam ambigu ("hi", "hello", "Helllo") → satu bubble dua bahasa yang menanyakan bahasa (tanpa angka);
  - jawaban "indo/indonesia/bahasa" → `id`; "english/inggris/en" → `en`.
- **Ganti eksplisit:**
  - harus ada nama bahasa bersama kata permintaan ("please reply in…", "pakai…", "switch to…", "bisa english?") di klausa yang sama;
  - pernyataan tidak mampu ("can't read/speak X", "gak ngerti bahasa X") → ganti ke bahasa lainnya;
  - apostrof `’` disamakan dengan `'`.
- **Tidak pernah mengganti bahasa:**
  - alamat dan nama tempat ("indonesia");
  - data terstruktur (Name:/Phone:/Address:);
  - pesan campuran, satu kata, typo, pesan marah;
  - angka yang tidak sedang ditunggu sebagai jawaban pilihan bahasa;
  - nama bahasa di dalam kutipan atau nama produk.
- **Pembeli menulis bahasa lain tanpa meminta:**
  - tetap balas bahasa terkunci;
  - setelah 2 pesan jelas berturut-turut, tawarkan ganti sekali per kontak.
- **Konfirmasi ganti:** satu kalimat dalam bahasa baru, lalu lanjutkan topik.
- **Template:** semua template (sistem, transaksi, handoff, kuota, fallback) tersedia dalam ID dan EN dan dipilih dari bahasa terkunci.

### 4.2 Pesanan terbuka
- Daftar dan rendering diambil dari DB (draft + menunggu bayar). Keranjang Redis hanyalah cache.
- **Sapaan tidak lagi menghapus keranjang** (sekarang menghapus di `ai/autoreply.go:293`).
- `boleh_ingatkan` dihitung kode dan bernilai benar bila semua syarat terpenuhi:
  - ada pesanan terbuka;
  - jeda ≥30 menit dari pesan pembeli sebelumnya;
  - belum diingatkan di sesi ini;
  - pengingat proaktif untuk pesanan itu kurang dari 2;
  - pembeli tidak sedang mengisi kolom checkout, tidak komplain, dan tidak sedang handoff.
- Isi pengingat: nomor pesanan, isi (maksimal 3 item), total (dengan catatan "belum termasuk ongkir"), bagian yang masih kurang, dan tawaran lanjut / pesanan baru / batal.
  - Bila pembeli bertanya hal lain, jawab dulu lalu tambahkan satu kalimat pengingat.
- Tindakan per jawaban pembeli:
  - **lanjut:** keranjang dipulihkan, lalu tanyakan hanya kolom yang tertunda.
  - **baru:** draft lama diparkir, lalu keranjang baru dibuat.
  - **batal:** draft yang belum dibayar dibatalkan; pesanan yang sudah dibayar diteruskan ke owner.
  - **≥2 pesanan terbuka:** tampilkan daftar bernomor (maksimal 3) dan minta pembeli memilih.
- Niat order baru dengan item, sementara ada draft: tanya dulu apakah ditambahkan ke draft atau dibuat terpisah. Pengecualian: kata tambah eksplisit ("tambah", "add") langsung menambah.

### 4.3 Melayani
- **Jawab dulu yang ditanya.** Atribut produk diambil dari deskripsi. Bila deskripsi produk bertentangan dengan profil toko, deskripsi produk yang menang.
- **Pertanyaan di tengah checkout:** jawab, lalu tutup dengan `kalimat_lanjut` untuk kolom yang tertunda; kode menambahkannya bila LLM lupa.
  - Kalimat tanya atau data tidak valid tidak pernah disimpan sebagai nama atau alamat.
  - Kesalahan data disebut persis (jumlah digit, huruf).
- **Bertanya tentang produk ≠ membeli.** Item hanya berubah bila `intent ∈ {order, amend}`.
  - Item ambigu (dua varian) ditanyakan khusus item itu; item lain dipertahankan.
  - Daftar multi-baris digabung ke keranjang, tidak pernah menghapus keranjang.
- **Pembatalan:** `cancel` dievaluasi sebelum `amend`. Menyebut item berarti menghapus baris itu; tanpa item berarti seluruh pesanan.
- **Bayar:** metode dan rekening hanya dari KB. Bot tidak pernah bilang "lunas" sebelum status dari tool menyatakannya.
- **Kirim:** aturan jam kirim dari KB digabung dengan jam saat ini (zona waktu tenant). Bot tidak pernah menjanjikan tanggal tiba.
- **Upsell:** maksimal 1 per pesanan, 1 produk, relevan, dan tidak ditawarkan saat komplain, saat pembeli mengisi kolom, atau setelah info bayar dikirim.
- **Pesan pendek atau typo** ("Helllo", "hmm", "yes"): dianggap salam atau jawaban atas pertanyaan terakhir. Tidak pernah memicu handoff pada kejadian pertama.

### 4.4 Handoff
- **Soft** (data tidak ada, nego harga, pertanyaan di luar KB): kirim notifikasi `ai_handoff` ke owner via `notification.CreateForAccounts`, berisi ringkasan 2 baris dan tautan. AI tetap melayani. Tidak ada notifikasi ganda untuk topik yang sama dalam 1 jam.
- **Hard** (minta manusia / admin / owner / administrator / CS / staf, komplain barang/refund, salah paham 2× berturut-turut):
  - kirim pesan jujur berbahasa terkunci, berisi info jam buka dari `opening_hours`;
  - kirim notifikasi;
  - pause dengan auto-resume 2 jam bila staf belum membalas. Saat resume, kirim satu kalimat permintaan maaf menunggu.
- **Kegagalan sistem** (LLM error, JSON rusak setelah retry, kuota): balasan jujur berbahasa terkunci ditambah notifikasi. Tidak pause.

## 5. Perbaikan jalur lama (aktif untuk semua tenant, tanpa flag)

| ID | Perbaikan |
|---|---|
| B1 | `description` dimuat di `loadActiveCatalog`/`loadCatalogItemsByIDs` dan `CatalogItem`, lalu tampil di `BuildCatalogContext` (≤160 karakter) dan `get_product`. `search_catalog` juga mencari di deskripsi. |
| B2 | `groundLLMReply` tidak lagi mengganti dengan kalimat baku Indonesia. Gantinya: retry/fallback berbahasa pembeli, reason disimpan di metadata, dan validator menerima jumlah qty × harga serta nominal KB. Pesan "not found" dari tool menjadi JSON netral bahasa. |
| B3 | Opsi bahasa mengikuti teks prompt (1 = Indonesia, 2 = English). Apostrof dinormalisasi. Negasi dibatasi klausa. Kata "indonesia" tanpa kata permintaan tidak mengganti bahasa. Bila `preferred_language` terisi, deteksi per pesan dimatikan. |
| B4 | Gerbang pertanyaan di langkah recipient/address: teks bertanda tanya atau berkata tanya ID/EN tidak dijadikan nama; pesan itu dijawab dulu lalu kolom ditanya ulang. Kalimat tanya yang menyebut produk tidak menambah keranjang. |
| B5 | `out_of_scope` diteruskan ke LLM, bukan handoff + pause. Pause hanya pada handoff hard dengan auto-resume. Notifikasi owner. Teks handoff dilokalkan. |
| B6 | Cancel seluruh pesanan dicek sebelum amend ("cancel my previous order", "batalkan pesanan"). Cancel tidak pernah menambah item dari riwayat. |
| B7 | Penjaga body kosong di `sendAiMessage`. Bila jawaban KB terlokalisasi kosong, `tryPaymentFAQAnswer` jatuh ke langkah berikutnya. |
| B8 | `isStructuredOrderCheckoutBreak` hanya berlaku bila ada keranjang. Daftar terstruktur digabung ke keranjang, tidak menghapusnya. |
| O1 | Upsell maksimal 1 saran, 1 kali per pesanan. |
| O2 | Digit di langkah HP/alamat tidak dibaca sebagai revisi qty. |
| O3 | Frasa minta manusia diperluas (administrator, owner, pemilik, manager, staff, person, human). |
| O4 | Salam waktu memakai zona waktu tenant dan kata waktu di pesan ("morning"); "good night" dipetakan ke malam. |
| O5 | Riwayat ke LLM tidak menduplikasi pesan saat ini. Hint tool digabung, bukan saling menimpa. |
| O6 | Blok KB: bila KB tenant ≤30 baris, kirim semua; jangan menulis "belum ada" bila ada baris. |
| O7 | Port `RETRIEVAL_BACKGROUND_DRAIN=1` (commit `ed7e18f`) ke `master` supaya `deploy.sh` kedua branch identik. |

Yang tidak dikerjakan hari ini dan dicatat sebagai sisa:
- parser alamat berbasis tabel 15 kota (diganti LLM extraction di jalur baru, sedangkan jalur lama hanya dilonggarkan);
- validasi HP luar negeri penuh;
- kedaluwarsa otomatis draft lama.

## 6. Rilis, keamanan, dan env

- **Branch:** `feat/pelayan-toko-llm-first` dari `master` lalu PR ke `master`. Pasangannya `feat/pelayan-toko-llm-first-cr` dari `origin/release-cr` (cherry-pick) lalu PR ke `release-cr`. Isi kode kedua branch saat ini identik kecuali `scripts/cloudrun/deploy.sh` dan `deploy/cloudrun/README.md`.
- **Env:**
  - Tidak ada secret atau env baru.
  - `GeminiAPIKey` hanya ada di Cloud Run. Di Encore Cloud, chain jatuh ke Anthropic dan JSON mode digantikan instruksi prompt + parse. Bila parse gagal → fail-open ke legacy.
  - `INLINE_INBOUND_AI` dan `RETRIEVAL_BACKGROUND_DRAIN` tetap hanya di Cloud Run.
- **Default flag mati:**
  - Merge tidak mengubah perilaku jalur baru. Perbaikan §5 langsung aktif.
  - Aktivasi untuk Omah dilakukan lewat admin flags (keputusan Viko).
  - Kill switch `ai_llm_first_kill` mematikan jalur baru global dalam ≤60 detik.
- **Kebocoran:** tidak ada nilai secret di kode, log, atau test; output mentah LLM di metadata diredaksi (HP/email) dan dipotong.
- **Deploy:** pekerjaan ini berakhir di PR (tanpa merge). Merge ke `master` men-deploy Encore Cloud; Cloud Run di-deploy manual dari `release-cr` lewat `scripts/cloudrun/deploy.sh`. Keduanya hanya setelah Viko setuju.

## 7. Pengujian

- **L1 (`go test`, tiap PR):** `internal/salesbrain` diuji dengan tabel kasus dari seluruh 34 gejala insiden.
  - Bahasa: semua kalimat insiden, termasuk `’`.
  - Pengingat pesanan terbuka.
  - Parser keputusan (JSON rusak, field hilang, item di luar katalog).
  - Validator (Rp total keranjang lolos, Rp karangan gagal, rekening karangan gagal, balasan kosong).
  - Kelengkapan template ID/EN.
  - Ukuran konteks full/topk.
- **L2 (`go test`, replay):** orkestrator diuji dengan LLM tiruan yang mengembalikan JSON terekam untuk 12 transkrip insiden. Diperiksa: state keranjang, bahasa, handoff, dan fail-open saat JSON rusak atau timeout.
- **Regresi:** test `internal/buyerflow`, `shared/llm`, `shared/retrieval` yang ada harus tetap hijau; paket `ai` lewat `encore test` bila infrastruktur lokal tersedia.
- **L3 (opsional, butuh izin Viko karena memakai key dan biaya ±US$1):** smoke live 12 transkrip insiden ke `gemini-3.1-flash-lite`.

## 8. Risiko

| Risiko | Mitigasi |
|---|---|
| Model flash-lite salah intent pada pesan campur/slang | Validator + retry; fail-open; kill switch; tes replay |
| Kuota habis lebih cepat | Kuota didiskusikan terpisah (D2); flag diaktifkan dulu hanya untuk tenant pilot; pesan kuota habis dilokalkan |
| Konflik dua branch | Cherry-pick per commit; diff isi dibandingkan setelahnya; deploy.sh disamakan |
| Encore Cloud tanpa Gemini | Fallback Anthropic + parse JSON; fail-open ke legacy |
| Data tenant kontradiktif ("fresh" vs "frozen", KB ganda) | Aturan "deskripsi produk menang"; daftar perbaikan data untuk owner (§9) |

## 9. Perbaikan data Omah (dikerjakan owner lewat dashboard)

1. Deskripsi profil: ganti "fresh seafood / fresh catches" menjadi frozen dan vacuum-sealed.
2. `base_pricing`: isi harga atau kebijakan harga, bukan "Transfer Bank dan QR".
3. `products_services`: sebut juga salmon dan beef.
4. KB:
   - hapus entri metode bayar yang tanpa nomor rekening (`c7a1b49a`);
   - gabungkan dua entri jam kirim menjadi satu: kirim hari itu bila bayar pukul 08:00–12:00 WIB, sampai 3–4 hari via J&T/JNE;
   - hapus salah satu entri minimum order.
