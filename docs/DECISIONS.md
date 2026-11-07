# Keputusan Desain Radbill

Jawaban final atas 8 pertanyaan terbuka PRD §13, asumsi API kriskris,
dan deviasi sadar dari PRD per fase. Bahasa: Indonesia.

## 1. Jawaban 8 pertanyaan terbuka PRD §13

### Q1. 1 pelanggan : N akun PPPoE, tagihan per akun PPPoE — FINAL

**Keputusan:** satu pelanggan boleh memiliki banyak akun PPPoE
(1:N, mis. beberapa lokasi), dan **invoice dibuat per akun PPPoE**,
bukan per pelanggan.

**Implementasi:** `pppoe_accounts.customer_id` → `customers.id`;
`invoices.pppoe_account_id` wajib. Portal meringkas multi-akun dalam
satu tampilan (`GET /api/v1/portal/summary`), tetapi bayar/isolir tetap
per akun. Ganti paket dan top-up juga per akun PPPoE.

### Q2. Callback utama + polling cadangan; polling saja bila tak ada domain publik — FINAL

**Keputusan:** webhook `POST /api/v1/webhooks/kriskris` adalah jalur
utama (respon < 10 detik, verifikasi HMAC + idempoten + verifikasi ulang
ke API kriskris), dan **poller batch in-process** (±tiap detik untuk
attempt pending, rate ≤ 60 req/mnt) adalah cadangan. Bila billing tidak
punya domain publik, sistem berjalan **murni polling** tanpa perubahan kode.

**Implementasi:** modul `internal/kriskris` (aplikasi `StartPoller` saat
boot billing). Bayar di QR basi/kedaluwarsa tetap melunasi invoice bila
nominal cocok (menghindari rekonsiliasi manual); invoice `void` + uang
masuk → attempt `failed` (parkir manual).

### Q3. Asumsi skala: ribuan pelanggan, puluhan NAS — FINAL (asumsi, belum diukur)

**Keputusan:** desain mengasumsikan **ribuan pelanggan dan puluhan NAS**
(PRD §9). Target latensi auth < 50 ms untuk data ter-cache **belum diukur**
di beban produksi — wajib load-test sebelum go-live besar.

**Implementasi yang menopangnya:** cache in-memory radiusd (tanpa query
per request), accounting tulis-batch, `LISTEN`/`NOTIFY` + polling,
indeks `radius_sessions(session_id, nas_id)`,
`usage_counters(pppoe_account_id, period_start)`,
`pppoe_accounts(username)`. Retensi/partisi `radius_sessions` belum
diimplementasikan — jadi PR operasional (lihat OPERATIONS.md §10).

### Q4. Siklus per tanggal aktivasi (default), prabayar dengan tenggang — FINAL

**Keputusan:** default `billing_cycle_mode=activation` (siklus per akun
mengikuti tanggal aktivasinya); mode `fixed` (tanggal tetap
`billing_cycle_day`, default 1) tersedia via settings. Model
**prabayar**: invoice terbit di awal siklus, layanan jalan selama periode
berjalan, **grace 3 hari** (`grace_days`, default 3) setelah jatuh tempo,
lalu isolir otomatis + CoA disconnect best-effort.

**Implementasi:** scheduler in-process (`SCHEDULER_INTERVAL_SECONDS`,
default 60): generate invoice idempoten, isolir grace-period, pengingat
dedup harian, status di `GET /api/v1/admin/scheduler/status`.

### Q5. `on_exhausted` per paket: reject | isolate | throttle; tanpa rollover — FINAL

**Keputusan:** aksi saat kuota/waktu habis dikonfigurasi **per paket**
(`packages.on_exhausted`): `reject` (Access-Reject/putus), `isolate`
(Accept rasa isolir), atau `throttle` (Accept di rate terdegradasi tanpa
batas waktu). **Tidak ada rollover kuota** ke periode berikut.

**Implementasi (Fase 3):** radiusd `decide.go`; reset periode mengikuti
`quota_reset` (`none|daily|monthly|billing_cycle`) secara lazy via
`period_start` baru. `throttle` memakai `fup_rate_limit` paket, fallback
ke rate isolir bila kosong.

### Q6. Notifikasi: log + SMTP, template bisa diubah; WhatsApp di luar fase — FINAL

**Keputusan:** provider notifikasi `log` (default) atau `smtp`, dengan
template yang bisa diubah (`tpl_invoice_issued`, `tpl_reminder`,
`tpl_isolated`, `tpl_paid`). WhatsApp (provider apapun) dan gateway email
khusus **tidak diimplementasikan** di fase ini; `smtp_*` generik cukup
untuk SMTP apapun. Semua pengiriman dicatat di `notification_log`.

**Implementasi:** dispatcher notifikasi + adapter settings; pengirim log
untuk dev, SMTP untuk produksi.

### Q7. Kode unik = pendapatan lain-lain, invoice lunas sebesar base — FINAL

**Keputusan:** kode unik kriskris (10–99) **dicatat terpisah sebagai
pendapatan lain-lain**, bukan disamarkan. Invoice dianggap lunas sebesar
`base_amount`; UI menampilkan "Total Rp86.550, termasuk kode unik 50".

**Implementasi:** dua baris `payments` per pelunasan QRIS —
baris base (`note=''`) + baris kode unik (`note='unique_code'`) —
dalam satu transaksi DB bersama perubahan status invoice, perpanjangan
masa aktif, aktivasi ulang, dan `NOTIFY`.

### Q8. Redirect isolir via firewall dst-nat + address-list; operator yang konfigurasi — FINAL

**Keputusan:** akun `isolated` tetap Access-Accept dengan
`Mikrotik-Address-List=isolir` + rate kecil; router **operator**
mengarahkan HTTP ke halaman tagihan via firewall `dst-nat`
(`src-address-list=isolir`, tcp/80 → server billing). Radbill tidak
menyentuh konfigurasi router secara otomatis.

**Implementasi:** atribut didorong radiusd (settings `isolir_address_list`
default `isolir`, `isolir_rate_limit` default `128k/128k`).
Rules lengkap + catatan halaman `/isolated` ada di
[MIKROTIK.md](MIKROTIK.md) §5.

## 2. Asumsi API kriskris

Tidak ada spesifikasi resmi kriskris; seluruh asumsi didokumentasikan di
satu tempat: [`apps/api/internal/kriskris/README.md`](../apps/api/internal/kriskris/README.md)
§"SEMUA asumsi API kriskris" (11 butir: header signature, bentuk body
create/cancel/get/batch, envelope error + kode bisnis, header webhook,
alias payload, satuan rupiah, `expires_at`).

Ringkasan yang paling berisiko bila API asli berbeda:

| # | Asumsi paling rapuh | Dampak bila salah |
|---|---|---|
| 6 | Path + body batch status (`POST /api/v1/external/invoices/batch`, `{"ids":[...]}`) | Poller cadangan mati; hanya callback yang jalan |
| 4 | Path cancel (`POST .../{id}/cancel`, body `{}`) | Attempt basi menumpuk; `duplicate_pending` naik |
| 2–3 | Body create + envelope respons (termasuk alias `qr_string`, `invoice_id`) | Pembuatan QR gagal total |
| 9 | Nama field webhook (`delivery_id`, `event`, `data.id`) | Idempoten jatuh ke fallback hash; duplikat diproses ulang (tetap aman karena verifikasi ulang + transaksi) |

Prinsip pertahanan yang sudah diimplementasikan: **jangan percaya payload
mentah** — setiap klaim pembayaran diverifikasi ulang via GET invoice dan
nominal harus cocok persis dengan `payment_attempts.amount`.

## 3. Deviasi sadar dari PRD (per fase)

### Fase 1 (commit `6a259e0`)

- **PAP double-decrypt fix.** Library `layeh` (`UserPassword_Lookup`)
  sudah mendekripsi; implementasi awal mendekripsi lagi sehingga PAP
  selalu gagal. Diperbaiki: pakai lookup mentah + test regresi vektor
  NAS deterministik. Perilaku akhir = PAP standar PRD.
- **Auto-migrate, bukan goose.** PRD §4 menyebut goose; implementasi
  memakai `CREATE TABLE IF NOT EXISTS` saat boot (konvensi boilerplate,
  dual-driver sqlite/postgres). Konsekuensi: tidak ada migrasi
  destruktif/alter versioned — perubahan skema harus aditif.
- **Binary bernama `bin/api`, bukan `bin/billing`.** Nama boilerplate
  dipertahankan (`make build` → `./bin/api`); dokumen operasional
  memakai nama nyata ini.
- **sqlite untuk dev.** PRD §4 menetapkan PostgreSQL; implementasi
  mendukung sqlite (dev/test) + postgres (prod). `LISTEN`/`NOTIFY` hanya
  di postgres; sqlite mengandalkan polling dan tidak untuk produksi.
- **sqlc + `database/sql`, bukan pgx langsung.** Akses data lewat sqlc
  di atas `database/sql` (driver `pgx/stdlib` untuk postgres,
  `modernc.org/sqlite` murni-Go agar `CGO_ENABLED=0`).

### Fase 2 (commit `c39d7b1`)

- **`settings:write` ganda (baca + tulis).** `GET /api/v1/admin/settings`
  dan `PUT ...` sama-sama mensyaratkan `settings:write` karena tidak ada
  permission `settings:read` (jumlah permission Fase 1 dikunci oleh test
  RBAC). Efek: role non-admin masa depan tidak bisa read-only settings.
- **Kode unik 10–99 (bukan 10–99 dua digit bebas 00–99).**
  Sesuai PRD §6.4 ("10 sampai 99").
- **Poller portal memakai `AttemptStatus` resolve attempt-id dulu.**
  Perbaikan orkestrator: polling portal mengirim attempt-id dengan
  fallback invoice-id + test regresi (kontrak README kriskris §wiring).
- **CoA aktivasi pasca-bayar best-effort.** Modul kriskris dilarang impor
  `network/coa` (aturan G2); aktivasi mengandalkan status DB + `NOTIFY` +
  interim. CoA langsung saat aktivasi adalah follow-up eksplisit untuk
  integrator (lihat README kriskris §FOLLOW-UP).
- **Template notifikasi `{{.Nama}}` dkk (Go template).** Bukan format
  vendor WA — konsisten dengan keputusan Q6.

### Fase 3 (commit `3032123`)

- **Throttle yang semula rasa-isolate menjadi throttle murni.**
  Implementasi awal `on_exhausted=throttle` masih membawa sisa batas
  (efek seperti isolate bagi sesi berjalan); Fase 3 memperbaikinya:
  `throttledAccept` = Accept di rate terdegradasi **tanpa**
  `Session-Timeout`/`Total-Limit` (keduanya akan menggagalkan throttle
  dengan mengakhiri sesi).
- **FUP CoA sekali-per-sesi.** CoA penurunan rate dikirim maksimal sekali
  per sesi (pelacakan di enforcer) agar tidak membanjiri NAS tiap interim.
- **Kuota > 4 GB memakai Gigawords.** `Mikrotik-Total-Limit` + atribut
  Gigawords; accounting memakai delta per sesi (menangani reset counter
  dan update duplikat).
- **Top-up efektif saat lunas via dua hook** (manual + kriskris),
  idempoten dan no-op selektif untuk invoice non-top-up.
- **Downgrade via tabel `plan_changes`** (status + `effective_at`),
  diterapkan scheduler — bukan ubah `package_id` langsung (aturan PRD §6.5).
- **Laporan JSON dulu, CSV di Fase 4.** Endpoint reports Fase 3 JSON
  saja; `?format=csv` ditambahkan Fase 4.

### Fase 4 (berjalan)

- **Reports memakai permission `invoice:read`.** Tidak ada permission
  backend `report:read`; keempat endpoint reports dijaga `invoice:read`
  (admin memegang semua permission sehingga tak berdampak kini; frontend
  memakai string `report:read` sebagai guard aspirasional — admin lolos,
  role non-admin masa depan perlu pemetaan ulang).
- **Audit memakai permission sendiri `audit:read`** (ditambahkan ke peta
  RBAC + test), dan hanya mutasi yang diaudit plus satu read eksplisit
  `pppoe.password_reveal` (PRD §5.2).
- **Halaman `/isolated` belum di-serve.** Backend/SPA belum punya
  route-nya; operator memasang halaman statis sendiri sementara
  (lihat [MIKROTIK.md](MIKROTIK.md) §5.2).
- **Metrik Fase 4 (`GET /api/v1/admin/metrics`)** menambah
  `uptime_seconds`, `db_ok`, status scheduler, `kriskris_pending`, dan
  `webhooks_failed_24h` untuk monitoring (lihat
  [OPERATIONS.md](OPERATIONS.md) §10).
