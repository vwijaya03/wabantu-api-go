# Shipped: Stok guard AI per gudang (Fase 4)

**Status:** Siap merge  
**Branch:** `feat/ai-stock-guard`  
**Tanggal:** 2026-06  
**Roadmap terkait:** [`docs/WHATSAPP_INBOX_MEDIA_PAYMENT_STOCK.md`](../docs/WHATSAPP_INBOX_MEDIA_PAYMENT_STOCK.md) — Fase 4

---

## Apa yang di-ship

AI WhatsApp menampilkan **stok available per gudang** (`on_hand - reserved`), menolak qty jika **tidak ada satu gudang** yang cukup, dan menyimpan `warehouseId` pada draft order.

---

## Label gudang ke pembeli

| Prioritas | Sumber |
|-----------|--------|
| 1 | `inv_warehouse.customer_label` (opsional, diisi owner di halaman Gudang) |
| 2 | `inv_warehouse.name` apa adanya |

Tidak pernah menampilkan `code` internal.

---

## Perilaku runtime

| Situasi | Perilaku |
|---------|----------|
| Tanya stok | Breakdown per gudang + total (jika >1 gudang) |
| Order qty | Auto-assign gudang default dulu, lalu gudang lain by `display_order` |
| Qty > stok satu gudang | Tolak + tampilkan breakdown per gudang |
| `persistDraftOrder` | Set `items[].warehouseId` + precheck DB |

---

## File kunci

| File | Peran |
|------|--------|
| `shared/tenantschema/inventory_schema_sql.go` | Kolom `customer_label` |
| `tenant/schema_patch_inventory.go` | Patch kolom |
| `inventory/inventory.go` | API warehouse CRUD |
| `ai/order_catalog.go` | `enrichCatalogStock` per gudang |
| `ai/order_stock_guard.go` | Guard + resolve warehouse |
| `ai/catalog_reply.go` | Format breakdown |
| `ai/order_flow.go` | `warehouseId` pada draft |

---

## Test

### Otomatis

```bash
cd api-go
encore test ./ai/ -run 'Stock|Warehouse|AdvanceOrderFlow' -count=1
```

- [x] `order_stock_guard_test.go` — multi-gudang, `customer_label`, breakdown
- [x] `order_flow` sim — qty dalam stok gudang default → `warehouseId` terisi
- [x] Tenant tanpa inventory → perilaku lama (tanpa stok)

### Manual QA

- [ ] Setup 2 gudang (mis. default stok 2 pcs, cabang stok 3 pcs) → tanya stok di WA → breakdown per gudang + total 5
- [ ] `customer_label` diisi (mis. "Surabaya") → label itu tampil di chat; kosong → pakai `name` gudang
- [ ] Order qty 5 → ditolak (meski total 5); qty 2 → lolos, draft punya `warehouseId` gudang default
- [ ] Halaman Gudang (FE): tambah/edit label pelanggan → tersimpan dan dipakai di chat

---

## Backlog

- Opsi E: pilih gudang by alamat pengiriman (heuristik)
- Bundle stock (`is_bundle = true`)
