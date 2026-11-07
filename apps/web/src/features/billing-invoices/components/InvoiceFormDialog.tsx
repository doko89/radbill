import { useRef, useState } from "react";
import type { FormEvent } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { ApiError } from "../../../shared/lib/api";
import { formatIDR } from "../../../shared/lib/billing-format";
import { listPppoeOptions } from "../../../shared/lib/billing-refs";
import {
  Button,
  Dialog,
  Field,
  FormAlert,
  Input,
  Select,
  fieldError,
} from "../../../shared/ui/billing-ui";
import { createInvoice } from "../api";
import type { InvoiceCreateInput } from "../api";

type ItemRow = {
  key: number;
  description: string;
  quantity: string;
  price: string;
};

export function InvoiceFormDialog({ onClose, onSaved }: { onClose: () => void; onSaved: () => void }) {
  const [pppoeAccountId, setPppoeAccountId] = useState("");
  const [dueDate, setDueDate] = useState("");
  const [notes, setNotes] = useState("");
  const [rows, setRows] = useState<ItemRow[]>([{ key: 0, description: "", quantity: "1", price: "" }]);
  const [clientErrors, setClientErrors] = useState<Record<string, string>>({});
  const seq = useRef(1);

  const pppoe = useQuery({ queryKey: ["billing", "ref", "pppoe"], queryFn: listPppoeOptions });

  const mutation = useMutation({
    mutationFn: (input: InvoiceCreateInput) => createInvoice(input),
    onSuccess: onSaved,
  });

  const serverFields = mutation.error instanceof ApiError ? mutation.error.fields : undefined;
  const serverMessage =
    mutation.error instanceof ApiError
      ? mutation.error.message
      : mutation.error
        ? "gagal membuat tagihan"
        : null;

  function updateRow(key: number, patch: Partial<ItemRow>): void {
    setRows((current) => current.map((row) => (row.key === key ? { ...row, ...patch } : row)));
  }

  function addRow(): void {
    const key = seq.current;
    seq.current += 1;
    setRows((current) => [...current, { key, description: "", quantity: "1", price: "" }]);
  }

  function removeRow(key: number): void {
    setRows((current) => (current.length <= 1 ? current : current.filter((row) => row.key !== key)));
  }

  function previewTotal(): number {
    let total = 0;
    for (const row of rows) {
      const qty = Number(row.quantity);
      const price = Number(row.price);
      if (Number.isFinite(qty) && Number.isFinite(price) && qty > 0 && price >= 0) {
        total += qty * price;
      }
    }
    return total;
  }

  function onSubmit(event: FormEvent): void {
    event.preventDefault();
    const errors: Record<string, string> = {};
    if (pppoeAccountId === "") {
      errors["pppoe_account_id"] = "akun PPPoE wajib dipilih";
    }
    if (rows.length === 0) {
      errors["items"] = "minimal satu item";
    }
    rows.forEach((row, index) => {
      const label = `item ${index + 1}`;
      if (row.description.trim() === "") {
        errors[`row-${row.key}-description`] = `${label}: deskripsi wajib diisi`;
      }
      const qty = Number(row.quantity);
      if (!Number.isInteger(qty) || qty <= 0) {
        errors[`row-${row.key}-quantity`] = `${label}: jumlah harus bilangan bulat positif`;
      }
      const price = Number(row.price);
      if (row.price.trim() === "" || !Number.isFinite(price) || price < 0) {
        errors[`row-${row.key}-price`] = `${label}: harga harus angka ≥ 0`;
      }
    });
    if (Object.keys(errors).length > 0) {
      setClientErrors(errors);
      return;
    }
    setClientErrors({});
    mutation.mutate({
      pppoe_account_id: pppoeAccountId,
      due_date: dueDate === "" ? undefined : dueDate,
      notes: notes.trim() === "" ? undefined : notes.trim(),
      items: rows.map((row) => ({
        description: row.description.trim(),
        quantity: Number(row.quantity),
        price: Number(row.price),
      })),
    });
  }

  return (
    <Dialog
      open
      onClose={onClose}
      wide
      title="Tagihan baru"
      description="Tagihan dibuat per akun PPPoE (PRD §6.1)."
    >
      <form onSubmit={onSubmit} className="flex flex-col gap-4" noValidate>
        <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
          <Field
            label="Akun PPPoE"
            htmlFor="invoice-pppoe"
            required
            error={
              clientErrors["pppoe_account_id"] ?? fieldError(serverFields, "pppoe_account_id")
            }
          >
            <Select
              id="invoice-pppoe"
              value={pppoeAccountId}
              onChange={(e) => setPppoeAccountId(e.target.value)}
              disabled={pppoe.isPending}
              invalid={clientErrors["pppoe_account_id"] !== undefined}
            >
              <option value="">Pilih akun…</option>
              {(pppoe.data ?? []).map((option) => (
                <option key={option.id} value={option.id}>
                  {option.label}
                </option>
              ))}
            </Select>
          </Field>
          <Field label="Jatuh tempo" htmlFor="invoice-due">
            <Input
              id="invoice-due"
              type="date"
              value={dueDate}
              onChange={(e) => setDueDate(e.target.value)}
            />
          </Field>
        </div>

        <fieldset className="border border-slate-200 rounded-md p-4">
          <legend className="text-sm font-semibold px-1">Item tagihan</legend>
          <div className="flex flex-col gap-3">
            {rows.map((row, index) => (
              <div key={row.key} className="grid grid-cols-1 sm:grid-cols-[1fr_90px_140px_auto] gap-2 items-end">
                <Field label={index === 0 ? "Deskripsi" : `Deskripsi ${index + 1}`} htmlFor={`item-desc-${row.key}`} error={clientErrors[`row-${row.key}-description`]}>
                  <Input
                    id={`item-desc-${row.key}`}
                    value={row.description}
                    onChange={(e) => updateRow(row.key, { description: e.target.value })}
                    invalid={clientErrors[`row-${row.key}-description`] !== undefined}
                  />
                </Field>
                <Field label="Jml" htmlFor={`item-qty-${row.key}`} error={clientErrors[`row-${row.key}-quantity`]}>
                  <Input
                    id={`item-qty-${row.key}`}
                    inputMode="numeric"
                    value={row.quantity}
                    onChange={(e) => updateRow(row.key, { quantity: e.target.value })}
                    invalid={clientErrors[`row-${row.key}-quantity`] !== undefined}
                  />
                </Field>
                <Field label="Harga" htmlFor={`item-price-${row.key}`} error={clientErrors[`row-${row.key}-price`]}>
                  <Input
                    id={`item-price-${row.key}`}
                    inputMode="numeric"
                    value={row.price}
                    onChange={(e) => updateRow(row.key, { price: e.target.value })}
                    invalid={clientErrors[`row-${row.key}-price`] !== undefined}
                  />
                </Field>
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={() => removeRow(row.key)}
                  disabled={rows.length <= 1}
                  aria-label={`Hapus item ${index + 1}`}
                >
                  Hapus
                </Button>
              </div>
            ))}
            <div className="flex items-center justify-between gap-2">
              <Button variant="secondary" size="sm" onClick={addRow}>
                Tambah item
              </Button>
              <p className="m-0 text-sm font-bold" role="status">
                Total: {formatIDR(previewTotal())}
              </p>
            </div>
          </div>
        </fieldset>

        <Field label="Catatan (opsional)" htmlFor="invoice-notes">
          <Input
            id="invoice-notes"
            value={notes}
            onChange={(e) => setNotes(e.target.value)}
          />
        </Field>
        {pppoe.isError && <FormAlert message="Gagal memuat daftar akun PPPoE." />}
        {serverMessage && <FormAlert message={serverMessage} />}
        <div className="flex justify-end gap-2">
          <Button variant="secondary" onClick={onClose} disabled={mutation.isPending}>
            Batal
          </Button>
          <Button type="submit" disabled={mutation.isPending}>
            {mutation.isPending ? "Menyimpan…" : "Buat tagihan"}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}
