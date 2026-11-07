import { useState } from "react";
import { Link } from "react-router-dom";
import { useQueryClient } from "@tanstack/react-query";
import { ApiError } from "../../../shared/lib/api";
import { formatDateTime, formatIDR } from "../../../shared/lib/billing-format";
import { Badge, Button, Dialog, FormAlert, FormNote } from "../../../shared/ui/billing-ui";
import { createPortalPlanChange, portalPublicPackageName } from "../api";
import type {
  PortalAccount,
  PortalPlanChangeResult,
  PortalPublicPackage,
} from "../api";

export type PlanDirection = "upgrade" | "downgrade" | "unknown";

/**
 * Dialog konfirmasi ganti paket: upgrade berlaku langsung (muncul invoice
 * prorata), downgrade berlaku periode depan. Tunggakan memblokir pengajuan
 * (sesuai PRD §6.5); server juga bisa menolak dengan 409 → pesan envelope
 * ditampilkan apa adanya.
 */
export function PlanChangeDialog({
  account,
  target,
  targetPrice,
  direction,
  hasArrears,
  unpaidCount,
  onClose,
}: {
  account: PortalAccount;
  target: PortalPublicPackage;
  targetPrice: number | null;
  direction: PlanDirection;
  hasArrears: boolean;
  unpaidCount: number;
  onClose: () => void;
}) {
  const queryClient = useQueryClient();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [result, setResult] = useState<PortalPlanChangeResult | null>(null);

  const targetName = portalPublicPackageName(target);

  async function submit(): Promise<void> {
    setBusy(true);
    setError(null);
    try {
      const data = await createPortalPlanChange({
        pppoe_account_id: account.id,
        to_package_id: target.id,
      });
      setResult(data);
      void queryClient.invalidateQueries({ queryKey: ["portal", "plan-changes"] });
      void queryClient.invalidateQueries({ queryKey: ["portal", "summary"] });
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "gagal mengajukan ganti paket");
    } finally {
      setBusy(false);
    }
  }

  return (
    <Dialog
      open
      onClose={onClose}
      title="Konfirmasi ganti paket"
      description={`Akun ${account.username}: ${account.package.name} → ${targetName}.`}
    >
      {result !== null ? (
        <div className="flex flex-col gap-3">
          <FormNote
            message={
              result.effective_at
                ? `Pengajuan diterima (status: ${result.status}). Berlaku ${formatDateTime(result.effective_at)}.`
                : `Pengajuan diterima (status: ${result.status}).`
            }
          />
          {result.prorata_invoice_id && (
            <p className="m-0 text-sm text-slate-600">
              Invoice prorata dibuat.{" "}
              <Link
                to={`/portal/invoices/${result.prorata_invoice_id}`}
                state={{ autoPay: true }}
                className="font-semibold text-emerald-800 underline"
              >
                Bayar invoice prorata
              </Link>
            </p>
          )}
          <div className="flex justify-end">
            <Button onClick={onClose}>Selesai</Button>
          </div>
        </div>
      ) : hasArrears ? (
        <div className="flex flex-col gap-3">
          <FormAlert
            message={`Ganti paket diblokir: masih ada ${unpaidCount} tagihan belum lunas. Lunasi dulu sesuai ketentuan.`}
          />
          <div className="flex justify-end gap-2">
            <Button variant="secondary" onClick={onClose}>
              Tutup
            </Button>
            <Link
              to="/portal/invoices"
              className="inline-flex min-h-[44px] items-center justify-center gap-2 rounded-md border border-emerald-700 bg-emerald-700 px-4 py-2 text-sm font-semibold text-white no-underline hover:bg-emerald-800 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-emerald-700"
            >
              Lihat tagihan
            </Link>
          </div>
        </div>
      ) : (
        <div className="flex flex-col gap-3">
          <div className="flex flex-wrap items-center gap-2">
            <Badge tone={direction === "upgrade" ? "info" : direction === "downgrade" ? "warning" : "default"}>
              {direction === "upgrade"
                ? "Upgrade — berlaku langsung"
                : direction === "downgrade"
                  ? "Downgrade — periode depan"
                  : "Ganti paket"}
            </Badge>
          </div>
          <p className="m-0 text-sm text-slate-600">
            {account.package.name} ({formatIDR(account.package.price)}) →{" "}
            <span className="font-bold text-slate-900">
              {targetName} ({formatIDR(targetPrice)})
            </span>
          </p>
          {direction === "upgrade" && (
            <p className="m-0 text-sm text-slate-600">
              Upgrade berlaku langsung. Selisih harga dihitung prorata dan muncul sebagai invoice
              baru yang harus dibayar.
            </p>
          )}
          {direction === "downgrade" && (
            <p className="m-0 text-sm text-slate-600">
              Downgrade berlaku mulai periode berikutnya. Paket aktif tetap dipakai sampai masa
              aktif habis.
            </p>
          )}
          {direction === "unknown" && (
            <p className="m-0 text-sm text-slate-600">
              Harga paket tujuan tidak tersedia, jadi waktu berlakunya mengikuti ketentuan server
              (upgrade langsung, downgrade periode depan).
            </p>
          )}
          {error && <FormAlert message={error} />}
          <div className="flex justify-end gap-2">
            <Button variant="secondary" onClick={onClose} disabled={busy}>
              Batal
            </Button>
            <Button onClick={() => void submit()} disabled={busy}>
              {busy ? "Mengajukan…" : "Ya, ganti paket"}
            </Button>
          </div>
        </div>
      )}
    </Dialog>
  );
}
