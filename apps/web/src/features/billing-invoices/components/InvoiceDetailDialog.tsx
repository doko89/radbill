import { useQuery } from "@tanstack/react-query";
import { ApiError } from "../../../shared/lib/api";
import { formatDate, formatDateTime, formatIDR } from "../../../shared/lib/billing-format";
import { Badge, Button, Dialog, FormAlert, Table, TBody, TD, TH, THead, TR } from "../../../shared/ui/billing-ui";
import {
  getInvoice,
  invoiceAccountLabel,
  invoiceItemLabel,
  invoiceItemPrice,
  invoiceItemQty,
  invoiceNotes,
  invoiceNumber,
  invoiceTotal,
} from "../api";

function statusTone(status: string | null | undefined): "success" | "warning" | "default" {
  if (status === "paid") {
    return "success";
  }
  if (status === "unpaid") {
    return "warning";
  }
  return "default";
}

export function InvoiceDetailDialog({ id, onClose }: { id: string; onClose: () => void }) {
  const detail = useQuery({ queryKey: ["billing", "invoices", id], queryFn: () => getInvoice(id) });
  const invoice = detail.data;

  return (
    <Dialog open onClose={onClose} wide title="Detail tagihan" description={invoice ? `Nomor ${invoiceNumber(invoice)}` : undefined}>
      {detail.isPending && (
        <p className="text-sm text-slate-500" role="status">
          Memuat detail…
        </p>
      )}
      {detail.isError && (
        <FormAlert
          message={detail.error instanceof ApiError ? detail.error.message : "gagal memuat detail"}
        />
      )}
      {invoice && (
        <div className="flex flex-col gap-4">
          <dl className="grid grid-cols-2 sm:grid-cols-4 gap-3 text-sm">
            <div>
              <dt className="text-slate-500">Status</dt>
              <dd className="m-0 mt-1">
                <Badge tone={statusTone(invoice.status)}>{invoice.status ?? "?"}</Badge>
              </dd>
            </div>
            <div>
              <dt className="text-slate-500">Total</dt>
              <dd className="m-0 mt-1 font-bold">{formatIDR(invoiceTotal(invoice))}</dd>
            </div>
            <div>
              <dt className="text-slate-500">Jatuh tempo</dt>
              <dd className="m-0 mt-1">{formatDate(invoice.due_date)}</dd>
            </div>
            <div>
              <dt className="text-slate-500">Dibayar</dt>
              <dd className="m-0 mt-1">{formatDateTime(invoice.paid_at)}</dd>
            </div>
            <div className="col-span-2">
              <dt className="text-slate-500">Akun / pelanggan</dt>
              <dd className="m-0 mt-1">{invoiceAccountLabel(invoice) || "-"}</dd>
            </div>
            <div className="col-span-2">
              <dt className="text-slate-500">Dibuat</dt>
              <dd className="m-0 mt-1">{formatDateTime(invoice.created_at ?? invoice.issued_at)}</dd>
            </div>
          </dl>
          {invoiceNotes(invoice) && (
            <p className="m-0 text-sm text-slate-600">
              <strong>Catatan:</strong> {invoiceNotes(invoice)}
            </p>
          )}
          <div>
            <h3 className="m-0 mb-2 text-sm font-bold text-slate-800">Item</h3>
            {(invoice.items ?? []).length === 0 ? (
              <p className="text-sm text-slate-500">Tidak ada rincian item.</p>
            ) : (
              <Table label="Item tagihan">
                <THead>
                  <TR>
                    <TH>Deskripsi</TH>
                    <TH align="right">Jml</TH>
                    <TH align="right">Harga</TH>
                    <TH align="right">Subtotal</TH>
                  </TR>
                </THead>
                <TBody>
                  {(invoice.items ?? []).map((item, index) => {
                    const qty = invoiceItemQty(item) ?? 0;
                    const price = invoiceItemPrice(item) ?? 0;
                    return (
                      <TR key={item.id ?? `item-${index}`}>
                        <TD>{invoiceItemLabel(item) || "-"}</TD>
                        <TD align="right" muted>
                          {qty}
                        </TD>
                        <TD align="right" muted>
                          {formatIDR(price)}
                        </TD>
                        <TD align="right">{formatIDR(qty * price)}</TD>
                      </TR>
                    );
                  })}
                </TBody>
              </Table>
            )}
          </div>
          <div className="flex justify-end">
            <Button variant="secondary" onClick={onClose}>
              Tutup
            </Button>
          </div>
        </div>
      )}
    </Dialog>
  );
}
