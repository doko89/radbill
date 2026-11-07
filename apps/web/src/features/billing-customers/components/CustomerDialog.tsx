import { useState } from "react";
import type { FormEvent } from "react";
import { useMutation } from "@tanstack/react-query";
import { ApiError } from "../../../shared/lib/api";
import { zodFieldErrors } from "../../../shared/lib/billing-forms";
import { firstText } from "../../../shared/lib/billing-format";
import {
  Button,
  Dialog,
  Field,
  FormAlert,
  Input,
  Textarea,
  fieldError,
} from "../../../shared/ui/billing-ui";
import { createCustomer, customerFormSchema, updateCustomer } from "../api";
import type { Customer, CustomerFormInput } from "../api";

function toInput(value: string): string | undefined {
  const trimmed = value.trim();
  return trimmed.length > 0 ? trimmed : undefined;
}

export function CustomerDialog({
  initial,
  onClose,
  onSaved,
}: {
  initial: Customer | null;
  onClose: () => void;
  onSaved: () => void;
}) {
  const [nama, setNama] = useState(initial ? firstText(initial.nama, initial.name) : "");
  const [telp, setTelp] = useState(
    initial ? firstText(initial.telp, initial.telepon, initial.phone) : "",
  );
  const [alamat, setAlamat] = useState(initial ? firstText(initial.alamat, initial.address) : "");
  const [catatan, setCatatan] = useState(
    initial ? firstText(initial.catatan, initial.notes) : "",
  );
  const [clientErrors, setClientErrors] = useState<Record<string, string>>({});

  const mutation = useMutation({
    mutationFn: (input: CustomerFormInput) =>
      initial ? updateCustomer(initial.id, input) : createCustomer(input),
    onSuccess: onSaved,
  });

  const serverFields = mutation.error instanceof ApiError ? mutation.error.fields : undefined;
  const serverMessage =
    mutation.error instanceof ApiError
      ? mutation.error.message
      : mutation.error
        ? "gagal menyimpan pelanggan"
        : null;

  function onSubmit(event: FormEvent): void {
    event.preventDefault();
    const parsed = customerFormSchema.safeParse({ nama: nama.trim(), telp, alamat, catatan });
    if (!parsed.success) {
      setClientErrors(zodFieldErrors(parsed.error));
      return;
    }
    setClientErrors({});
    mutation.mutate({
      nama: parsed.data.nama,
      telp: toInput(parsed.data.telp ?? ""),
      alamat: toInput(parsed.data.alamat ?? ""),
      catatan: toInput(parsed.data.catatan ?? ""),
    });
  }

  return (
    <Dialog
      open
      onClose={onClose}
      title={initial ? "Ubah pelanggan" : "Pelanggan baru"}
      description="Data kontak pelanggan untuk penagihan dan notifikasi."
    >
      <form onSubmit={onSubmit} className="flex flex-col gap-4" noValidate>
        <Field
          label="Nama"
          htmlFor="customer-nama"
          required
          error={clientErrors["nama"] ?? fieldError(serverFields, "nama", "name")}
        >
          <Input
            id="customer-nama"
            autoComplete="name"
            value={nama}
            onChange={(e) => setNama(e.target.value)}
            invalid={clientErrors["nama"] !== undefined}
          />
        </Field>
        <Field
          label="Telepon"
          htmlFor="customer-telp"
          error={clientErrors["telp"] ?? fieldError(serverFields, "telp", "telepon", "phone")}
        >
          <Input
            id="customer-telp"
            autoComplete="tel"
            inputMode="tel"
            value={telp}
            onChange={(e) => setTelp(e.target.value)}
            invalid={clientErrors["telp"] !== undefined}
          />
        </Field>
        <Field
          label="Alamat"
          htmlFor="customer-alamat"
          error={clientErrors["alamat"] ?? fieldError(serverFields, "alamat", "address")}
        >
          <Textarea
            id="customer-alamat"
            rows={2}
            value={alamat}
            onChange={(e) => setAlamat(e.target.value)}
            invalid={clientErrors["alamat"] !== undefined}
          />
        </Field>
        <Field
          label="Catatan"
          htmlFor="customer-catatan"
          error={clientErrors["catatan"] ?? fieldError(serverFields, "catatan", "notes")}
        >
          <Textarea
            id="customer-catatan"
            rows={2}
            value={catatan}
            onChange={(e) => setCatatan(e.target.value)}
            invalid={clientErrors["catatan"] !== undefined}
          />
        </Field>
        {serverMessage && <FormAlert message={serverMessage} />}
        <div className="flex justify-end gap-2">
          <Button variant="secondary" onClick={onClose} disabled={mutation.isPending}>
            Batal
          </Button>
          <Button type="submit" disabled={mutation.isPending}>
            {mutation.isPending ? "Menyimpan…" : "Simpan"}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}
