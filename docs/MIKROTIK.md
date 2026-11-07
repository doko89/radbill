# Setup MikroTik (RouterOS)

Panduan menghubungkan router MikroTik sebagai NAS ke Radbill.
Lihat juga: [OPERATIONS.md](OPERATIONS.md) (deploy server),
[DECISIONS.md](DECISIONS.md) §8 (keputusan redirect isolir).

Konvensi contoh di dokumen ini:

| Simbol | Contoh | Ganti dengan |
|---|---|---|
| IP radiusd/billing | `203.0.113.10` | IP publik/VPN server Anda |
| IP router (NAS) | `203.0.113.1` | IP sumber request RADIUS dari router |
| Secret RADIUS | `RAHASIA-SAMA-32-CHAR` | String acak, SAMA di router dan tabel `nas` |
| Domain billing | `billing.isp-anda.id` | Domain HTTPS billing |

Semua perintah dijalankan di terminal RouterOS (`/system ssh` atau WinBox
→ Terminal). Berlaku untuk RouterOS 6 maupun 7 (catatan beda ditulis
eksplisit).

## 1. Daftarkan NAS di Radbill dulu

Sebelum menyentuh router, buat baris NAS di panel admin
(`/admin/nas` → Tambah): nama, **IP persis IP sumber router**
(`203.0.113.1`), secret, port CoA `3799`.

> radiusd **membuang** request dari IP yang tidak terdaftar sebagai NAS
> (tanpa respons sama sekali). Bila auth seolah timeout total, penyebab
> #1 adalah IP NAS salah (mis. IP VPN vs IP publik).

## 2. RADIUS client di RouterOS

```routeros
# Server RADIUS (service=ppp untuk PPPoE)
/radius add address=203.0.113.10 secret=RAHASIA-SAMA-32-CHAR \
    service=ppp timeout=1s comment="radbill billing"

# Pakai RADIUS untuk login + accounting PPPoE
/ppp aaa set use-radius=yes accounting=yes interim-update=5m
```

Verifikasi:

```routeros
/radius print
```

Contoh output yang benar:

```text
Flags: X - disabled
 #   SERVICE       ADDRESS         SECRET           TIMEOUT
 0   ppp           203.0.113.10    RAHASIA-SAMA...  1s
```

`/radius monitor 0` menampilkan `requests`, `accepts`, `rejects`,
`timeouts` — `timeouts` naik terus = secret salah atau UDP 1812/1813
tertutup (lihat §7).

## 3. Profil PPPoE

Kecepatan, pool, dan batas sesi **didorong server per-sesi** via atribut
RADIUS, jadi profil router boleh generik:

```routeros
/ppp profile add name=radbill use-encryption=no \
    comment="rate-limit & pool dari RADIUS"
/ppp profile set radbill local-address=10.99.0.1
```

- `use-encryption=no`: Radbill hanya mendukung PAP + CHAP
  (MS-CHAPv2/EAP di luar cakupan PRD).
- Jangan isi `rate-limit` di profil kecuali sebagai fallback darurat;
  nilai RADIUS (`Mikrotik-Rate-Limit` dari paket) selalu menang.
- Pool IP: buat pool yang namanya SAMA dengan `address_pool` di tiap
  paket Radbill, karena server mengirim `Framed-Pool=<nama>`:

```routeros
/ip pool add name=pppoe-pelanggan ranges=10.99.1.2-10.99.1.254
```

Akun dengan IP statis menerima `Framed-IP-Address` dan tidak butuh pool.

## 4. Interim-update

Server mengirim `Acct-Interim-Interval` (default **300 detik**, diubah
via settings `acct_interim_interval`) di tiap Access-Accept, dan nilai
itu yang dipakai router. `interim-update=5m` di `/ppp aaa` hanya fallback
bila atribut tidak ada — samakan saja keduanya di 5 menit.

Kenapa penting: kuota dicek tiap Interim-Update; makin jarang, makin besar
kelebihan pakai yang lolos antar-interim. Jangan di atas 5 menit bila
paket kuota dipakai.

## 5. Isolir: address-list `isolir` + redirect

Mekanisme (keputusan final, PRD pertanyaan #8): akun `isolated` tetap
**Access-Accept**, tetapi dengan atribut:

- `Mikrotik-Address-List=isolir` (default; diubah via settings
  `isolir_address_list`) — RouterOS otomatis memasukkan IP sesi ke
  address-list `isolir`, tanpa entri manual.
- `Mikrotik-Rate-Limit=128k/128k` (default; via `isolir_rate_limit`) —
  sesi isolir tetap lambat walau lolos redirect.

Firewall lalu membelokkan HTTP ke halaman tagihan. Urutan rules penting
(RouterOS mengevaluasi dari atas).

### 5.1. Rules lengkap (tempel berurutan)

```routeros
# 1) Izinkan DNS agar browser bisa resolve (tanpa ini halaman redirect
#    pun gagal dibuka karena DNS diblokir)
/ip firewall filter add chain=forward action=accept \
    src-address-list=isolir protocol=udp dst-port=53 \
    comment="isolir: allow DNS" place-before=0
/ip firewall filter add chain=forward action=accept \
    src-address-list=isolir protocol=tcp dst-port=53 \
    comment="isolir: allow DNS tcp" place-before=1

# 2) Izinkan akses langsung ke server billing (portal bayar + halaman
#    /isolated harus reachable)
/ip firewall filter add chain=forward action=accept \
    src-address-list=isolir dst-address=203.0.113.10 \
    comment="isolir: allow billing" place-before=2

# 3) Belokkan HTTP (port 80) ke server billing
/ip firewall nat add chain=dstnat action=dst-nat \
    src-address-list=isolir protocol=tcp dst-port=80 \
    to-addresses=203.0.113.10 to-ports=80 \
    comment="isolir: redirect http ke billing"

# 4) Blokir sisanya (HTTPS tidak bisa di-redirect transparan — browser
#    akan error sertifikat; itu ekspektasi normal, lihat §5.2)
/ip firewall filter add chain=forward action=reject reject-with=icmp-admin-prohibited \
    src-address-list=isolir comment="isolir: blokir selain billing+DNS" place-before=3
```

Cek hasil:

```routeros
/ip firewall address-list print where list=isolir
/ip firewall nat print where comment~"isolir"
```

Saat akun terisolir dial-up, IP-nya muncul di address-list `isolir`
otomatis (dynamic, huruf `D`).

### 5.2. Halaman `/isolated`: di-serve dari mana?

**Status implementasi: belum ada.** Backend maupun SPA Radbill saat ini
**belum me-serve halaman `/isolated`** (tidak ada route-nya). Sampai
halaman itu dibangun, pasang halaman statis sendiri di server billing:

```nginx
# nginx: halaman isolir statis (HTTP port 80, karena redirect dst-nat
# di atas mengarah ke port 80)
server {
    listen 80;
    server_name billing.isp-anda.id;
    root /var/www/isolated;
    location = / { try_files /index.html =404; }
}
```

`/var/www/isolated/index.html` minimal: logo ISP, teks "Layanan
diblokir sementara karena tagihan menunggak", tombol ke
`https://billing.isp-anda.id/portal/invoices`, dan nomor WhatsApp admin.

Catatan jujur soal HTTPS: redirect transparan hanya untuk HTTP port 80.
Pengguna yang membuka situs HTTPS saat terisolir akan melihat error
sertifikat (bukan halaman isolir) — itu keterbatasan teknis standar
semua sistem redirect (bukan bug). Praktik umum: beri tahu pelanggan
untuk membuka `http://` apa saja / portal billing langsung, dan andalkan
notifikasi tagihan sebelum isolir.

## 6. Rate-limit per paket (profil kecepatan)

Format string RouterOS standar di kolom `rate_limit` paket, contoh:

| Paket | `rate_limit` | Arti |
|---|---|---|
| 10 Mbps | `10M/10M` | rx/tx 10 Mbps |
| 20/5 Mbps | `20M/5M` | unduh 20, unggah 5 |
| Burst | `10M/10M 20M/20M 10M/10M 30/30 8` | max-limit + burst-limit + burst-threshold + burst-time + priority |

Alur enforcement:

- Sesi normal: `Mikrotik-Rate-Limit` paket.
- FUP tersentuh: server kirim **CoA** dengan `fup_rate_limit` —
  kecepatan turun **tanpa memutus sesi**.
- Kuota/waktu habis: ikut `on_exhausted` paket —
  `reject` (putus), `isolate` (Accept rasa isolir), atau `throttle`
  (Accept di rate FUP/isolir tanpa batas waktu).

## 7. CoA / Disconnect (UDP 3799)

Billing mengirim CoA (ubah kecepatan/FUP) dan Disconnect-Request (putus
sesi: isolir manual, ganti paket, disconnect dari panel) ke NAS.

Di router:

```routeros
# RouterOS 7
/radius incoming set accept=yes port=3799

# RouterOS 6: /radius incoming set accept=yes port=3799
```

Firewall: izinkan UDP 3799 dari IP billing ke router:

```routeros
/ip firewall filter add chain=input action=accept protocol=udp \
    src-address=203.0.113.10 dst-port=3799 \
    comment="radbill: CoA dari billing" place-before=0
```

Port CoA per-NAS bisa diubah di kolom `coa_port` tabel `nas`
(kosong/0 = 3799). Uji dari panel: `/admin/nas` → Tes CoA pada baris
NAS (atau `POST /api/v1/admin/nas/:id/test-coa`).

> CoA butuh sesi aktif yang cocok (username + NAS). Bila selalu timeout
> padahal auth jalan: cek `accept=yes`, firewall input, dan pastikan
> billing menembak IP yang SAMA dengan IP router yang terlihat dari
> server (`/ip address print`; awas NAT/VPN).

## 8. Uji end-to-end

1. **Auth + accounting dasar.** Buat pelanggan + akun PPPoE `test-01`
   (status `active`) di panel. Dari server, uji RADIUS langsung:

   ```bash
   # Debian/Ubuntu: apt install freeradius-utils
   radtest test-01 'password-pppoe' 127.0.0.1 0 RAHASIA-SAMA-32-CHAR
   ```

   Harus `Access-Accept` + atribut `Mikrotik-Rate-Limit`.
   Lalu dial PPPoE sungguhan dari router/laptop ke MikroTik; sesi harus
   muncul di `/admin/sessions` (tab Online).

2. **Isolir.** Di panel, Isolir akun `test-01` (atau tunggu scheduler
   grace-period). Dial ulang / disconnect sesi lama:
   - `/ppp active print` → sesi ada, `address-list` = `isolir`.
   - `/ip firewall address-list print where list=isolir` → IP sesi ada.
   - Buka `http://neverssl.com` dari client → mendarat di halaman
     `/isolated` (port 80). Kecepatan ≈ 128k.
   - Aktifkan lagi di panel → sesi baru normal, IP hilang dari `isolir`.

3. **CoA.** Dari sesi aktif normal, tekan Disconnect di `/admin/sessions`
   → sesi putus dalam ±5 detik. Untuk FUP: set `fup_threshold_bytes`
   kecil di paket uji, generate traffic, pastikan rate turun tanpa putus
   (cek `/ppp active print detail` → `rate-limit` berubah).

4. **Accounting.** Cek `radius_sessions` terisi dan
   `usage_counters` bertambah setelah Interim-Update pertama (±5 menit
   atau paksa dari router). Toleransi selisih kecil antar-interim wajar.

Simpan akun `test-01` sebagai probe permanen untuk monitoring
(lihat [OPERATIONS.md](OPERATIONS.md) §10).
