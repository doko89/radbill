# kriskris — integrasi QRIS (agen C2, G2)

Self-contained di `internal/kriskris/**`. Tidak mengimpor paket G1/G2 lain
kecuali `shared/id`; SQL ditulis tangan (`database/sql`, `$1`, waktu TEXT
RFC3339 UTC) agar tanpa konflik regen sqlc.

Struktur: `domain/` (murni, stdlib saja) → `infrastructure/` (client HTTP,
settings terenkripsi, store SQL) → `application/` (service + poller) →
`presentation/` (handler Fiber). `testkit/` hanya untuk `_test.go`
(fake server kriskris + sqlite `:memory:` + seed).

## API publik (didikte — jangan ubah)

```go
// presentation (package kriskrispres)
h := kriskrispres.NewHandler(pool *sql.DB) // baca env/settings sendiri
h.PayInvoice    // method fiber: POST invoice :id -> buat QR
h.AttemptStatus // method fiber: GET invoice :id (+?attempt_id=)
h.Webhook       // method fiber: POST /api/v1/webhooks/kriskris (publik)

// application (package kriskrisapp)
stop := kriskrisapp.StartPoller(ctx context.Context, pool *sql.DB) // -> stop func
```

Tambahan non-didikte (opsional, tidak wajib dipakai C1):
`NewHandlerWithService`, `NewService`, `NewServiceFromPool`,
`Service.{Pay,Status,Claim,ConfirmPaid,ProcessWebhookSync}`,
`EncryptValue/DecryptValue`, `Client.{CreateInvoice,CancelInvoice,GetInvoice,BatchStatus}`.

## Kontrak auth (C1 menjamin)

- C1 memasang middleware `Require` (JWT) SEBELUM `PayInvoice`/`AttemptStatus`.
- Handler membaca `c.Locals("billing_customer_id")` (string; kosong = admin,
  lewati cek kepemilikan) dan `c.Locals("billing_role")` (info saja).
  Klaim JWT tidak pernah dibaca langsung; `customer_id` tidak pernah dari
  URL/body (anti-IDOR). Invoice orang lain → **404** (bukan 403).
- `Webhook` publik tanpa JWT; keaslian dari HMAC (di bawah).

## Kontrak settings-rahasia

- Kunci AES: `SHA256(SETTINGS_ENC_KEY)`; GCM; format
  `enc:v1:<base64(nonce12|ciphertext)>`.
- Key: `kriskris_api_key`, `kriskris_api_secret`,
  `kriskris_callback_secret` (terenkripsi), `kriskris_base_url` (plain).
- Env override (bila non-kosong, env menang):
  `KRISKRIS_BASE_URL`, `KRISKRIS_API_KEY`, `KRISKRIS_API_SECRET`,
  `KRISKRIS_CALLBACK_SECRET`, `SETTINGS_ENC_KEY`.
- Tanpa konfigurasi lengkap: semua endpoint jawab 500 generik, poller
  log + retry tiap 30 dtk (boot tidak crash).

## SEMUA asumsi API kriskris (tidak ada spes resmi — koreksi di satu tempat)

| # | Asumsi | Lokasi kode |
|---|--------|-------------|
| 1 | Header request: `X-Kriskris-Timestamp` (RFC3339 UTC), `X-Kriskris-Signature` (hex HMAC atas 5 baris kanonik PRD §6.4), `X-Kriskris-Api-Key` (identitas key) | `infrastructure/client.go` |
| 2 | `POST /api/v1/external/invoices` body persis `{"amount":<int rupiah>,"label":<nomor invoice>,"expiry_minutes":15}` (tanpa field lain) | `Client.CreateInvoice` |
| 3 | Respons create/get: `{"data":{"id","qr_payload","amount","status","expires_at"}}`; objek flat tanpa `data` juga diterima; alias `qr_string`/`qr_data`/`invoice_id` diterima | `decodeInvoice` |
| 4 | `POST /api/v1/external/invoices/{id}/cancel` body `{}` (path asumsi; PRD hanya tulis "`POST .../cancel`"); 404/sudah-cancel = sukses | `Client.CancelInvoice` |
| 5 | `GET /api/v1/external/invoices/{id}` (PRD tulis "`GET /external/invoices/:id`"); 404 → `ErrRemoteNotFound` | `Client.GetInvoice` |
| 6 | Batch: `POST /api/v1/external/invoices/batch` body `{"ids":[...]}` maks 50, respons `{"data":[{"id","status","amount"},...]}` (key `invoices` juga diterima). Path DAN bentuk body asumsi penuh | `Client.BatchStatus` |
| 7 | Error: `{"error":{"code","message"}}` (flat `{"code",...}` juga diterima); kode bisnis `duplicate_pending`, `gobiz_offline`, `quota_exceeded`, `invalid_amount` | `asRequestError` |
| 8 | Webhook header `X-Kriskris-Signature: sha256=<hex>` atas body mentah (dari PRD); `X-Kriskris-Timestamp` dicek ±5 mnt HANYA bila ada (nama header asumsi) | `presentation/handler.go` |
| 9 | Payload webhook: `delivery_id`/`event`/`data.id` (+ alias `deliveryId`, `type`, `invoice_id`, `data.invoice_id`); fallback idempotensi `evt:<event>:<id>` lalu `sha256:<body>`; event selain `invoice.paid` di-200 + abaikan | `parseWebhook` |
| 10 | `amount` integer rupiah (bukan sen); `status` remote lowercase `pending\|paid\|expired\|cancelled\|failed` | client + service |
| 11 | `expires_at` respons: RFC3339; bila kosong → now+15 mnt | `Service.Pay` |

Status lokal `payment_attempts`: `pending|paid|expired|cancelled|failed`.
Nominal remote WAJIB sama persis dengan `attempts.amount`, jika tidak TIDAK
di-apply (PRD §6.4: contoh payload kriskris tidak konsisten).

Perilaku rekap: bayar di QR basi/kedaluwarsa (transfer telat) TETAP
melunasi invoice bila nominal cocok (attempt `cancelled`/`expired` masih
applicable) — menghindari rekonsiliasi manual. Invoice `void` + uang masuk →
attempt `failed` (parkir untuk rekonsiliasi manual). Invoice sudah `paid`
(mis. manual) → attempt ditutup tanpa dobel pembayaran.

## Kontrak DB yang diasumsikan (DDL milik C1)

`payment_attempts(id, invoice_id, kriskris_invoice_id UNIQUE, base_amount,
unique_code, amount, qr_payload, status, expires_at, paid_at, created_at,
updated_at)`, `webhook_deliveries(delivery_id PK, event, payload,
received_at)`, `payments(..., note, ...)` (`note=''` baris base,
`note='unique_code'` baris kode), kolom G1 apa adanya untuk
`invoices/payments(method 'kriskris' sudah di CHECK)/pppoe_accounts/packages/
settings`. `NOTIFY`: `SELECT pg_notify('radbill_events', '{"type":"pppoe",
"action":"upsert","id":...}')` best-effort SETELAH commit; error diabaikan
(jalur sqlite; radiusd fallback polling). Payload meniru format G1
`network/notify` tanpa mengimpornya.

## FOLLOW-UP: CoA/Disconnect ke NAS (ke C1/orkestrator)

DILARANG impor `network/coa` oleh aturan G2 — modul ini TIDAK mengirim
CoA/Disconnect. Setelah apply path (webhook/poller) melunasi invoice:
status DB + `expires_at` + `NOTIFY radbill_events` sudah cukup untuk
radiusd (cache refresh via LISTEN/NOTIFY + polling). Bila sesi aktif harus
langsung diputus/di-CoA saat aktivasi (bukan menunggu interim), C1/
orkestrator menambahkan pemanggilan CoA di titik integrasi (mis. hook
setelah `ConfirmPaid`/`ApplyPaidTx` sukses, atau subscriber
`radbill_events` di sisi billing). CoA butuh data NAS (`nas.ip/secret/
coa_port`) yang hanya dimiliki lapisan network — di luar kepemilikan G2.

## Info orkestrator / C1 (wiring)

1. Route yang disarankan (param `:id` = id invoice):
   `POST /api/v1/portal/invoices/:id/pay` → `PayInvoice` (JWT + CSRF);
   `GET .../pay` → `AttemptStatus` (JWT); `POST /api/v1/webhooks/kriskris`
   → `Webhook` (**publik, WAJIB dikecualikan dari middleware CSRF**
   boilerplate — caller eksternal tak punya token CSRF).
2. Isi Locals `billing_customer_id` (string, `""` = admin) +
   `billing_role` di middleware sebelum Pay/Status.
3. Jalankan `kriskrisapp.StartPoller(ctx, pool)` saat boot billing
   (in-process, berhenti via ctx/stop func). Rate ≤60 req/mnt (token bucket).
4. Mapping respons: Pay 201 `{data:{attempt_id,qr_payload,expires_at,
   amount,base_amount,unique_code,message}}`; 404 `{code:not_found}`;
   409 paid/void; 503 `{code:gobiz_offline|quota_exceeded|duplicate_pending}`
   → frontend SEMBUNYIKAN tombol bayar + tawarkan manual;
   `invalid_amount` → 500 generik (detail hanya di log).
5. Webhook selalu 2xx cepat (<10 dtk: klaim = 1 insert) kecuali 401
   (HMAC salah) / 400 (JSON rusak) / 500 (DB/config rusak).
6. Prasyarat: jam server NTP (±5 mnt untuk signature); tabel C1
   termigrasi; settings/env terisi (lihat Kontrak settings).
7. Polling portal: `AttemptStatus` tiap beberapa detik hingga
   `invoice_status=paid` atau `attempt.status` final.

## Verifikasi (agen C2)

`gofmt` bersih; `go build`, `go vet`, `go test ./internal/kriskris/...`
3x hijau + 1x `-race` hijau (41 test, tanpa internet: fake kriskris via
`httptest` + sqlite `:memory:` aktif). Tidak ada file di luar
`apps/api/internal/kriskris/**` yang dibuat/diubah; tidak commit/push.
