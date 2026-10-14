# Checklist: Simulator demo + Firebase realtime

**Status:** Catatan ops / QA lomba (master `b5cbd1a`+, PR #97–#100 / #216–#217 sudah merged).

## Env Vercel (frontend)

Empat variabel **publik** (bukan secret server). Sumber kanonik: `web-frontend/lib/firebase/config.ts` — Firebase aktif hanya jika **keempatnya** terisi.

| Env | Map ke config |
|-----|----------------|
| `NEXT_PUBLIC_FIREBASE_API_KEY` | `firebaseWebConfig.apiKey` |
| `NEXT_PUBLIC_FIREBASE_AUTH_DOMAIN` | `authDomain` |
| `NEXT_PUBLIC_FIREBASE_PROJECT_ID` | `projectId` |
| `NEXT_PUBLIC_FIREBASE_APP_ID` | `appId` |

Jika kosong → UI inbox/simulator tetap jalan dengan **polling REST**, bukan listener Firestore.

## Firestore rules

File rules ada di repo **api-go**: [`firestore.rules`](../firestore.rules) (bukan `web-frontend`). Deploy rules ke project Firebase yang sama dengan env di atas.

## Simulator backend (Encore produksi)

| Item | Detail |
|------|--------|
| `SimulatorTenantAllowlist` | Env comma-separated (ID tenant atau schema, mis. `t_omah_apparel`). **Kosong = semua 403** (fail closed). |
| Sesi | **Owner/staff** tenant demo; **`super_admin` dengan tenant terpilih** boleh (Keputusan Viko). Tanpa tenant di sesi → `PermissionDenied`. |
| `/auth/firebase-token` | **Admin platform 401**; sesi **Pantau (impersonation) 403** — lihat shipped admin-auth (#217). |
| Rate limit | 20/menit per tenant + session (Redis + in-memory fallback). |

Ref: [`docs/API_AI_CONFIG.md`](../docs/API_AI_CONFIG.md), shipped [`2026-10-13_165000_simulator-autoreply.md`](./2026-10-13_165000_simulator-autoreply.md).

## Pesanan (dashboard)

Halaman **Pesanan** (`/dashboard/orders`) **tidak** memakai realtime Firestore; daftar order di-**refresh manual** (tombol muat ulang / refetch query). Inbox boleh realtime jika Firebase configured.

## Setujui pesanan + Pantau

- `orders/page.tsx`: `readOnly={Boolean(user?.impersonation?.active)}` → tombol Setujui disabled di mode Pantau.
- `order-approve-button.tsx`: copy «Mode pantau: hanya lihat»; server `approveGate` juga 403 untuk impersonation.

## Verifikasi singkat

- [ ] Keempat `NEXT_PUBLIC_FIREBASE_*` terisi di Vercel preview/produksi demo.
- [ ] `SimulatorTenantAllowlist` memuat tenant demo di Encore **staging** (= produksi).
- [ ] Simulator POST/GET bukan 403 untuk owner demo.
- [ ] Super admin tanpa tenant → 403 simulator.
- [ ] Pantau: Setujui disabled + approve API 403.
