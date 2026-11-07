# Referensi API Radbill (`/api/v1`)

Referensi ringkas endpoint billing + portal + webhook. Bahasa: Indonesia.
Detail operasional: [OPERATIONS.md](OPERATIONS.md); keputusan desain:
[DECISIONS.md](DECISIONS.md).

Status: Fase 1–3 terdaftar penuh; endpoint Fase 4 (E1) ditandai `(E1)`.

## 1. Konvensi umum

- Basis: `http://localhost:3000/api/v1` (dev) atau domain HTTPS (prod).
- Auth panel: header `Authorization: Bearer <access_token>` (±15 menit)
  + cookie refresh httpOnly (30 hari). `POST /auth/refresh` memutar token.
- CSRF: method unsafe (`POST/PUT/PATCH/DELETE`) ke `/api/*` wajib header
  `X-CSRF-Token` dari cookie `csrf_token` (dapatkan via
  `GET /api/auth/csrf`). Pengecualian: webhook kriskris (publik).
- Portal (`/portal/*`): `customer_id` selalu dari klaim JWT, tidak pernah
  dari URL/body. Akses data orang lain → `404` (bukan 403).
- Pagination list: `?page=&per_page=` (audit-logs: halaman tetap 50,
  param `page` saja).

Envelope sukses:

```json
{ "data": { "id": "..." } }
{ "data": [ { "id": "..." } ], "total": 42 }
```

Envelope error:

```json
{ "error": { "code": "not_found", "message": "...", "field": "username" } }
```

`field` hanya ada untuk error validasi. Kode HTTP umum: `400` validasi,
`401` kredensial/sesi salah, `403` tanpa permission (atau CSRF hilang),
`404` tidak ada, `409` konflik status (mis. invoice sudah lunas,
ganti paket saat menunggak), `503` kriskris (`gobiz_offline`,
`quota_exceeded`, `duplicate_pending`).

## 2. Auth

| Method | Path | Auth | Permission | Deskripsi |
|---|---|---|---|---|
| POST | `/auth/login` | publik (rate limit) | — | Login; balas `access_token` + `user`, set cookie refresh |
| POST | `/auth/refresh` | cookie refresh | — | Putar access token |
| POST | `/auth/logout` | cookie | — | Revoke refresh + hapus cookie |
| GET | `/auth/me` | JWT | — | User + permission saat ini |
| POST | `/auth/password/reset-request` | publik | — | Minta token reset (selalu 200, anti-enumerasi) |
| POST | `/auth/password/reset` | publik + token | — | Konsumsi token reset + set password baru |
| POST | `/auth/password/change` | JWT | — (semua role) | Ganti password + revoke semua refresh |

Contoh curl login → pakai Bearer:

```bash
BASE=http://localhost:3000/api/v1
JAR=/tmp/radbill.jar

# 1) Login (butuh CSRF karena POST): ambil cookie csrf dulu
curl -s -c "$JAR" -o /dev/null "$BASE/../auth/csrf"
CSRF=$(grep csrf_token "$JAR" | awk '{print $NF}')

TOKEN=$(curl -s -b "$JAR" -c "$JAR" -X POST "$BASE/auth/login" \
  -H 'Content-Type: application/json' \
  -H "X-CSRF-Token: $CSRF" \
  -d '{"username":"admin","password":"admin123"}' \
  | python3 -c 'import sys,json; print(json.load(sys.stdin)["data"]["access_token"])')

# 2) Pakai Bearer untuk request berikut (cookies tetap dikirim)
curl -s -b "$JAR" -H "Authorization: Bearer $TOKEN" "$BASE/auth/me"

# 3) Refresh bila access token kedaluwarsa (401)
curl -s -b "$JAR" -c "$JAR" -X POST "$BASE/auth/refresh" \
  -H 'Content-Type: application/json' \
  -H "X-CSRF-Token: $(grep csrf_token "$JAR" | awk '{print $NF}')" -d '{}'
```

## 3. Admin: master data

`:id` = id UUID-ish (TEXT). List: `?q=&page=&per_page=` (kunci saring
tambahan per resource, mis. `?status=`).

| Method | Path | Auth | Permission | Deskripsi |
|---|---|---|---|---|
| GET/POST | `/admin/customers` | JWT | `customer:read` / `:write` | List + buat pelanggan |
| GET/PUT/PATCH/DELETE | `/admin/customers/:id` | JWT | `customer:read` / `:write` | Detail + ubah + hapus |
| GET/POST | `/admin/packages` | JWT | `package:read` / `:write` | List + buat paket |
| GET/PUT/PATCH/DELETE | `/admin/packages/:id` | JWT | `package:read` / `:write` | Detail + ubah + hapus |
| GET/POST | `/admin/nas` | JWT | `nas:read` / `:write` | List + buat NAS |
| GET/PUT/PATCH/DELETE | `/admin/nas/:id` | JWT | `nas:read` / `:write` | Detail + ubah + hapus |
| POST | `/admin/nas/:id/test-coa` | JWT | `nas:write` | Tes CoA ke NAS |

## 4. Admin: akun PPPoE

| Method | Path | Auth | Permission | Deskripsi |
|---|---|---|---|---|
| GET/POST | `/admin/pppoe-accounts` | JWT | `pppoe:read` / `:write` | List + buat akun |
| GET/PUT/PATCH/DELETE | `/admin/pppoe-accounts/:id` | JWT | `pppoe:read` / `:write` | Detail + ubah + hapus |
| GET | `/admin/pppoe-accounts/:id?reveal=true` | JWT | `pppoe:read` | Detail + password plaintext (**diaudit** `pppoe.password_reveal`) |
| POST | `/admin/pppoe-accounts/:id/isolate` | JWT | `pppoe:write` | Isolir + disconnect best-effort |
| POST | `/admin/pppoe-accounts/:id/activate` | JWT | `pppoe:write` | Aktifkan kembali |
| POST | `/admin/pppoe-accounts/:id/disable` | JWT | `pppoe:write` | Nonaktifkan (reject permanen) |
| POST | `/admin/pppoe-accounts/:id/extend` | JWT | `pppoe:write` | Perpanjang masa aktif |
| POST | `/admin/pppoe-accounts/:id/change-package` | JWT | `pppoe:write` | Ganti paket (admin, langsung) |
| POST | `/admin/pppoe-accounts/:id/reset-quota` | JWT | `pppoe:write` | Reset hitungan kuota periode berjalan |
| POST | `/admin/pppoe-accounts/:id/disconnect` | JWT | `pppoe:write` | Disconnect sesi via CoA |
| POST | `/admin/pppoe-accounts/:id/portal-account` | JWT | `pppoe:write` | Buat akun login portal pelanggan |
| POST | `/admin/pppoe-accounts/:id/portal-account/reset` | JWT | `pppoe:write` | Reset password portal |

## 5. Admin: sesi, invoice, plan changes

| Method | Path | Auth | Permission | Deskripsi |
|---|---|---|---|---|
| GET | `/admin/sessions?online=true` | JWT | `session:read` | Sesi online (auto-refresh UI 15 dtk) |
| GET | `/admin/sessions/history?username=&page=` | JWT | `session:read` | Riwayat sesi |
| GET/POST | `/admin/invoices` | JWT | `invoice:read` / `:write` | List + buat invoice manual |
| GET | `/admin/invoices/:id` | JWT | `invoice:read` | Detail invoice + item |
| POST | `/admin/invoices/:id/void` | JWT | `invoice:write` | Batalkan invoice |
| POST | `/admin/invoices/:id/manual-payment` | JWT | `payment:write` | Catat bayar manual (tunai/transfer) |
| GET | `/admin/plan-changes` | JWT | `pppoe:read` | List ganti-paket (termasuk pending downgrade) |

## 6. Admin: settings, scheduler, dashboard, audit

| Method | Path | Auth | Permission | Deskripsi |
|---|---|---|---|---|
| GET | `/admin/settings/` | JWT | `settings:write` | List settings (rahasia termasking `***`) |
| PUT | `/admin/settings/` | JWT | `settings:write` | Ubah settings (kirim `***` = tidak berubah) |
| GET | `/admin/scheduler/status` | JWT | `settings:write` | Status job scheduler (`last_ok`, `runs`) |
| GET | `/admin/dashboard` (E1) | JWT | admin | Agregat dasbor: `revenue_mtd`, `outstanding_total`, `overdue_count`, `accounts{active,isolated,disabled}`, `online_sessions`, `recent_payments[]` |
| GET | `/admin/metrics` (E1) | JWT | admin | `uptime_seconds`, `db_ok`, `scheduler`, `kriskris_pending`, `webhooks_failed_24h` |
| GET | `/admin/audit-logs?actor&action&from&to&page` (E1) | JWT | `audit:read` | List audit (`{data[],total,page}`; halaman 50) |

## 7. Admin: laporan (JSON + CSV)

Semua mendukung `?format=csv` (unduh file; JSON bila tanpa param).
Permission: `invoice:read`.

| Method | Path | Filter | Deskripsi |
|---|---|---|---|
| GET | `/admin/reports/revenue` | `from`, `to` (YYYY-MM-DD), `group_by=day\|month` | `{points[{period,total}], total}` |
| GET | `/admin/reports/usage` | `from`, `to` | `{points[{date,bytes_in,bytes_out}], totals}` |
| GET | `/admin/reports/receivables` | — | `{invoices[{id,number,customer_name,total,due_date,days_overdue}], total}` |
| GET | `/admin/reports/growth` | `months` (3/6/12) | `{points[{period,new,churn,active}]}` |

Audit CSV: `GET /admin/audit-logs?actor&...&format=csv` (maks 10.000 baris,
kolom: `id,actor_id,actor_username,action,target_type,target_id,detail,ip,created_at`).

## 8. Portal pelanggan (`user`)

| Method | Path | Auth | Permission | Deskripsi |
|---|---|---|---|---|
| GET | `/portal/summary` | JWT | `customer:read_own` | Ringkasan multi-akun: status, sisa kuota/waktu, jatuh tempo |
| GET | `/portal/invoices` | JWT | `invoice:read_own` | List tagihan milik sendiri |
| GET | `/portal/invoices/:id` | JWT | `invoice:read_own` | Detail tagihan |
| GET | `/portal/usage?from&to` | JWT | `session:read_own` | Pemakaian harian |
| POST | `/portal/invoices/:id/pay` | JWT | `payment:write_own` | Buat QR (`201 {attempt_id,qr_payload,expires_at,amount,base_amount,unique_code}`) |
| GET | `/portal/payment-attempts/:id` | JWT | `invoice:read_own` | Status attempt (polling portal) |
| POST | `/portal/topup` | JWT | `payment:write_own` | Beli top-up kuota (preset 1/5/10 GB) |
| POST/GET | `/portal/plan-changes` | JWT | `payment:write_own` / `invoice:read_own` | Ajukan / list ganti paket mandiri |
| POST | `/portal/plan-changes/:id/cancel` | JWT | `payment:write_own` | Batalkan pengajuan |
| GET | `/portal/packages` | JWT | `package:read` | Katalog paket publik |

Batas ganti paket: `upgrade_group` sama + `allow_self_service`, tanpa
tunggakan (409 bila ada), maks 1x/bulan; upgrade langsung + prorata,
downgrade pending periode berikut.

## 9. Webhook kriskris (publik)

| Method | Path | Auth | Deskripsi |
|---|---|---|---|
| POST | `/api/v1/webhooks/kriskris` | HMAC `X-Kriskris-Signature: sha256=<hex>` atas body mentah | Klaim cepat (<10 dtk) lalu proses async; event selain `invoice.paid` di-200 + abaikan |

Respons: `2xx` sukses/duplikat (idempoten via `delivery_id`),
`401` HMAC salah, `400` JSON rusak, `500` DB/config rusak.
Setelah verifikasi, server memanggil GET invoice kriskris dan nominal
harus cocok persis — detail asumsi di
[`apps/api/internal/kriskris/README.md`](../apps/api/internal/kriskris/README.md).
