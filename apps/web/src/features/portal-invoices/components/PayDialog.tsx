import { useEffect, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Check, Copy } from "lucide-react";
import { ApiError } from "../../../shared/lib/api";
import { formatDateTime, formatIDR } from "../../../shared/lib/billing-format";
import { Button, Dialog, FormAlert, FormNote } from "../../../shared/ui/billing-ui";
import { getPaymentAttempt, isPayChannelDown, payAttemptId, payPortalInvoice } from "../api";
import type { PayAttempt, PortalPendingAttempt } from "../api";

/** Tampilan QR terpadu untuk attempt baru maupun lanjutan (pending). */
type AttemptView = {
  id: string | null;
  qrPayload: string;
  expiresAt: string;
  amount: number;
  uniqueCode: number | null;
  message: string | null;
};

function fromPay(data: PayAttempt): AttemptView {
  return {
    id: payAttemptId(data),
    qrPayload: data.qr_payload,
    expiresAt: data.expires_at,
    amount: data.amount,
    uniqueCode: data.unique_code,
    message: data.message ?? null,
  };
}

function fromPending(data: PortalPendingAttempt): AttemptView {
  return {
    id: data.id,
    qrPayload: data.qr_payload,
    expiresAt: data.expires_at,
    amount: data.amount,
    uniqueCode: null,
    message: null,
  };
}

function secondsUntil(iso: string, now: number): number {
  const end = new Date(iso).getTime();
  if (Number.isNaN(end)) {
    return 0;
  }
  return Math.max(0, Math.floor((end - now) / 1000));
}

function formatCountdown(totalSeconds: number): string {
  const minutes = Math.floor(totalSeconds / 60);
  const seconds = totalSeconds % 60;
  return `${String(minutes).padStart(2, "0")}:${String(seconds).padStart(2, "0")}`;
}

const TERMINAL = new Set(["paid", "expired", "cancelled", "failed"]);

/**
 * Dialog Bayar QRIS: buat attempt, tampilkan payload QR + hitung mundur,
 * polling status tiap 3 detik. Tanpa dep QR baru: payload ditampilkan
 * sebagai teks + tombol salin (keputusan C3, lihat laporan).
 */
export function PayDialog({
  invoiceId,
  invoiceNumber,
  total,
  pending,
  onClose,
  onPaid,
  onChannelDown,
}: {
  invoiceId: string;
  invoiceNumber: string;
  total: number;
  pending: PortalPendingAttempt | null;
  onClose: () => void;
  onPaid: () => void;
  onChannelDown: () => void;
}) {
  const [view, setView] = useState<AttemptView | null>(pending ? fromPending(pending) : null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [channelDown, setChannelDown] = useState(false);
  const [copied, setCopied] = useState(false);
  const [now, setNow] = useState(() => Date.now());

  const pollId = view?.id ?? null;
  const status = useQuery({
    queryKey: ["portal", "attempt", pollId],
    queryFn: () => getPaymentAttempt(pollId ?? ""),
    enabled: pollId !== null && !channelDown,
    refetchInterval: (query) => {
      const data = query.state.data;
      if (!data || TERMINAL.has(data.status)) {
        return false;
      }
      return 3000;
    },
  });

  const hasView = view !== null;
  useEffect(() => {
    if (!hasView) {
      return;
    }
    const timer = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(timer);
  }, [hasView]);

  useEffect(() => {
    if (status.data?.status === "paid") {
      onPaid();
    }
  }, [status.data?.status, onPaid]);

  const left = view ? secondsUntil(view.expiresAt, now) : 0;
  const expired = view !== null && left <= 0;
  const paid = status.data?.status === "paid";
  const terminal = expired || (status.data !== undefined && TERMINAL.has(status.data.status));

  async function startPay(): Promise<void> {
    setBusy(true);
    setError(null);
    try {
      const data = await payPortalInvoice(invoiceId);
      setView(fromPay(data));
      setNow(Date.now());
    } catch (err) {
      if (isPayChannelDown(err)) {
        setChannelDown(true);
        onChannelDown();
      } else {
        setError(err instanceof ApiError ? err.message : "gagal membuat kode QR");
      }
    } finally {
      setBusy(false);
    }
  }

  async function copyPayload(): Promise<void> {
    if (!view) {
      return;
    }
    try {
      await navigator.clipboard.writeText(view.qrPayload);
      setCopied(true);
      window.setTimeout(() => setCopied(false), 2500);
    } catch {
      setError("gagal menyalin, salin manual dari kolom di bawah");
    }
  }

  return (
    <Dialog
      open
      onClose={onClose}
      title={`Bayar ${invoiceNumber}`}
      description="Pindai QRIS dengan aplikasi pembayaran Anda."
    >
      <div className="flex flex-col gap-4">
        {channelDown ? (
          <div className="flex flex-col gap-3" role="alert">
            <FormAlert message="Pembayaran QRIS sedang tidak tersedia (kanal GoBiz/saldo gangguan). Tombol bayar disembunyikan sementara." />
            <div className="rounded-md border border-slate-200 bg-slate-50 px-3 py-2 text-sm text-slate-700">
              <p className="m-0 font-semibold">Opsi bayar manual</p>
              <p className="m-0 mt-1">
                Bayar tunai/transfer ke admin lalu konfirmasi agar tagihan ditandai lunas. Total
                tagihan: <span className="font-bold tabular-nums">{formatIDR(total)}</span>.
              </p>
            </div>
            <div className="flex justify-end">
              <Button variant="secondary" onClick={onClose}>
                Tutup
              </Button>
            </div>
          </div>
        ) : paid ? (
          <div className="flex flex-col gap-3">
            <FormNote message="Pembayaran lunas. Layanan akan aktif kembali otomatis." />
            <div className="flex justify-end">
              <Button onClick={onClose}>Selesai</Button>
            </div>
          </div>
        ) : view === null ? (
          <div className="flex flex-col gap-3">
            <p className="m-0 text-sm text-slate-600">
              Total tagihan <span className="font-bold text-slate-900 tabular-nums">{formatIDR(total)}</span>.
              Kode QR berlaku 15 menit sejak dibuat.
            </p>
            {error && <FormAlert message={error} />}
            <div className="flex justify-end gap-2">
              <Button variant="secondary" onClick={onClose}>
                Batal
              </Button>
              <Button onClick={() => void startPay()} disabled={busy}>
                {busy ? "Membuat QR…" : "Buat kode QR"}
              </Button>
            </div>
          </div>
        ) : (
          <div className="flex flex-col gap-3">
            <div className="flex items-baseline justify-between gap-2">
              <p className="m-0 text-sm text-slate-500">
                Total bayar{" "}
                <span className="font-bold text-slate-900 tabular-nums">{formatIDR(view.amount)}</span>
              </p>
              <p
                className={`m-0 text-sm font-bold tabular-nums ${expired ? "text-red-700" : "text-emerald-700"}`}
                role="timer"
                aria-label={expired ? "Kode QR kedaluwarsa" : `Berlaku ${formatCountdown(left)} lagi`}
              >
                {expired ? "Kedaluwarsa" : formatCountdown(left)}
              </p>
            </div>
            {view.uniqueCode !== null && view.uniqueCode > 0 && (
              <p className="m-0 text-sm text-slate-600">
                Total {formatIDR(view.amount)}, termasuk kode unik {view.uniqueCode}.
                {view.message ? ` ${view.message}` : ""}
              </p>
            )}
            {view.uniqueCode === null && (
              <p className="m-0 text-sm text-slate-500">
                Melanjutkan pembayaran sebelumnya (kedaluwarsa {formatDateTime(view.expiresAt)}).
              </p>
            )}
            <label
              htmlFor="pay-qr-payload"
              className="text-sm font-semibold text-slate-800"
            >
              Payload QRIS (salin ke aplikasi pembayaran)
            </label>
            <textarea
              id="pay-qr-payload"
              readOnly
              rows={4}
              value={view.qrPayload}
              onFocus={(e) => e.target.select()}
              className="w-full font-mono text-xs text-slate-800 bg-slate-50 border border-slate-300 rounded-md px-3 py-2 break-all"
              style={{ overflowWrap: "anywhere" }}
            />
            <div className="flex flex-wrap gap-2">
              <Button variant="secondary" size="sm" onClick={() => void copyPayload()}>
                {copied ? (
                  <Check className="size-4" aria-hidden="true" />
                ) : (
                  <Copy className="size-4" aria-hidden="true" />
                )}
                {copied ? "Tersalin" : "Salin payload"}
              </Button>
            </div>
            {error && <FormAlert message={error} />}
            {status.isError && !terminal && (
              <p className="m-0 text-xs text-slate-500" role="status">
                Gagal mengecek status, mencoba lagi otomatis…
              </p>
            )}
            {!terminal && (
              <p className="m-0 text-xs text-slate-500" role="status">
                Menunggu pembayaran — status diperbarui otomatis tiap 3 detik.
              </p>
            )}
            {expired && !paid && (
              <div className="flex flex-col gap-2">
                <FormAlert message="Kode QR kedaluwarsa. Buat kode baru untuk melanjutkan." />
                <div className="flex justify-end gap-2">
                  <Button variant="secondary" onClick={onClose}>
                    Tutup
                  </Button>
                  <Button onClick={() => void startPay()} disabled={busy}>
                    {busy ? "Membuat QR…" : "Buat QR baru"}
                  </Button>
                </div>
              </div>
            )}
            {!expired && status.data && TERMINAL.has(status.data.status) && !paid && (
              <div className="flex flex-col gap-2">
                <FormAlert message={`Pembayaran ${status.data.status}. Buat kode baru untuk mencoba lagi.`} />
                <div className="flex justify-end gap-2">
                  <Button variant="secondary" onClick={onClose}>
                    Tutup
                  </Button>
                  <Button onClick={() => void startPay()} disabled={busy}>
                    {busy ? "Membuat QR…" : "Buat QR baru"}
                  </Button>
                </div>
              </div>
            )}
            {!terminal && (
              <div className="flex justify-end">
                <Button variant="secondary" onClick={onClose}>
                  Tutup
                </Button>
              </div>
            )}
          </div>
        )}
      </div>
    </Dialog>
  );
}
