# PRD: Aplikasi Billing RADIUS untuk MikroTik

| | |
|---|---|
| Versi | 0.1 (draft) |
| Tanggal | 7 November 2026 |
| Status | Untuk review |

---

## 1. Ringkasan

Aplikasi billing untuk ISP/RT-RW Net yang mengelola pelanggan PPPoE di router MikroTik. Sistem terdiri dari dua binary Go yang berbagi satu database PostgreSQL:

- **billing**: REST API + panel web (React) untuk admin dan portal pelanggan.
- **radiusd**: server RADIUS (auth, accounting, CoA/Disconnect) yang berdiri sendiri, sehingga billing boleh restart tanpa memutus autentikasi pelanggan.

Pembayaran online memakai **kriskris** (QRIS via outlet GoBiz). Dana langsung masuk ke rekening outlet; kriskris hanya mencatat dan mencocokkan transaksi.

## 2. Tujuan dan Non-Tujuan

### Tujuan
1. Mengotomatiskan siklus hidup pelanggan: aktivasi, tagihan, pembayaran, isolir, dan pengaktifan kembali.
2. Mendukung paket berbasis **waktu (validity/online time)**, **kuota**, dan **FUP**, serta kombinasinya.
3. Pelanggan dapat membayar, ganti paket, dan top-up kuota secara mandiri dari portal.
4. Jalur autentikasi RADIUS tetap berjalan meskipun aplikasi billing mati.
5. Deploy sederhana: dua binary + PostgreSQL.

### Non-Tujuan (fase awal)
- Voucher hotspot dan autentikasi hotspot.
- Registrasi pelanggan baru secara mandiri.
- MS-CHAPv2, EAP, dan RadSec.
- Multi-tenant/reseller.
- Tiket gangguan, inventaris perangkat, dan pemetaan jaringan.
- Payment gateway selain kriskris.

## 3. Pengguna dan RBAC

| Role | Deskripsi | Akses |
|---|---|---|
| `admin` | Operator ISP | Seluruh `/admin/*` |
| `user` | Pelanggan | Hanya `/portal/*`, terbatas pada data miliknya |

Ketentuan:
- RBAC diimplementasikan sebagai **peta permission di kode** (contoh: `package:write`, `pppoe:write`, `invoice:read_own`), bukan pengecekan `role == admin` tersebar. Role baru (kasir, teknisi) cukup menambah entri peta.
- Untuk role `user`, `customer_id` **selalu diambil dari klaim JWT**, tidak pernah dari URL/body (mencegah IDOR).
- Admin pertama dibuat lewat CLI atau env var, bukan endpoint publik.

## 4. Arsitektur dan Stack

```
MikroTik (NAS) <--RADIUS--> radiusd ----+
                                         +--> PostgreSQL
Browser <--HTTPS--> billing (Fiber) ----+
                        |
                        +--> kriskris API (buat tagihan QRIS)
kriskris --callback--> billing
billing --CoA/Disconnect (UDP 3799)--> MikroTik
```

| Lapisan | Pilihan |
|---|---|
| Backend billing | Go + **Fiber v3** (`github.com/gofiber/fiber/v3`) |
| Frontend | React + Vite + TanStack Query + shadcn/ui, di-embed ke binary billing lewat `go:embed` |
| RADIUS | Go + `layeh.com/radius` (RFC 2865/2866/5176), dictionary vendor MikroTik (ID 14988) |
| Database | PostgreSQL |
| Akses data | `sqlc` + `pgx` |
| Migrasi | `goose` |
| Scheduler | In-process di billing (tagihan, isolir, polling pembayaran) |

Struktur repo (satu Go module):
```
cmd/billing/     API + embed web/dist
cmd/radiusd/     RADIUS server
internal/        model, db (sqlc), service bersama
web/             React + Vite
migrations/      goose
```

Catatan Fiber v3:
- Fiber berbasis fasthttp, bukan `net/http`. Library yang bergantung pada `http.Handler` perlu adaptor.
- Handler memakai signature v3 (`func(c fiber.Ctx) error`). Pin versi di `go.mod` dan baca catatan migrasi v2 ke v3 sebelum menulis middleware.
- Frontend di-serve lewat middleware static dari `embed.FS`, dengan fallback `index.html` untuk routing sisi klien (SPA).

Komunikasi antar-komponen: **billing <=> PostgreSQL <=> radiusd**. Tidak ada panggilan langsung billing ke radiusd. Perubahan data dikirim lewat `LISTEN/NOTIFY` agar cache radiusd diperbarui; polling berkala menjadi cadangan.

## 5. Autentikasi

Dua domain yang terpisah total:

| | Panel (admin/user) | PPPoE |
|---|---|---|
| Dipakai oleh | billing | radiusd |
| Mekanisme | JWT | PAP + CHAP |
| Kredensial | **Hash** (argon2id) | **Plaintext** (keputusan produk) |
| Tabel | `accounts` | `pppoe_accounts` |

### 5.1 Panel (JWT)
- Access token ±15 menit: `sub`, `role`, `customer_id`.
- Refresh token disimpan **hash-nya** di DB, dikirim lewat cookie httpOnly + Secure + SameSite, dapat direvoke (ganti password, akun dinonaktifkan).
- Rate limit dan penguncian sementara pada login gagal berulang.
- Lupa/reset password via token sekali pakai berumur pendek.

### 5.2 PPPoE (plaintext)
- Password PPPoE tidak boleh sama dengan password panel dan tidak diturunkan darinya.
- Plaintext tidak boleh muncul di log, response API untuk role `user`, atau backup tidak terenkripsi.
- Admin melihat password hanya lewat aksi eksplisit ("tampilkan"), dan aksi itu dicatat di audit log.
- Akses DB dibatasi: radiusd memakai user DB khusus dengan hak minimum.

## 6. Kebutuhan Fungsional

### 6.1 Manajemen Pelanggan dan Akun PPPoE (admin)
- CRUD pelanggan (nama, telepon, alamat, catatan).
- CRUD akun PPPoE: username (unik), password, paket, status (`active` | `isolated` | `disabled`), masa aktif/jatuh tempo, IP statis (opsional), NAS (opsional).
- Aksi: isolir, aktifkan, nonaktifkan, perpanjang, ganti paket, reset kuota, disconnect sesi.
- Membuat/mereset akun login portal untuk pelanggan.
- Relasi pelanggan ke akun PPPoE **1:N** (satu pelanggan boleh punya beberapa akun/lokasi). Tagihan dibuat per akun PPPoE. *(Lihat pertanyaan terbuka #1.)*

### 6.2 Paket
Paket punya `type` tersimpan: `time` (time based) | `quota` (kuota based) | `fup` (FUP based). Form admin menyesuaikan field batas mengikuti tipe; API menerima tipe kosong (legacy → `time`) dan menolak nilai di luar enum.

| Atribut | Keterangan |
|---|---|
| `nama`, `harga`, `periode` | Umum |
| `type` | `time` \| `quota` \| `fup` |
| `rate_limit` | Kecepatan normal (`Mikrotik-Rate-Limit`) |
| `address_pool` | Pool IP |
| `validity_seconds` | Masa berlaku sejak aktivasi/perpanjangan (nullable) |
| `online_seconds_limit` | Jatah waktu online (nullable) |
| `quota_bytes` | Kuota per periode (nullable) |
| `quota_reset` | `none` \| `daily` \| `monthly` \| `billing_cycle` |
| `fup_threshold_bytes`, `fup_rate_limit` | Ambang dan kecepatan setelah FUP |
| `on_exhausted` | `reject` \| `isolate` \| `throttle` |
| `is_public`, `allow_self_service`, `upgrade_group` | Kontrol tampil dan pindah paket mandiri |

### 6.3 Penagihan
- Generate tagihan otomatis oleh scheduler sesuai siklus (tanggal tetap atau per tanggal aktivasi, dikonfigurasi di pengaturan).
- Invoice: nomor unik, item, jatuh tempo, status (`unpaid` | `paid` | `void`).
- Pembayaran manual oleh admin (tunai/transfer), termasuk rekonsiliasi pembayaran yang terlambat setelah QR kedaluwarsa.
- Semua jenis tagihan (bulanan, prorata ganti paket, top-up kuota) berupa **invoice** biasa sehingga satu alur pembayaran melayani semuanya.
- Masa tenggang (grace period) sebelum isolir, dapat dikonfigurasi.
- Notifikasi tagihan dan pengingat via WhatsApp/email (template dapat diubah). Pengiriman diatur lewat konfigurasi di pengaturan.

### 6.4 Pembayaran Online (kriskris)

Alur:
1. Pelanggan menekan **Bayar** di `/portal/invoices/:id`.
2. Billing membatalkan percobaan sebelumnya yang masih `pending` (`POST .../cancel`), lalu membuat tagihan kriskris (`POST /api/v1/external/invoices`, `label` = nomor invoice).
3. Hasil disimpan di `payment_attempts`; portal menampilkan QR (`qr_payload`) dan hitung mundur 15 menit.
4. kriskris mengirim callback `invoice.paid`.
5. Billing memverifikasi, lalu dalam satu transaksi DB: invoice `paid`, catat pembayaran, perpanjang masa aktif, ubah akun `isolated` menjadi `active`, `NOTIFY` ke radiusd, kirim CoA bila perlu.
6. Portal (polling status) menampilkan "Lunas".

Ketentuan:
- QR dibuat **hanya saat pelanggan menekan Bayar**, karena QR hanya berlaku 15 menit sedangkan invoice bisa hidup berhari-hari.
- **Kode unik** (10 sampai 99) menambah total transfer. UI harus menjelaskannya ("Total Rp86.550, termasuk kode unik 50"). Invoice dianggap lunas sebesar `base_amount`; kode unik dicatat terpisah sebagai pendapatan lain-lain.
- Endpoint callback: `POST /api/v1/webhooks/kriskris`, publik tanpa JWT, dengan syarat:
  - Verifikasi HMAC-SHA256 atas **body mentah** (header `X-Kriskris-Signature`, format `sha256=hex`), pembanding waktu konstan.
  - **Idempoten** lewat `webhook_deliveries.delivery_id` (unique); duplikat dibalas 2xx dan diabaikan.
  - Balas 2xx secepatnya (batas 10 detik); proses berat di background.
  - **Jangan percaya payload mentah**: setelah verifikasi, panggil `GET /external/invoices/:id`, pastikan status `paid` dan nominal cocok dengan `payment_attempts`. (Contoh payload `invoice.paid` di dokumentasi kriskris memiliki angka yang tidak konsisten, sehingga validasi ulang wajib.)
- **Polling cadangan**: job mengecek attempt `pending` lewat endpoint batch (maks. 50 id) tiap ±5 detik hingga kedaluwarsa, agar pembayaran tetap tertangkap bila callback gagal. Batas kriskris: 60 request/menit per API key.
- Penandatanganan request ke kriskris: timestamp RFC3339 (toleransi ±5 menit dari jam server), HMAC-SHA256 atas 5 baris kanonik (timestamp, METHOD, path, query mentah, hex(sha256(body))). Jam server billing wajib disinkronkan (NTP).
- Penanganan error bisnis:

| Kode | Tindakan |
|---|---|
| `duplicate_pending` | Retry otomatis dengan jeda singkat |
| `gobiz_offline` | Sembunyikan tombol bayar, beri peringatan ke admin, tawarkan bayar manual |
| `quota_exceeded` | Peringatan ke admin (saldo/jatah kriskris habis), tawarkan bayar manual |
| `invalid_amount` | Catat error, tampilkan pesan umum |

- API key, API secret, dan callback secret disimpan **terenkripsi** dan tidak pernah ditampilkan ulang di UI.
- Prasyarat deployment: billing harus dapat dijangkau dari internet (domain publik + HTTPS) agar callback bisa masuk. Bila tidak, sistem berjalan murni dengan polling. *(Lihat pertanyaan terbuka #2.)*

### 6.5 Portal Pelanggan (`user`)
- Dashboard: status layanan, sisa kuota/waktu, jatuh tempo.
- Detail paket dan akun PPPoE milik sendiri.
- Grafik dan riwayat pemakaian.
- Daftar tagihan, detail, tombol bayar, riwayat pembayaran.
- **Ganti paket mandiri** (hanya paket `allow_self_service` dalam `upgrade_group` yang sama).
- **Top-up kuota** (jika paket mendukung).
- Profil dan ganti password panel.

Aturan ganti paket:
| Hal | Aturan awal |
|---|---|
| Upgrade | Berlaku langsung, pelanggan membayar selisih prorata |
| Downgrade | Berlaku mulai periode berikutnya |
| Tunggakan | Ganti paket diblokir bila ada invoice belum lunas |
| Frekuensi | Maksimal 1x per periode |
| Pencatatan | Tabel `plan_changes` (status, `effective_at`), bukan langsung mengubah `package_id` |

### 6.6 Sesi dan Jaringan
- Daftar sesi online (dari accounting) dan riwayat sesi.
- Disconnect sesi dari panel (CoA/Disconnect-Request ke NAS).
- CRUD NAS: nama, IP, shared secret, port CoA; tes CoA.
- Pengaturan profil isolir.

### 6.7 Laporan
Pendapatan per periode/paket, pemakaian per pelanggan/paket, piutang dan tunggakan, pertumbuhan dan churn. Ekspor CSV.

### 6.8 Pengaturan dan Audit
- Umum (perusahaan, zona waktu, mata uang), penagihan (siklus, jatuh tempo, grace period), notifikasi, payment gateway, RADIUS (interim interval, atribut default).
- **Audit log** untuk aksi admin: perubahan paket, status akun, pembayaran manual, penampilan password, perubahan pengaturan.

## 7. Perilaku radiusd

### 7.1 Autentikasi
- Lookup NAS berdasarkan IP sumber (shared secret per NAS dari tabel `nas`). Request dari IP tak dikenal dibuang.
- Metode: **PAP dan CHAP**.
- Keputusan:
  - `disabled` atau tidak ditemukan: **Access-Reject**.
  - `isolated`: **Access-Accept** dengan atribut isolir (rate-limit kecil dan/atau `Mikrotik-Address-List=isolir`), lalu MikroTik mengarahkan ke halaman `/isolated`.
  - `active`: Accept dengan `Mikrotik-Rate-Limit`, `Framed-Pool`/`Framed-IP-Address`, `Session-Timeout`, `Acct-Interim-Interval`, dan `Mikrotik-Total-Limit` (+Gigawords bila >4 GB) sesuai sisa jatah.
- Kedaluwarsa (`expires_at` terlewat) atau jatah habis mengikuti `on_exhausted` paket.
- Seluruh data pelanggan aktif dan NAS di-cache in-memory, diperbarui lewat `LISTEN/NOTIFY` + polling cadangan. Auth tidak melakukan query DB per request.

### 7.2 Accounting
- Menangani Start, Interim-Update, Stop, serta Accounting-On/Off (menutup sesi yatim saat router reboot).
- Penulisan ke DB **dibatch**, bukan satu insert per paket.
- `usage_counters` diperbarui dengan **delta** terhadap update sebelumnya per sesi, memperhitungkan Gigawords, reset counter sesi, dan update duplikat.
- Reset periode kuota dilakukan lazy lewat `period_start` baru.

### 7.3 Enforcement saat sesi berjalan
| Komponen | Mekanisme |
|---|---|
| Validity / online time | `Session-Timeout` = sisa waktu; MikroTik memutus sendiri |
| Kuota | Dicek tiap Interim-Update; Disconnect-Request bila habis |
| FUP | Saat melewati ambang, kirim **CoA** dengan `Mikrotik-Rate-Limit` baru tanpa memutus sesi |
| Perubahan status/paket oleh billing | CoA atau Disconnect-Request ke NAS |

`Acct-Interim-Interval` default 1 sampai 5 menit (dapat diatur). Toleransi kecil pada kuota dapat diterima karena adanya jeda antar-interim.

## 8. Model Data (garis besar)

```
accounts            id, username, password_hash, role, status
refresh_tokens      id, account_id, token_hash, expires_at, revoked_at
customers           id, account_id?, nama, telp, alamat
packages            id, nama, harga, periode, rate_limit, address_pool,
                    validity_seconds, online_seconds_limit, quota_bytes,
                    quota_reset, fup_threshold_bytes, fup_rate_limit,
                    on_exhausted, is_public, allow_self_service, upgrade_group
pppoe_accounts      id, customer_id, package_id, nas_id?, username (unique),
                    password, status, expires_at/due_day, static_ip?
usage_counters      pppoe_account_id, period_start, bytes_in, bytes_out,
                    online_seconds
quota_topups        id, pppoe_account_id, bytes, period_start, invoice_id
plan_changes        id, pppoe_account_id, from_package, to_package,
                    effective_at, status
nas                 id, nama, ip, secret, coa_port
radius_sessions     id, session_id, pppoe_account_id, nas_id, start/stop,
                    bytes_in/out, terminate_cause, ...
invoices            id, nomor, pppoe_account_id, customer_id, total,
                    due_date, status
invoice_items       id, invoice_id, deskripsi, jumlah, harga
payments            id, invoice_id, amount, method (kriskris|manual),
                    attempt_id?, paid_at
payment_attempts    id, invoice_id, kriskris_invoice_id (unique), base_amount,
                    unique_code, amount, qr_payload, status, expires_at, paid_at
webhook_deliveries  delivery_id (unique), event, received_at
settings            key, value (rahasia dienkripsi)
audit_logs          id, actor_id, action, target, detail, created_at
```

Pertimbangan: indeks pada `radius_sessions(session_id, nas_id)`, `usage_counters(pppoe_account_id, period_start)`, `pppoe_accounts(username)`. Pertimbangkan partisi atau retensi untuk `radius_sessions` bila volume besar.

## 9. Kebutuhan Non-Fungsional

| Aspek | Target |
|---|---|
| Ketersediaan auth | radiusd tetap melayani saat billing mati; start/restart tanpa ketergantungan pada billing |
| Latensi auth | Di bawah 50 ms untuk data ter-cache (target awal, perlu diukur) |
| Skala awal | Perlu dikonfirmasi *(pertanyaan terbuka #3)*; desain mengasumsikan ribuan pelanggan dan puluhan NAS |
| Keamanan | HTTPS wajib, argon2id, cookie aman, rate limit login dan endpoint publik, validasi input, user DB berhak minimum |
| Rahasia | API key/secret, callback secret, dan shared secret NAS tidak masuk log dan tidak ditampilkan ulang |
| Observabilitas | Log terstruktur, `/healthz` pada kedua binary, metrik dasar (auth rate, reject, antrean accounting, kegagalan callback) |
| Backup | Backup PostgreSQL terenkripsi (basis data memuat password PPPoE plaintext) |
| Jam | Sinkron NTP (wajib untuk signature kriskris dan perhitungan masa aktif) |
| Deploy | Dua binary + PostgreSQL; konfigurasi lewat env; migrasi dijalankan eksplisit |

## 10. Metrik Keberhasilan
- Persentase tagihan yang lunas otomatis tanpa intervensi admin.
- Waktu dari pembayaran hingga layanan aktif kembali (target: dalam hitungan detik sampai satu menit).
- Jumlah pembayaran yang memerlukan rekonsiliasi manual.
- Tingkat keberhasilan autentikasi yang sah dan waktu respons RADIUS.
- Selisih antara pemakaian di `usage_counters` dan data accounting mentah.

## 11. Roadmap

**Fase 1: Fondasi**
Skema DB, auth panel (JWT + RBAC), CRUD pelanggan/paket/PPPoE/NAS, radiusd (PAP/CHAP, accounting, cache), sesi online, pembayaran manual.

**Fase 2: Penagihan dan Pembayaran**
Generate invoice otomatis, isolir/aktivasi otomatis + CoA, integrasi kriskris (QR, callback, polling), portal pelanggan (tagihan, bayar, pemakaian), notifikasi.

**Fase 3: Paket Lanjutan**
Kuota, online time, FUP dengan CoA, top-up kuota, ganti paket mandiri (prorata), laporan.

**Fase 4: Penyempurnaan**
Audit log lengkap, ekspor laporan, dashboard dan metrik, pengerasan keamanan, dokumentasi operasional.

**Di luar fase ini (kandidat):** voucher hotspot, registrasi mandiri, role kasir/teknisi, MS-CHAPv2, tiket gangguan.

## 12. Risiko dan Mitigasi

| Risiko | Mitigasi |
|---|---|
| Kebocoran password PPPoE plaintext | User DB berhak minimum, backup terenkripsi, tidak masuk log/API pelanggan, audit penampilan password |
| IDOR pada portal | `customer_id` dari JWT, tes otorisasi otomatis per endpoint |
| Callback ganda atau palsu | HMAC pada body mentah, idempotensi `delivery_id`, verifikasi ulang ke API kriskris |
| Callback tidak sampai | Polling cadangan + rekonsiliasi manual |
| Kuota tidak akurat (Gigawords, reset counter, update duplikat) | Perhitungan delta yang diuji dengan kasus tepi, toleransi kecil |
| Lonjakan tulis accounting | Batch insert, interval interim yang wajar, indeks dan retensi |
| Ketergantungan pada kriskris/GoBiz (outlet offline, saldo habis) | Deteksi error bisnis, peringatan admin, jalur bayar manual |
| Jam server melenceng | NTP, pemantauan |
| Perubahan API Fiber v3 | Pin versi, baca catatan rilis sebelum upgrade |

## 13. Pertanyaan Terbuka

1. Apakah satu pelanggan boleh memiliki lebih dari satu akun PPPoE, dan apakah tagihan dibuat per pelanggan atau per akun PPPoE?
2. Apakah server billing dapat diakses dari internet (domain publik + HTTPS)? Ini menentukan apakah callback menjadi jalur utama atau polling saja.
3. Perkiraan skala: jumlah pelanggan dan jumlah router/NAS?
4. Siklus tagihan: tanggal tetap untuk semua pelanggan atau per tanggal aktivasi? Apakah prabayar atau pascabayar?
5. Aksi default saat kuota habis: putus, throttle, atau isolir? Apakah ada rollover kuota?
6. Kanal notifikasi: WhatsApp (provider apa), email, atau keduanya?
7. Perlakuan kode unik kriskris dalam pembukuan (dicatat sebagai pendapatan lain-lain atau disamarkan)?
8. Mekanisme redirect halaman isolir di MikroTik (firewall dst-nat berbasis address list) dan siapa yang mengonfigurasinya?
