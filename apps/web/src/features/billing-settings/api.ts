import { z } from "zod";
import { billingRequest } from "../../shared/lib/billing-api";

/**
 * Pengaturan admin (kontrak: GET/PUT /api/v1/admin/settings butuh
 * `settings:write`). Nilai rahasia tiba ter-mask `***` dan hanya ditulis
 * bila diisi (jangan kirim balik mask-nya).
 */

const settingsEnvelope = z.object({
  data: z.record(z.string(), z.string()),
});

export const KRISKRIS_MODES = ["webhook", "poll"] as const;
export type KriskrisMode = (typeof KRISKRIS_MODES)[number];

export const KRISKRIS_MODE_META: Record<KriskrisMode, { title: string; desc: string }> = {
  webhook: {
    title: "Webhook + polling cadangan",
    desc: "kriskris memanggil URL callback server saat status berubah; polling 5 detik tetap jalan sebagai cadangan. Butuh server bisa diakses internet + callback secret terisi.",
  },
  poll: {
    title: "Polling saja",
    desc: "Untuk server di jaringan privat yang tak bisa menerima callback. Status dibaca berkala dari kriskris; endpoint webhook dinonaktifkan.",
  },
};

export type SettingField =
  | { key: string; label: string; kind: "text"; hint?: string }
  | { key: string; label: string; kind: "secret"; hint?: string }
  | { key: string; label: string; kind: "select"; options: { value: string; label: string }[]; hint?: string }
  | { key: string; label: string; kind: "textarea"; hint?: string };

export type SettingSection = { title: string; desc: string; fields: SettingField[] };

export const MASKED = "***";

export const SETTING_SECTIONS: SettingSection[] = [
  {
    title: "kriskris QRIS",
    desc: "Kredensial dari dasbor kriskris. Rahasia tersimpan terenkripsi dan tak pernah ditampilkan.",
    fields: [
      { key: "kriskris_base_url", label: "Base URL", kind: "text", hint: "Default https://kriskris.id" },
      { key: "kriskris_api_key", label: "API key", kind: "secret" },
      { key: "kriskris_api_secret", label: "API secret", kind: "secret" },
      {
        key: "kriskris_callback_secret",
        label: "Callback secret",
        kind: "secret",
        hint: "Wajib dalam mode webhook; boleh kosong dalam mode polling.",
      },
    ],
  },
  {
    title: "Penagihan",
    desc: "Siklus invoice otomatis dan masa tenggang sebelum isolir.",
    fields: [
      {
        key: "billing_cycle_mode",
        label: "Mode siklus",
        kind: "select",
        options: [
          { value: "activation", label: "Per tanggal aktivasi" },
          { value: "fixed", label: "Tanggal tetap tiap bulan" },
        ],
      },
      { key: "billing_cycle_day", label: "Tanggal siklus", kind: "text", hint: "1–31, untuk mode tanggal tetap" },
      { key: "grace_days", label: "Masa tenggang (hari)", kind: "text", hint: "0 = isolir tepat saat jatuh tempo" },
      { key: "topup_price_per_gb", label: "Harga top-up per GB (Rp)", kind: "text" },
      { key: "company_name", label: "Nama perusahaan", kind: "text", hint: "Dipakai di template notifikasi" },
    ],
  },
  {
    title: "Notifikasi",
    desc: "Provider pengiriman template tagihan.",
    fields: [
      {
        key: "notif_provider",
        label: "Provider",
        kind: "select",
        options: [
          { value: "log", label: "Log saja" },
          { value: "smtp", label: "SMTP email" },
        ],
      },
      { key: "notif_from", label: "Pengirim", kind: "text" },
      { key: "smtp_host", label: "SMTP host", kind: "text" },
      { key: "smtp_port", label: "SMTP port", kind: "text" },
      { key: "smtp_user", label: "SMTP user", kind: "text" },
      { key: "smtp_pass", label: "SMTP password", kind: "secret" },
    ],
  },
  {
    title: "Template pesan",
    desc: "Variabel: {{.Nama}} {{.Nomor}} {{.Total}} {{.JatuhTempo}} {{.Username}} {{.Perusahaan}}.",
    fields: [
      { key: "tpl_invoice_issued", label: "Tagihan terbit", kind: "textarea" },
      { key: "tpl_reminder", label: "Pengingat", kind: "textarea" },
      { key: "tpl_isolated", label: "Isolir", kind: "textarea" },
      { key: "tpl_paid", label: "Lunas", kind: "textarea" },
    ],
  },
];

export async function listSettings(): Promise<Record<string, string>> {
  const raw: unknown = await billingRequest("/api/v1/admin/settings/");
  return settingsEnvelope.parse(raw).data;
}

export async function updateSettings(values: Record<string, string>): Promise<Record<string, string>> {
  const raw: unknown = await billingRequest("/api/v1/admin/settings/", {
    method: "PUT",
    body: values,
  });
  return settingsEnvelope.parse(raw).data;
}
