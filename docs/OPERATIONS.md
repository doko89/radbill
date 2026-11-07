# Operasional Radbill

Panduan deploy dan operasi untuk administrator sistem. Bahasa: Indonesia.
Lihat juga: [MIKROTIK.md](MIKROTIK.md) (setup RouterOS),
[API.md](API.md) (referensi endpoint), [DECISIONS.md](DECISIONS.md)
(keputusan desain).

## 1. Arsitektur

Dua binary Go + satu database PostgreSQL. Tidak ada panggilan langsung
antar-binary; sinkronisasi lewat tabel + `LISTEN`/`NOTIFY` channel
`radbill_events` (fallback: polling berkala).

```text
MikroTik (NAS) <--RADIUS 1812/1813--> radiusd (bin/radiusd) ----+
                                                                 +--> PostgreSQL
Browser <--HTTPS--> reverse proxy --> billing (bin/api :3000) --+
                        |                |
                        |                +--> kriskris API (QRIS)
kriskris --callback--> billing (POST /api/v1/webhooks/kriskris)
billing --CoA/Disconnect UDP 3799--> MikroTik
```

| Binary | Sumber | Peran | Port default |
|---|---|---|---|
| `bin/api` | `apps/api/cmd/api` | REST API + panel web (SPA di-embed) + scheduler in-process + poller kriskris | TCP 3000 (`APP_PORT`) |
| `bin/radiusd` | `apps/api/cmd/radiusd` | RADIUS auth (PAP/CHAP) + accounting + enforcement kuota/FUP | UDP 1812/1813, TCP 8081 (health) |

Sifat penting:

- `radiusd` tetap melayani autentikasi saat `bin/api` mati atau restart
  (cache in-memory + DB langsung). Urutan start bebas.
- Skema DB auto-migrate saat boot (`CREATE TABLE IF NOT EXISTS`);
  tidak ada tool migrasi terpisah (PRD menyebut goose, implementasi
  memakai auto-migrate bawaan — lihat [DECISIONS.md](DECISIONS.md)).
- Dev memakai sqlite (`DB_DRIVER=sqlite`); produksi WAJIB postgres
  (`LISTEN`/`NOTIFY` hanya jalan di postgres; sqlite mengandalkan polling
  dan bukan untuk produksi multi-proses).

## 2. Prasyarat

- Linux x86_64 atau ARM64, Go 1.24+ (hanya saat build dari source),
  Node.js 20+ (hanya saat build SPA).
- **PostgreSQL 14+** dengan satu database + dua user DB (hak minimum):
  - `radbill` (dipakai `bin/api`): baca-tulis semua tabel.
  - `radbill_radius` (dipakai `bin/radiusd`): baca semua tabel,
    tulis hanya `radius_sessions` + `usage_counters`.
- **NTP tersinkron** (wajib): signature request kriskris memakai timestamp
  RFC3339 dengan toleransi ±5 menit, dan perhitungan masa aktif bergantung
  pada jam server. Verifikasi: `timedatectl status` menunjukkan
  `System clock synchronized: yes`.
- Domain publik + HTTPS bila memakai callback kriskris. Mode konfirmasi
  dipilih di `/admin/settings` → "Mode konfirmasi kriskris" (`webhook` =
  callback + polling cadangan; `poll` = polling saja untuk jaringan privat,
  tanpa restart). Tanpa domain publik, pakai mode `poll` — lihat PRD §6.4.
- UDP 1812/1813 dari IP router NAS ke server radiusd, dan UDP 3799
  dua arah antara billing dan NAS (CoA/Disconnect).

Contoh pembuatan user DB:

```sql
CREATE DATABASE radbill;
CREATE USER radbill WITH PASSWORD 'ganti-dengan-kuat';
CREATE USER radbill_radius WITH PASSWORD 'ganti-dengan-kuat-juga';
GRANT ALL PRIVILEGES ON DATABASE radbill TO radbill;
-- Jalankan setelah tabel terbuat (atau set default privileges):
GRANT CONNECT ON DATABASE radbill TO radbill_radius;
GRANT USAGE ON SCHEMA public TO radbill_radius;
GRANT SELECT ON ALL TABLES IN SCHEMA public TO radbill_radius;
GRANT INSERT, UPDATE ON radius_sessions, usage_counters TO radbill_radius;
ALTER DEFAULT PRIVILEGES IN SCHEMA public
  GRANT SELECT ON TABLES TO radbill_radius;
```

## 3. Variabel environment

Semua konfigurasi lewat env (file `.env` opsional untuk dev; env asli
selalu menang atas `.env`). Tanda `*` = wajib di produksi
(`APP_ENV=prod` gagal boot bila kosong).

### 3.1. Umum + billing (`bin/api`)

| Key | Default | Keterangan |
|---|---|---|
| `APP_ENV` | `dev` | `dev` \| `prod` \| `test` |
| `APP_PORT` | `3000` | Port HTTP billing |
| `DB_DRIVER` | `sqlite` | `sqlite` (dev) \| `postgres` (prod) |
| `SQLITE_PATH` | `./data/app.db` | Path sqlite; `:memory:` untuk test |
| `DATABASE_URL` | — | Wajib bila postgres, mis. `postgres://radbill:pw@127.0.0.1:5432/radbill?sslmode=require` |
| `SESSION_SECRET` * | — | Rahasia cookie sesi boilerplate (min. 32 char acak) |
| `SESSION_TTL_HOURS` | `12` | Umur sesi cookie |
| `ADMIN_USER` | `admin` | Seed admin boilerplate (bila tabel users kosong) |
| `ADMIN_PASS` * | `admin123` (dev) | Password seed; wajib diisi di prod |
| `UPLOAD_DIR` | `./data/uploads` | Penyimpanan avatar, di-serve di `/uploads/*` |
| `STORAGE_DRIVER` | `local` | `local` \| `s3` |
| `S3_ENDPOINT` | — | Wajib bila s3, `host[:port]` tanpa skema |
| `S3_REGION` | `us-east-1` | Region S3 |
| `S3_BUCKET` | — | Wajib bila s3 (auto-create bila hilang) |
| `S3_ACCESS_KEY` | — | Wajib bila s3 |
| `S3_SECRET_KEY` | — | Wajib bila s3 |
| `S3_USE_SSL` | `true` | `false` untuk MinIO HTTP lokal |
| `JWT_SECRET` * | — | Rahasia JWT panel billing (min. 32 char acak) |
| `JWT_ACCESS_TTL_MINUTES` | `15` | Umur access token (±15 mnt per PRD §5.1) |
| `REFRESH_TTL_DAYS` | `30` | Umur refresh token (cookie httpOnly) |
| `TZ` | `Asia/Jakarta` | Zona waktu proses billing |
| `SETTINGS_ENC_KEY` * | — | Kunci enkripsi settings (`SHA256` → AES-256-GCM); backup bersama DB |
| `SCHEDULER_INTERVAL_SECONDS` | `60` | Tick scheduler (generate invoice, isolir grace, applier downgrade) |
| `KRISKRIS_BASE_URL` | — | Override env untuk settings `kriskris_base_url` (env menang bila non-kosong) |
| `KRISKRIS_API_KEY` | — | Override env untuk `kriskris_api_key` |
| `KRISKRIS_API_SECRET` | — | Override env untuk `kriskris_api_secret` |
| `KRISKRIS_CALLBACK_SECRET` | — | Override env untuk `kriskris_callback_secret` |
| `KRISKRIS_MODE` | `webhook` | `webhook` atau `poll` (poll = tanpa callback, untuk jaringan privat; berlaku live tanpa restart) |

Login eksternal boilerplate (semua mati default; tidak dipakai billing):

| Key | Default | Keterangan |
|---|---|---|
| `OIDC_ENABLED` | `false` | `true` untuk login OIDC |
| `OIDC_ISSUER` | `https://accounts.google.com` | Issuer provider |
| `OIDC_CLIENT_ID` / `OIDC_CLIENT_SECRET` / `OIDC_REDIRECT_URL` | — | Wajib bila OIDC aktif |
| `OIDC_LABEL` | `Google` | Label tombol login |
| `LDAP_ENABLED` | `false` | `true` untuk login LDAP |
| `LDAP_URL` | — | Wajib bila aktif, `ldaps://` di produksi |
| `LDAP_STARTTLS` | `false` | `true` bila server pakai StartTLS |
| `LDAP_CA_FILE` | — | Pin CA privat (PEM) |
| `LDAP_BIND_DN` / `LDAP_BIND_PASSWORD` | — | Search mode (disarankan) |
| `LDAP_BASE_DN` | — | Wajib bila aktif |
| `LDAP_USER_FILTER` | `(uid=%s)` | Harus mengandung satu `%s` |
| `LDAP_USER_DN_TEMPLATE` | — | Direct bind bila tanpa `LDAP_BIND_DN` |
| `LDAP_MAIL_ATTR` / `LDAP_NAME_ATTR` | `mail` / `displayName` | Atribut direktori |
| `LDAP_LABEL` | `Company Login` | Label tab login |

### 3.2. radiusd (`bin/radiusd`)

| Key | Default | Keterangan |
|---|---|---|
| `DB_DRIVER` | `sqlite` | `postgres` di produksi |
| `SQLITE_PATH` | `./data/app.db` | Hanya dev (SATU proses saja; sqlite + 2 proses tidak aman) |
| `DATABASE_URL` | — | Wajib bila postgres; pakai user `radbill_radius` |
| `RADIUS_AUTH_ADDR` | `:1812` | Listen UDP auth |
| `RADIUS_ACCT_ADDR` | `:1813` | Listen UDP accounting |
| `RADIUS_HEALTH_ADDR` | `:8081` | Listen TCP `/healthz` |
| `RADIUS_CACHE_POLL_SECONDS` | `60` | Reload penuh cache (cadangan `LISTEN`/`NOTIFY`) |
| `TZ` | — | Zona waktu proses radiusd (kosong = sistem) |
| `LOG_LEVEL` | `info` | `debug` \| `info` \| `warn` \| `error` |

### 3.3. Contoh `.env` produksi

```bash
APP_ENV=prod
APP_PORT=3000
DB_DRIVER=postgres
DATABASE_URL=postgres://radbill:KUAT-1@127.0.0.1:5432/radbill?sslmode=require

SESSION_SECRET=isi-32-char-acak-dari-openssl-rand-hex-32
JWT_SECRET=isi-32-char-acak-lain-dari-openssl-rand-hex-32
SETTINGS_ENC_KEY=isi-32-char-acak-lain-lagi-jangan-sama
ADMIN_USER=admin
ADMIN_PASS=ganti-saat-seed-lalu-ganti-lagi

JWT_ACCESS_TTL_MINUTES=15
REFRESH_TTL_DAYS=30
SCHEDULER_INTERVAL_SECONDS=60
TZ=Asia/Jakarta
UPLOAD_DIR=/var/lib/radbill/uploads

# radiusd (bisa satu file .env yang sama bila satu host)
RADIUS_AUTH_ADDR=:1812
RADIUS_ACCT_ADDR=:1813
RADIUS_HEALTH_ADDR=127.0.0.1:8081
RADIUS_CACHE_POLL_SECONDS=60
LOG_LEVEL=info
# Catatan: radiusd membaca DATABASE_URL yang sama; di produksi sebaiknya
# radiusd memakai EnvironmentFile terpisah dengan user radbill_radius.
```

Buat rahasia acak: `openssl rand -hex 32`.

## 4. Instalasi

```bash
# 1. Build (di mesin build atau CI)
make setup            # go mod download + npm install (sekali saja)
make build            # -> ./bin/api  (API + SPA ter-embed)
make build-radiusd    # -> ./bin/radiusd

# Cross-compile ARM64 bila perlu:
make build-linux-arm64  # -> ./bin/api-linux-arm64

# 2. Di server: salin binary + buat user + direktori data
useradd --system --no-create-home --shell /usr/sbin/nologin radbill
mkdir -p /opt/radbill/bin /var/lib/radbill/uploads
cp bin/api bin/radiusd /opt/radbill/bin/
chown -R radbill:radbill /opt/radbill /var/lib/radbill

# 3. Isi /etc/radbill/billing.env dan /etc/radbill/radiusd.env (mode 600)
# 4. Pasang unit systemd di bawah, lalu:
systemctl daemon-reload
systemctl enable --now radbill-billing radbill-radiusd
```

Admin panel pertama: buat lewat CLI/env (`ADMIN_USER`/`ADMIN_PASS`
dipakai saat seed), bukan endpoint publik (PRD §3).

## 5. Unit systemd contoh

`/etc/systemd/system/radbill-billing.service`:

```ini
[Unit]
Description=Radbill billing (API + panel web)
After=network.target postgresql.service
Wants=postgresql.service

[Service]
Type=simple
User=radbill
Group=radbill
WorkingDirectory=/opt/radbill
EnvironmentFile=/etc/radbill/billing.env
ExecStart=/opt/radbill/bin/api
Restart=on-failure
RestartSec=5
# Pengerasan dasar
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ReadWritePaths=/var/lib/radbill

[Install]
WantedBy=multi-user.target
```

`/etc/systemd/system/radbill-radiusd.service`:

```ini
[Unit]
Description=Radbill radiusd (RADIUS auth + accounting)
After=network.target postgresql.service
Wants=postgresql.service

[Service]
Type=simple
User=radbill
Group=radbill
WorkingDirectory=/opt/radbill
EnvironmentFile=/etc/radbill/radiusd.env
ExecStart=/opt/radbill/bin/radiusd
Restart=on-failure
RestartSec=5
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
# UDP 1812/1813 < 1024 butuh capability bila tidak pakai authbind:
AmbientCapabilities=CAP_NET_BIND_SERVICE

[Install]
WantedBy=multi-user.target
```

`/etc/radbill/radiusd.env` (mode `600`, milik `radbill`):

```bash
DB_DRIVER=postgres
DATABASE_URL=postgres://radbill_radius:KUAT-2@127.0.0.1:5432/radbill?sslmode=require
RADIUS_AUTH_ADDR=:1812
RADIUS_ACCT_ADDR=:1813
RADIUS_HEALTH_ADDR=127.0.0.1:8081
RADIUS_CACHE_POLL_SECONDS=60
TZ=Asia/Jakarta
LOG_LEVEL=info
```

## 6. Reverse proxy + TLS

Billing hanya HTTP polos; TLS di-terminate di proxy. Contoh Caddy
(otomatis Let's Encrypt):

```caddy
billing.isp-anda.id {
    reverse_proxy 127.0.0.1:3000
}
```

Contoh nginx:

```nginx
server {
    listen 443 ssl;
    server_name billing.isp-anda.id;

    ssl_certificate     /etc/letsencrypt/live/billing.isp-anda.id/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/billing.isp-anda.id/privkey.pem;

    location / {
        proxy_pass http://127.0.0.1:3000;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto https;
    }
}
```

Wajib: callback kriskris (`POST /api/v1/webhooks/kriskris`) harus bisa
dijangkau publik via HTTPS; bila tidak, pembayaran online hanya mengandalkan
poller (tetap jalan, aktivasi bisa terlambat).

## 7. Backup terenkripsi

Basis data memuat **password PPPoE plaintext** — backup WAJIB terenkripsi
dan tidak pernah polos di disk maupun log.

Backup harian dengan `age` (disarankan):

```bash
# Sekali saja: buat key
age-keygen -o /root/radbill-backup-key.txt   # simpan offline!
chmod 600 /root/radbill-backup-key.txt
PUB=$(grep -o 'age1[0-9a-z]*' /root/radbill-backup-key.txt)

# Cron harian (/etc/cron.d/radbill-backup)
0 2 * * * postgres pg_dump -d radbill --format=custom \
  | age -r "$PUB" -o /var/backups/radbill/radbill-$(date +\%F).dump.age
```

Alternatif dengan GPG:

```bash
0 2 * * * postgres pg_dump -d radbill --format=custom \
  | gpg --encrypt --recipient ops@isp-anda.id \
  -o /var/backups/radbill/radbill-$(date +\%F).dump.gpg
```

Yang ikut di-backup: dump DB + `/etc/radbill/*.env` (berisi
`SETTINGS_ENC_KEY` — tanpa ini, rahasia kriskris di tabel settings tidak
bisa didekripsi) + kunci backup itu sendiri (offline, terpisah dari server).

Retensi contoh: 7 harian + 4 mingguan; uji restore tiap bulan.

## 8. Restore

```bash
# age
age --decrypt -i /root/radbill-backup-key.txt \
  -o /tmp/radbill.dump /var/backups/radbill/radbill-2026-11-07.dump.age

# lalu restore ke DB kosong
createdb -U postgres radbill_restore
pg_restore -U postgres -d radbill_restore /tmp/radbill.dump
shred -u /tmp/radbill.dump   # hapus file polos

# GPG: ganti langkah decrypt dengan
gpg --decrypt -o /tmp/radbill.dump /var/backups/radbill/radbill-2026-11-07.dump.gpg
```

Pindahkan traffic setelah verifikasi (`/healthz` OK + login panel OK),
bukan sebelum.

## 9. Upgrade

1. Backup DB (bagian 7) sebelum tiap upgrade.
2. Build binary baru, simpan versi lama (`api-$(date +%F)`).
3. Stop `radbill-billing`, ganti binary, start lagi.
   radiusd BOLEH tetap jalan (auth tidak terputus); upgrade radiusd
   terpisah dengan cara yang sama.
4. Verifikasi: `/healthz` kedua binary + login panel + satu auth
   RADIUS uji (lihat [MIKROTIK.md](MIKROTIK.md) §8).
5. Rollback = kembalikan binary lama + restore DB bila skema berubah
   (skema auto-migrate aditif; downgrade umumnya aman).

## 10. Healthcheck & monitoring

| Sinyal | Cara cek | Normal |
|---|---|---|
| Billing hidup + DB OK | `GET http://127.0.0.1:3000/healthz` → `{"status":"ok"}` | HTTP 200 (`degraded` = DB tidak terjangkau) |
| radiusd hidup | `GET http://127.0.0.1:8081/healthz` | HTTP 200 + JSON cache/writer/metrik |
| Scheduler jalan | `GET /api/v1/admin/scheduler/status` (Bearer admin) | `last_ok: true` tiap job, `runs` bertambah |
| Metrik Fase 4 | `GET /api/v1/admin/metrics` (Bearer admin) | `db_ok: true`, pantau `kriskris_pending` & `webhooks_failed_24h` |
| RADIUS riil | `radtest`/`radclient` auth + accounting dari IP NAS | Accept + sesi tercatat |
| Jam sinkron | `timedatectl status` | `System clock synchronized: yes` |

Alert minimum yang disarankan: `/healthz` non-200, `last_ok=false` pada
job scheduler, `webhooks_failed_24h` naik, disk backup penuh, dan NTP
tidak sinkron. Log kedua binary terstruktur ke stderr —
kumpulkan via journald (`journalctl -u radbill-billing`).

## 11. Troubleshooting umum

| Gejala | Kemungkinan penyebab | Tindakan |
|---|---|---|
| Boot gagal `SETTINGS_ENC_KEY/JWT_SECRET/SESSION_SECRET is required in prod` | Rahasia kosong di `APP_ENV=prod` | Isi ketiga rahasia (min. 32 char acak) |
| Semua auth RADIUS reject | IP NAS belum terdaftar / secret salah | Samakan IP + secret di tabel `nas` dan RouterOS (`/radius print`) |
| Request dari router diabaikan total | Bukan NAS dikenal / firewall | radiusd membuang request dari IP tak dikenal; cek tabel `nas` + UDP 1812/1813 |
| Perubahan paket/status lambat di radiusd | `LISTEN`/`NOTIFY` tidak jalan (mis. PgBouncer transaction mode) | Mengandalkan polling `RADIUS_CACHE_POLL_SECONDS`; perbaiki koneksi native bila perlu |
| Invoice tidak ter-generate | Scheduler tidak jalan / settings siklus salah | Cek `/scheduler/status`; cek `billing_cycle_mode`/`billing_cycle_day` |
| QRIS tidak bisa dibuat (`gobiz_offline`) | Outlet GoBiz offline | Sembunyikan tombol bayar, tawarkan bayar manual (jalur resmi PRD §6.4) |
| `quota_exceeded` saat bayar | Jatah/saldo kriskris habis | Peringatan admin + bayar manual |
| Callback 401 di log | `kriskris_callback_secret` beda dengan dashboard kriskris | Samakan secret; pembayaran telat tetap tertangkap poller |
| Jam signature invalid | NTP melenceng > 5 menit | Sinkronkan NTP; toleransi kriskris ±5 menit |
| Kuota tidak akurat | Interim jarang / counter router reset | Kecilkan `acct_interim_interval` (default 300 dtk); toleransi kecil wajar antar-interim |
| Akun `throttle` malah putus | `fup_rate_limit` kosong | Fallback rate isolir dipakai; isi `fup_rate_limit` di paket |
| Backup tidak bisa didekripsi | `SETTINGS_ENC_KEY` hilang / kunci age/GPG hilang | Tidak bisa dipulihkan — simpan kunci offline sejak awal |
