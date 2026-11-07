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
  fieldError,
} from "../../../shared/ui/billing-ui";
import { createNas, nasFormSchema, updateNas } from "../api";
import type { Nas, NasFormInput } from "../api";

export function NasDialog({
  initial,
  onClose,
  onSaved,
}: {
  initial: Nas | null;
  onClose: () => void;
  onSaved: () => void;
}) {
  const [nama, setNama] = useState(initial ? firstText(initial.nama, initial.name) : "");
  const [ip, setIp] = useState(
    initial ? firstText(initial.ip, initial.host, initial.address) : "",
  );
  const [secret, setSecret] = useState("");
  const [coaPort, setCoaPort] = useState(
    initial?.coa_port === null || initial?.coa_port === undefined
      ? "3799"
      : String(initial.coa_port),
  );
  const [clientErrors, setClientErrors] = useState<Record<string, string>>({});

  const mutation = useMutation({
    mutationFn: (input: NasFormInput) =>
      initial ? updateNas(initial.id, input) : createNas(input),
    onSuccess: onSaved,
  });

  const serverFields = mutation.error instanceof ApiError ? mutation.error.fields : undefined;
  const serverMessage =
    mutation.error instanceof ApiError
      ? mutation.error.message
      : mutation.error
        ? "gagal menyimpan NAS"
        : null;

  function onSubmit(event: FormEvent): void {
    event.preventDefault();
    const parsed = nasFormSchema.safeParse({ nama: nama.trim(), ip: ip.trim() });
    const errors = parsed.success ? {} : zodFieldErrors(parsed.error);
    const port = Number(coaPort.trim());
    if (coaPort.trim() !== "" && (!Number.isInteger(port) || port < 1 || port > 65535)) {
      errors["coa_port"] = "port harus 1–65535";
    }
    if (!initial && secret === "") {
      errors["secret"] = "shared secret wajib diisi untuk NAS baru";
    }
    if (Object.keys(errors).length > 0) {
      setClientErrors(errors);
      return;
    }
    setClientErrors({});
    mutation.mutate({
      nama: nama.trim(),
      ip: ip.trim(),
      secret: secret === "" ? undefined : secret,
      coa_port: coaPort.trim() === "" ? undefined : Number(coaPort.trim()),
    });
  }

  return (
    <Dialog
      open
      onClose={onClose}
      title={initial ? "Ubah NAS" : "NAS baru"}
      description="Router MikroTik sebagai RADIUS client. Secret tidak pernah ditampilkan ulang."
    >
      <form onSubmit={onSubmit} className="flex flex-col gap-4" noValidate>
        <Field
          label="Nama"
          htmlFor="nas-nama"
          required
          error={clientErrors["nama"] ?? fieldError(serverFields, "nama", "name")}
        >
          <Input
            id="nas-nama"
            value={nama}
            onChange={(e) => setNama(e.target.value)}
            invalid={clientErrors["nama"] !== undefined}
          />
        </Field>
        <Field
          label="IP router"
          htmlFor="nas-ip"
          required
          error={clientErrors["ip"] ?? fieldError(serverFields, "ip", "host", "address")}
        >
          <Input
            id="nas-ip"
            inputMode="decimal"
            placeholder="mis. 192.168.1.1"
            value={ip}
            onChange={(e) => setIp(e.target.value)}
            invalid={clientErrors["ip"] !== undefined}
          />
        </Field>
        <Field
          label={initial ? "Shared secret baru (opsional)" : "Shared secret"}
          htmlFor="nas-secret"
          required={!initial}
          hint={initial ? "Kosongkan jika tidak diubah." : undefined}
          error={clientErrors["secret"] ?? fieldError(serverFields, "secret")}
        >
          <Input
            id="nas-secret"
            type="password"
            autoComplete="new-password"
            value={secret}
            onChange={(e) => setSecret(e.target.value)}
            invalid={clientErrors["secret"] !== undefined}
          />
        </Field>
        <Field
          label="Port CoA"
          htmlFor="nas-coa-port"
          error={clientErrors["coa_port"] ?? fieldError(serverFields, "coa_port")}
        >
          <Input
            id="nas-coa-port"
            inputMode="numeric"
            value={coaPort}
            onChange={(e) => setCoaPort(e.target.value)}
            invalid={clientErrors["coa_port"] !== undefined}
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
