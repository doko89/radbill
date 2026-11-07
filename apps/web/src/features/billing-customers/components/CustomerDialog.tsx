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
  FormNote,
  Input,
  Textarea,
  fieldError,
} from "../../../shared/ui/billing-ui";
import {
  createCustomer,
  createCustomerPortalAccount,
  customerFormSchema,
  resetCustomerPortalAccount,
  updateCustomer,
} from "../api";
import type { Customer, CustomerFormInput } from "../api";
import type { PortalCredential } from "../../../shared/lib/portal-credential";

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
  const [portalUsername, setPortalUsername] = useState("");
  const [portalPassword, setPortalPassword] = useState("");
  const [credential, setCredential] = useState<PortalCredential | null>(null);
  const [portalNote, setPortalNote] = useState<string | null>(null);
  const [portalError, setPortalError] = useState<string | null>(null);

  const mutation = useMutation({
    mutationFn: (input: CustomerFormInput) =>
      initial ? updateCustomer(initial.id, input) : createCustomer(input),
    onSuccess: onSaved,
  });

  const portalMutation = useMutation({
    mutationFn: (input: { mode: "create" | "reset"; username: string; password: string }) => {
      if (!initial) {
        throw new Error("no customer");
      }
      return input.mode === "create"
        ? createCustomerPortalAccount(initial.id, {
            username: input.username,
            password: input.password,
          })
        : resetCustomerPortalAccount(initial.id, { password: input.password });
    },
    onSuccess: (data) => {
      setCredential(data);
      setPortalPassword("");
      setPortalNote("Akun portal berhasil diproses. Salin kredensial sekali ini.");
      setPortalError(null);
    },
    onError: (error: unknown) => {
      setPortalError(error instanceof ApiError ? error.message : "akun portal gagal");
      setPortalNote(null);
    },
  });

  function onPortal(mode: "create" | "reset"): void {
    const username = portalUsername.trim();
    if (mode === "create" && username === "") {
      setPortalError("username portal wajib diisi");
      setPortalNote(null);
      return;
    }
    if (portalPassword !== "" && portalPassword.length < 8) {
      setPortalError("sandi portal minimal 8 karakter (kosongkan untuk acak)");
      setPortalNote(null);
      return;
    }
    portalMutation.mutate({ mode, username, password: portalPassword });
  }

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
      {initial ? (
        <div className="mt-6 border-t border-slate-200 pt-4">
          <h3 className="m-0 mb-1 text-sm font-bold text-slate-800">Akun login portal</h3>
          <p className="mt-0 mb-3 text-xs text-slate-500">
            {initial.account_id ? "Pelanggan sudah punya login portal." : "Belum punya login portal."}{" "}
            Sandi kosong = dibuat acak oleh server.
          </p>
          <div className="grid grid-cols-1 sm:grid-cols-2 gap-3 mb-3">
            <Field label="Username portal" htmlFor="customer-portal-username">
              <Input
                id="customer-portal-username"
                autoComplete="username"
                placeholder="mis. 0812xxxx"
                value={portalUsername}
                onChange={(e) => setPortalUsername(e.target.value)}
              />
            </Field>
            <Field label="Sandi portal" htmlFor="customer-portal-password">
              <Input
                id="customer-portal-password"
                type="password"
                autoComplete="new-password"
                placeholder="min. 8 karakter / kosong = acak"
                value={portalPassword}
                onChange={(e) => setPortalPassword(e.target.value)}
              />
            </Field>
          </div>
          <div className="flex flex-wrap gap-2">
            <Button
              variant="secondary"
              size="sm"
              disabled={portalMutation.isPending}
              onClick={() => onPortal("create")}
            >
              {portalMutation.isPending && portalMutation.variables?.mode === "create"
                ? "Memproses…"
                : "Buat akun portal"}
            </Button>
            <Button
              variant="secondary"
              size="sm"
              disabled={portalMutation.isPending}
              onClick={() => onPortal("reset")}
            >
              {portalMutation.isPending && portalMutation.variables?.mode === "reset"
                ? "Memproses…"
                : "Reset sandi portal"}
            </Button>
          </div>
          {credential && (
            <div className="grid grid-cols-1 sm:grid-cols-2 gap-3 mt-3">
              <Field label="Username" htmlFor="customer-portal-result-username">
                <Input
                  id="customer-portal-result-username"
                  readOnly
                  value={credential.username ?? "-"}
                />
              </Field>
              <Field label="Sandi" htmlFor="customer-portal-result-password">
                <Input
                  id="customer-portal-result-password"
                  readOnly
                  value={credential.password ?? "-"}
                  className="font-mono"
                />
              </Field>
            </div>
          )}
          {portalNote && (
            <div className="mt-3">
              <FormNote message={portalNote} />
            </div>
          )}
          {portalError && (
            <div className="mt-3">
              <FormAlert message={portalError} />
            </div>
          )}
        </div>
      ) : (
        <p className="text-xs text-slate-500 mt-4 mb-0">
          Login portal diatur setelah pelanggan disimpan (tombol Ubah).
        </p>
      )}
    </Dialog>
  );
}
