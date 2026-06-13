# Order Ownership — Security Research & 100 Unit Tests

Insiden: pembeli **Local Test** (`6281292066606`) membatalkan pesanan **WB-EAA94534** milik contact lain (**The Ngiek Ing** / `+628888339455`) lewat chat WhatsApp.

**Tindakan ini tidak valid** — cancel/status harus terikat ke **contact_id pemilik order**, bukan hanya `conversation_id`.

## Cara menjalankan

```bash
cd api-go
encore test ./ai/ -run TestOrderOwnership100 -count=1
encore test ./ai/ -count=1
```

---

## Root cause

| Lapisan | Sebelum | Masalah |
|---------|---------|---------|
| SQL lookup | `WHERE conversation_id = $1` | Order dengan `contact_id` berbeda masih ketemu jika conversation sama atau order pernah di-reassign di dashboard |
| Cancel UPDATE | `WHERE id = $1` saja | Siapa pun di chat bisa cancel asal tahu WB-XXXX |
| Phone | Tidak dicek | Nomor HP pengirim tidak diverifikasi |

**Skenario produksi:** Order dibuat dari chat Local Test → admin ubah contact di dashboard ke The Ngiek Ing → `conversation_id` tetap milik Local Test → Local Test masih bisa `batalkan WB-EAA94534`.

---

## Fix

1. **`orderAccessScope`** — `{ConversationID, ContactID}` dari conversation + contact WhatsApp pengirim.
2. **`sqlOrderOwnerFilter`** — semua query chat order:
   ```sql
   AND (
     (contact_id IS NOT NULL AND contact_id = $contact)
     OR (contact_id IS NULL AND conversation_id = $convo)  -- legacy
   )
   ```
3. **`loadOrderByRefForContact`** — jika ref ada di conversation tapi `contact_id` beda → `AccessDenied` (bukan cancel).
4. **`cancelPersistedOrder`** — UPDATE dengan guard `conversation_id` + owner filter.
5. **`OrderChatAccessAllowed`** — verifikasi phone pengirim vs contact order (defense in depth).

File: `order_ownership.go`, `order_customer.go`, `autoreply.go`.

---

## Model keamanan chat order

```
Inbound WA (phone P) → contact_id C → conversation_id V
                              ↓
Order cancel/status hanya jika:
  order.contact_id = C  (atau legacy null + order.conversation_id = V)
  AND optional: normalize(P) = normalize(contact.phone)
```

| Aktor | Order contact | Boleh cancel? |
|-------|---------------|---------------|
| Pemilik (same contact_id) | match | Ya |
| Local test | The Ngiek Ing | **Tidak** |
| Tahu WB-XXXX saja | bukan miliknya | **Tidak** |
| Legacy (contact_id NULL) | same conversation | Ya |

---

## Suite 100 test — `TestOrderOwnership100`

| Kategori | Count | Fokus |
|----------|-------|-------|
| `contact_scope` | 35 | OrderAccessibleByContact |
| `phone_guard` | 20 | Normalisasi nomor HP |
| `chat_access` | 15 | Combined scope + phone |
| `production_eaa94534` | 10 | Replay WB-EAA94534 |
| `adversarial_ref` | 10 | Tebak nomor WB |
| `legacy_null_contact` | 10 | Order tanpa contact_id |

File: `order_ownership_gen.go`, `order_ownership_100_test.go`.

---

## Rekomendasi operasional

1. **Jangan reassign contact order** tanpa pindah conversation — atau lock cancel chat setelah reassign.
2. Dashboard edit contact harus log audit (future).
3. CS manual cancel via dashboard tetap allowed (bukan lewat chat AI).

---

## Referensi

- Thread produksi Jun 2026, `conversation_id=7f3f02c4-6a0a-4e01-912f-0be70dde81ca`
- [ORDER_STATUS_BUYER_RESEARCH.md](./ORDER_STATUS_BUYER_RESEARCH.md)
- [ORDER_CUSTOMER_CHAT.md](./ORDER_CUSTOMER_CHAT.md)
