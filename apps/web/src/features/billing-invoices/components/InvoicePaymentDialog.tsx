import { useState } from "react";
import type { FormEvent } from "react";
import { useMutation } from "@tanstack/react-query";
import { ApiError } from "../../../shared/lib/api";
import { formatIDR } from "../../../shared/lib/billing-format";
import {
  Button,
  Dialog,
  Field,
  FormAlert,
  Input,
  Select,
  fieldError,
} from "../../../shared/ui/billing-ui";
import { recordManualPayment, voidInvoice } from "../api";
import type { Invoice } from "../api";
import { invoiceNumber, invoiceTotal } from "../api";

export function InvoicePaymentDialog({
  invoice,
  mode,
  onClose,
  onSaved,
}: {
  invoice: Invoice;
  mode: "pay" | "void";
  onClose: () => void;
  onSaved: () => void;
}) {
  const total = invoiceTotal(invoice);
  const [amount, setAmount] = useState(total === null ? "" : String(total));
  const [method, setMethod] = useState("tunai");
  const [note, setNote] = useState("");
  const [reason, setReason] = useState("");
  const [clientError, setClientError] = useState<string | null>(null);

  const mutation = useMutation({
    mutationFn: () =>
      mode === "pay"
        ? recordManualPayment(invoice.id, {
            amount: Number(amount),
            method,
            note: note.trim() === "" ? undefined : note.trim(),
          })
        : voidInvoice(invoice.id, reason.trim() === "" ? undefined : reason.trim()),
    onSuccess: onSaved,
  });

  const serverFields = mutation.error instanceof ApiError ? mutation.error.fields : undefined;
  const serverMessage =
    mutation.error instanceof ApiError
      ? mutation.error.message
      : mutation.error
        ? mode === "pay"
          ? "gagal mencatat pembayaran"
          : "gagal membatalkan tagihan"
        : null;

  function onSubmit(event: FormEvent): void {
    event.preventDefault();
    if (mode === "pay") {
      const value = Number(amount);
      if (amount.trim() === "" || !Number.isFinite(value) || value <= 0) {
        setClientError("nominal harus angka lebih dari 0");
        return;
      }
    }
    setClientError(null);
    mutation.mutate();
  }

  return (
    <Dialog
      open
      onClose={onClose}
      title={mode === "pay" ? "Catat pembayaran manual" : "Batalkan tagihan?"}
      description={`Tagihan ${invoiceNumber(invoice)} · ${formatIDR(total)}`}
    >
      <form onSubmit={onSubmit} className="flex flex-col gap-4" noValidate>
        {mode === "pay" ? (
          <>
            <Field
              label="Nominal diterima (Rp)"
              htmlFor="payment-amount"
              required
              error={clientError ?? fieldError(serverFields, "amount")}
            >
              <Input
                id="payment-amount"
                inputMode="numeric"
                value={amount}
                onChange={(e) => setAmount(e.target.value)}
                invalid={clientError !== null}
              />
            </Field>
            <Field label="Metode" htmlFor="payment-method">
              <Select id="payment-method" value={method} onChange={(e) => setMethod(e.target.value)}>
                <option value="tunai">tunai</option>
                <option value="transfer">transfer</option>
                <option value="qris">qris</option>
                <option value="lainnya">lainnya</option>
              </Select>
            </Field>
            <Field label="Catatan (opsional)" htmlFor="payment-note">
              <Input
                id="payment-note"
                value={note}
                onChange={(e) => setNote(e.target.value)}
                placeholder="mis. bukti transfer BCA …1234"
              />
            </Field>
          </>
        ) : (
          <Field label="Alasan pembatalan (opsional)" htmlFor="void-reason">
            <Input
              id="void-reason"
              value={reason}
              onChange={(e) => setReason(e.target.value)}
              placeholder="mis. salah input nominal"
            />
          </Field>
        )}
        {serverMessage && <FormAlert message={serverMessage} />}
        <div className="flex justify-end gap-2">
          <Button variant="secondary" onClick={onClose} disabled={mutation.isPending}>
            Batal
          </Button>
          <Button type="submit" variant={mode === "void" ? "danger" : "primary"} disabled={mutation.isPending}>
            {mutation.isPending ? "Memproses…" : mode === "pay" ? "Catat lunas" : "Ya, batalkan"}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}
