import { z } from "zod";
import { billingRequest, withQuery } from "../../shared/lib/billing-api";
import { firstNumber, firstText } from "../../shared/lib/billing-format";

export const invoiceItemSchema = z.object({
  id: z.string().nullish(),
  deskripsi: z.string().nullish(),
  description: z.string().nullish(),
  jumlah: z.number().nullish(),
  quantity: z.number().nullish(),
  harga: z.number().nullish(),
  price: z.number().nullish(),
  subtotal: z.number().nullish(),
  total: z.number().nullish(),
});
export type InvoiceItem = z.infer<typeof invoiceItemSchema>;

export const invoiceSchema = z.object({
  id: z.string(),
  nomor: z.string().nullish(),
  number: z.string().nullish(),
  invoice_number: z.string().nullish(),
  pppoe_account_id: z.string().nullish(),
  customer_id: z.string().nullish(),
  customer_name: z.string().nullish(),
  username: z.string().nullish(),
  total: z.number().nullish(),
  amount: z.number().nullish(),
  base_amount: z.number().nullish(),
  paid_amount: z.number().nullish(),
  status: z.string().nullish(),
  due_date: z.string().nullish(),
  issued_at: z.string().nullish(),
  paid_at: z.string().nullish(),
  created_at: z.string().nullish(),
  catatan: z.string().nullish(),
  notes: z.string().nullish(),
  items: z.array(invoiceItemSchema).nullish(),
});
export type Invoice = z.infer<typeof invoiceSchema>;

const invoiceListEnvelope = z.object({ data: z.array(invoiceSchema), total: z.number() });
const invoiceItemEnvelope = z.object({ data: invoiceSchema });

export type InvoiceListParams = {
  search?: string | undefined;
  status?: string | undefined;
  customer_id?: string | undefined;
  pppoe_account_id?: string | undefined;
  page?: number | undefined;
  per_page?: number | undefined;
};

export type InvoiceList = {
  data: Invoice[];
  total: number;
};

export async function listInvoices(params: InvoiceListParams = {}): Promise<InvoiceList> {
  const raw: unknown = await billingRequest(
    withQuery("/api/v1/admin/invoices", { ...params, q: params.search ?? null }),
  );
  return invoiceListEnvelope.parse(raw);
}

export async function getInvoice(id: string): Promise<Invoice> {
  const raw: unknown = await billingRequest(`/api/v1/admin/invoices/${id}`);
  return invoiceItemEnvelope.parse(raw).data;
}

export type InvoiceItemInput = {
  description: string;
  quantity: number;
  price: number;
};

export type InvoiceCreateInput = {
  pppoe_account_id: string;
  due_date?: string | undefined;
  notes?: string | undefined;
  items: InvoiceItemInput[];
};

export async function createInvoice(input: InvoiceCreateInput): Promise<Invoice> {
  const body: Record<string, unknown> = {
    pppoe_account_id: input.pppoe_account_id,
    items: input.items.map((item) => ({
      deskripsi: item.description,
      description: item.description,
      jumlah: item.quantity,
      quantity: item.quantity,
      harga: item.price,
      price: item.price,
    })),
  };
  if (input.due_date) {
    body["due_date"] = input.due_date;
  }
  if (input.notes) {
    body["notes"] = input.notes;
    body["catatan"] = input.notes;
  }
  const raw: unknown = await billingRequest("/api/v1/admin/invoices", {
    method: "POST",
    body,
  });
  return invoiceItemEnvelope.parse(raw).data;
}

/** Void memakai POST :id/void (asumsi kontrak; lihat laporan). */
export async function voidInvoice(id: string, reason?: string): Promise<Invoice> {
  const body: Record<string, string> =
    reason && reason.trim() !== "" ? { reason: reason.trim(), catatan: reason.trim() } : {};
  const raw: unknown = await billingRequest(`/api/v1/admin/invoices/${id}/void`, {
    method: "POST",
    body,
  });
  return invoiceItemEnvelope.parse(raw).data;
}

export type ManualPaymentInput = {
  amount: number;
  method: string;
  note?: string | undefined;
};

export async function recordManualPayment(id: string, input: ManualPaymentInput): Promise<Invoice> {
  const body: Record<string, string | number> = { amount: input.amount, method: input.method };
  if (input.note && input.note.trim() !== "") {
    body["note"] = input.note.trim();
    body["catatan"] = input.note.trim();
  }
  const raw: unknown = await billingRequest(`/api/v1/admin/invoices/${id}/manual-payment`, {
    method: "POST",
    body,
  });
  return invoiceItemEnvelope.parse(raw).data;
}

export function invoiceNumber(invoice: Invoice): string {
  return firstText(invoice.nomor, invoice.invoice_number, invoice.number, invoice.id);
}

export function invoiceTotal(invoice: Invoice): number | null {
  return firstNumber(invoice.total, invoice.amount, invoice.base_amount);
}

export function invoiceAccountLabel(invoice: Invoice): string {
  return firstText(invoice.username, invoice.customer_name, invoice.pppoe_account_id);
}

export function invoiceItemLabel(item: InvoiceItem): string {
  return firstText(item.deskripsi, item.description);
}

export function invoiceItemQty(item: InvoiceItem): number | null {
  return firstNumber(item.jumlah, item.quantity);
}

export function invoiceItemPrice(item: InvoiceItem): number | null {
  return firstNumber(item.harga, item.price);
}

export function invoiceNotes(invoice: Invoice): string {
  return firstText(invoice.catatan, invoice.notes);
}
