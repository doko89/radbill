import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { useQueryClient } from "@tanstack/react-query";
import { ApiError } from "../../../shared/lib/api";
import { formatBytes, formatIDR } from "../../../shared/lib/billing-format";
import { Button, Dialog, Field, FormAlert, FormNote, Select } from "../../../shared/ui/billing-ui";
import { TOPUP_PRESETS_BYTES, createPortalTopup } from "../api";
import type { PortalAccount, TopupInvoice, TopupPresetBytes } from "../api";

function presetLabel(bytes: TopupPresetBytes): string {
  switch (bytes) {
    case 1073741824:
      return "1 GB";
    case 5368709120:
      return "5 GB";
    case 10737418240:
      return "10 GB";
  }
}

function hasQuota(account: PortalAccount): boolean {
  return account.package.quota_bytes !== null && account.package.quota_bytes !== undefined;
}

function initialAccountId(accounts: PortalAccount[], defaultId: string | null): string {
  const quota = accounts.filter(hasQuota);
  if (defaultId !== null && quota.some((a) => a.id === defaultId)) {
    return defaultId;
  }
  return quota[0]?.id ?? "";
}

/**
 * Dialog Top-Up kuota bersama (namespace `portal-*`, dipakai dasbor,
 * tagihan, dan halaman paket). Preset 1/5/10 GB → buat invoice → tombol
 * "Bayar sekarang" mengarah ke detail invoice dengan dialog Bayar terbuka.
 * Harga final selalu dari respons invoice (server), bukan estimasi lokal.
 */
export function TopupDialog({
  accounts,
  defaultAccountId,
  onClose,
}: {
  accounts: PortalAccount[];
  defaultAccountId: string | null;
  onClose: () => void;
}) {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const quotaAccounts = accounts.filter(hasQuota);
  const [accountId, setAccountId] = useState(() => initialAccountId(accounts, defaultAccountId));
  const [bytes, setBytes] = useState<TopupPresetBytes>(TOPUP_PRESETS_BYTES[0] ?? 1073741824);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [created, setCreated] = useState<TopupInvoice | null>(null);

  const selected = quotaAccounts.find((a) => a.id === accountId) ?? quotaAccounts[0] ?? null;

  async function submit(): Promise<void> {
    if (selected === null) {
      return;
    }
    setBusy(true);
    setError(null);
    try {
      const invoice = await createPortalTopup({ pppoe_account_id: selected.id, bytes });
      setCreated(invoice);
      void queryClient.invalidateQueries({ queryKey: ["portal", "invoices"] });
      void queryClient.invalidateQueries({ queryKey: ["portal", "summary"] });
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "gagal membuat invoice top-up");
    } finally {
      setBusy(false);
    }
  }

  function payNow(): void {
    if (created === null) {
      return;
    }
    onClose();
    navigate(`/portal/invoices/${created.id}`, { state: { autoPay: true } });
  }

  return (
    <Dialog
      open
      onClose={onClose}
      title="Top-Up kuota"
      description="Tambah kuota akun berkuota. Invoice dibuat dulu, lalu bayar seperti biasa."
    >
      {quotaAccounts.length === 0 ? (
        <div className="flex flex-col gap-3">
          <FormAlert message="Paket Anda tidak memakai kuota, jadi top-up tidak tersedia untuk akun ini." />
          <div className="flex justify-end">
            <Button variant="secondary" onClick={onClose}>
              Tutup
            </Button>
          </div>
        </div>
      ) : created !== null ? (
        <div className="flex flex-col gap-3">
          <FormNote
            message={`Invoice ${created.number} dibuat: ${formatBytes(bytes)} seharga ${formatIDR(created.total)}. Bayar agar kuota langsung bertambah.`}
          />
          <div className="flex justify-end gap-2">
            <Button variant="secondary" onClick={onClose}>
              Nanti
            </Button>
            <Button onClick={payNow}>Bayar sekarang</Button>
          </div>
        </div>
      ) : (
        <div className="flex flex-col gap-4">
          {quotaAccounts.length > 1 ? (
            <Field label="Akun" htmlFor="topup-account" required>
              <Select
                id="topup-account"
                value={accountId}
                onChange={(e) => setAccountId(e.target.value)}
              >
                {quotaAccounts.map((account) => (
                  <option key={account.id} value={account.id}>
                    {account.username} · {account.package.name}
                  </option>
                ))}
              </Select>
            </Field>
          ) : (
            selected !== null && (
              <p className="m-0 text-sm text-slate-600">
                Akun <span className="font-bold text-slate-900">{selected.username}</span> ·{" "}
                {selected.package.name}
              </p>
            )
          )}
          <fieldset className="m-0 border-0 p-0">
            <legend className="text-sm font-semibold text-slate-800 px-0">
              Pilih tambahan kuota <span aria-hidden="true" className="text-red-700">*</span>
            </legend>
            <div className="mt-2 grid grid-cols-3 gap-2" role="radiogroup" aria-label="Preset kuota">
              {TOPUP_PRESETS_BYTES.map((preset) => {
                const checked = bytes === preset;
                return (
                  <label
                    key={preset}
                    className={`flex min-h-[44px] cursor-pointer items-center justify-center gap-2 rounded-md border px-3 py-2 text-sm font-bold focus-within:outline-2 focus-within:outline-offset-2 focus-within:outline-emerald-700 ${
                      checked
                        ? "border-emerald-700 bg-emerald-50 text-emerald-900"
                        : "border-slate-300 bg-white text-slate-700 hover:bg-slate-50"
                    }`}
                  >
                    <input
                      type="radio"
                      name="topup-bytes"
                      value={preset}
                      checked={checked}
                      onChange={() => setBytes(preset)}
                      className="size-4 accent-emerald-700"
                    />
                    {presetLabel(preset)}
                  </label>
                );
              })}
            </div>
          </fieldset>
          <p className="m-0 text-sm text-slate-500">
            Harga final dihitung server dan tampil di invoice setelah dibuat.
          </p>
          {error && <FormAlert message={error} />}
          <div className="flex justify-end gap-2">
            <Button variant="secondary" onClick={onClose} disabled={busy}>
              Batal
            </Button>
            <Button onClick={() => void submit()} disabled={busy || selected === null}>
              {busy ? "Membuat invoice…" : "Buat invoice top-up"}
            </Button>
          </div>
        </div>
      )}
    </Dialog>
  );
}
