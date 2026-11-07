import { useEffect, useRef, useState } from "react";
import { Link, useLocation, useParams } from "react-router-dom";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { ApiError } from "../../../shared/lib/api";
import { formatDate, formatDateTime, formatIDR } from "../../../shared/lib/billing-format";
import {
  Badge,
  Button,
  Card,
  FormAlert,
  PageHeader,
  Table,
  TBody,
  TD,
  TH,
  THead,
  TR,
} from "../../../shared/ui/billing-ui";
import { getPortalInvoice } from "../api";
import { PayDialog } from "../components/PayDialog";

function statusTone(status: string): "success" | "warning" | "default" {
  if (status === "paid") {
    return "success";
  }
  if (status === "unpaid") {
    return "warning";
  }
  return "default";
}

/** Top-up / prorata mengarah ke sini dengan state `{autoPay:true}`. */
function wantsAutoPay(state: unknown): boolean {
  if (typeof state !== "object" || state === null) {
    return false;
  }
  return (state as { autoPay?: unknown }).autoPay === true;
}

export function PortalInvoiceDetailPage() {
  const { id } = useParams();
  const location = useLocation();
  const [paying, setPaying] = useState(false);
  const [channelDown, setChannelDown] = useState(false);
  const autoOpened = useRef(false);
  const queryClient = useQueryClient();

  const detail = useQuery({
    queryKey: ["portal", "invoice", id],
    queryFn: () => getPortalInvoice(id ?? ""),
    enabled: (id ?? "") !== "",
  });

  const invoiceStatus = detail.data?.status;
  useEffect(() => {
    if (!autoOpened.current && wantsAutoPay(location.state) && invoiceStatus === "unpaid") {
      autoOpened.current = true;
      setPaying(true);
    }
  }, [invoiceStatus, location.state]);

  function onPaid(): void {
    setPaying(false);
    void queryClient.invalidateQueries({ queryKey: ["portal", "invoice", id] });
    void queryClient.invalidateQueries({ queryKey: ["portal", "invoices"] });
    void queryClient.invalidateQueries({ queryKey: ["portal", "summary"] });
  }

  const invoice = detail.data;
  const unpaid = invoice?.status === "unpaid";

  return (
    <div>
      <PageHeader
        title={invoice ? invoice.number : "Detail tagihan"}
        description={
          invoice ? `${invoice.pppoe_username} · ${invoice.package_name}` : "Memuat…"
        }
        actions={
          <Link
            to="/portal/invoices"
            className="inline-flex items-center justify-center gap-2 font-semibold rounded-md border text-sm px-3 py-1.5 min-h-[36px] bg-white border-slate-300 text-slate-800 hover:bg-slate-50 no-underline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-emerald-700"
          >
            Kembali
          </Link>
        }
      />
      {channelDown && (
        <div className="mb-4" role="alert">
          <FormAlert message="Pembayaran QRIS sedang tidak tersedia. Silakan bayar manual (tunai/transfer) lalu konfirmasi ke admin." />
        </div>
      )}
      {detail.isPending && (
        <p className="text-sm text-slate-500" role="status">
          Memuat tagihan…
        </p>
      )}
      {detail.isError && (
        <div className="flex flex-col gap-3">
          <FormAlert
            message={
              detail.error instanceof ApiError ? detail.error.message : "gagal memuat tagihan"
            }
          />
          <div>
            <Button variant="secondary" size="sm" onClick={() => void detail.refetch()}>
              Coba lagi
            </Button>
          </div>
        </div>
      )}
      {invoice && (
        <div className="flex flex-col gap-5">
          <Card>
            <div className="flex flex-wrap items-center justify-between gap-3">
              <div className="flex items-center gap-3">
                <Badge tone={statusTone(invoice.status)}>{invoice.status}</Badge>
                <p className="m-0 text-sm text-slate-500">
                  Jatuh tempo <span className="font-semibold text-slate-800">{formatDate(invoice.due_date)}</span>
                  {" · "}Terbit {formatDate(invoice.issued_at)}
                  {invoice.paid_at && ` · Lunas ${formatDateTime(invoice.paid_at)}`}
                </p>
              </div>
              <p className="m-0 text-xl font-bold text-slate-900 tabular-nums">
                {formatIDR(invoice.total)}
              </p>
            </div>
            {unpaid && !channelDown && (
              <div className="mt-4">
                <Button onClick={() => setPaying(true)}>
                  {invoice.pending_attempt ? "Lanjutkan pembayaran" : "Bayar sekarang"}
                </Button>
              </div>
            )}
          </Card>

          <div>
            <h2 className="m-0 mb-2 text-base font-bold text-slate-900">Rincian</h2>
            <Table label="Rincian tagihan">
              <THead>
                <TR>
                  <TH>Deskripsi</TH>
                  <TH align="right">Qty</TH>
                  <TH align="right">Harga</TH>
                  <TH align="right">Subtotal</TH>
                </TR>
              </THead>
              <TBody>
                {invoice.items.map((item) => (
                  <TR key={item.id}>
                    <TD>{item.description}</TD>
                    <TD align="right" muted>
                      {item.qty}
                    </TD>
                    <TD align="right" muted>
                      {formatIDR(item.price)}
                    </TD>
                    <TD align="right">{formatIDR(item.qty * item.price)}</TD>
                  </TR>
                ))}
              </TBody>
            </Table>
          </div>

          <div>
            <h2 className="m-0 mb-2 text-base font-bold text-slate-900">Riwayat pembayaran</h2>
            {invoice.payments.length === 0 ? (
              <p className="m-0 text-sm text-slate-500">Belum ada pembayaran untuk tagihan ini.</p>
            ) : (
              <Table label="Riwayat pembayaran">
                <THead>
                  <TR>
                    <TH>Waktu</TH>
                    <TH>Metode</TH>
                    <TH align="right">Jumlah</TH>
                  </TR>
                </THead>
                <TBody>
                  {invoice.payments.map((payment) => (
                    <TR key={payment.id}>
                      <TD muted>{formatDateTime(payment.paid_at)}</TD>
                      <TD>{payment.method}</TD>
                      <TD align="right">{formatIDR(payment.amount)}</TD>
                    </TR>
                  ))}
                </TBody>
              </Table>
            )}
          </div>
        </div>
      )}
      {paying && invoice && (
        <PayDialog
          invoiceId={invoice.id}
          invoiceNumber={invoice.number}
          total={invoice.total}
          pending={invoice.pending_attempt}
          onClose={() => setPaying(false)}
          onPaid={onPaid}
          onChannelDown={() => {
            setPaying(false);
            setChannelDown(true);
          }}
        />
      )}
    </div>
  );
}
