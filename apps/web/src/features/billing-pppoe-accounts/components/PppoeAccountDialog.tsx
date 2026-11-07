import { useState } from "react";
import type { FormEvent } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { Eye, EyeOff } from "lucide-react";
import { ApiError } from "../../../shared/lib/api";
import { zodFieldErrors } from "../../../shared/lib/billing-forms";
import {
  listCustomerOptions,
  listNasOptions,
  listPackageOptions,
} from "../../../shared/lib/billing-refs";
import {
  Button,
  Dialog,
  Field,
  FormAlert,
  Input,
  Select,
  fieldError,
} from "../../../shared/ui/billing-ui";
import { createPppoeAccount, pppoeFormSchema, updatePppoeAccount } from "../api";
import type { PppoeAccount, PppoeFormInput } from "../api";

function toDateInput(value: string | null | undefined): string {
  if (!value) {
    return "";
  }
  return value.length >= 10 ? value.slice(0, 10) : value;
}

export function PppoeAccountDialog({
  initial,
  onClose,
  onSaved,
}: {
  initial: PppoeAccount | null;
  onClose: () => void;
  onSaved: () => void;
}) {
  const [customerId, setCustomerId] = useState(initial?.customer_id ?? "");
  const [packageId, setPackageId] = useState(initial?.package_id ?? "");
  const [nasId, setNasId] = useState(initial?.nas_id ?? "");
  const [username, setUsername] = useState(initial?.username ?? "");
  const [password, setPassword] = useState("");
  const [showPassword, setShowPassword] = useState(false);
  const [status, setStatus] = useState(initial?.status ?? "active");
  const [expiresAt, setExpiresAt] = useState(toDateInput(initial?.expires_at));
  const [staticIp, setStaticIp] = useState(initial?.static_ip ?? "");
  const [clientErrors, setClientErrors] = useState<Record<string, string>>({});

  const customers = useQuery({ queryKey: ["billing", "ref", "customers"], queryFn: listCustomerOptions });
  const packages = useQuery({ queryKey: ["billing", "ref", "packages"], queryFn: listPackageOptions });
  const nasList = useQuery({ queryKey: ["billing", "ref", "nas"], queryFn: listNasOptions });

  const mutation = useMutation({
    mutationFn: (input: PppoeFormInput) =>
      initial ? updatePppoeAccount(initial.id, input) : createPppoeAccount(input),
    onSuccess: onSaved,
  });

  const serverFields = mutation.error instanceof ApiError ? mutation.error.fields : undefined;
  const serverMessage =
    mutation.error instanceof ApiError
      ? mutation.error.message
      : mutation.error
        ? "gagal menyimpan akun PPPoE"
        : null;

  function onSubmit(event: FormEvent): void {
    event.preventDefault();
    const parsed = pppoeFormSchema.safeParse({
      customer_id: customerId,
      package_id: packageId,
      username: username.trim(),
      static_ip: staticIp.trim(),
      expires_at: expiresAt,
    });
    const errors = parsed.success ? {} : zodFieldErrors(parsed.error);
    if (!initial && password.trim() === "") {
      errors["password"] = "kata sandi wajib diisi untuk akun baru";
    }
    if (Object.keys(errors).length > 0) {
      setClientErrors(errors);
      return;
    }
    setClientErrors({});
    mutation.mutate({
      customer_id: customerId,
      package_id: packageId,
      nas_id: nasId === "" ? undefined : nasId,
      username: username.trim(),
      password: password === "" ? undefined : password,
      status,
      expires_at: expiresAt === "" ? undefined : expiresAt,
      static_ip: staticIp.trim() === "" ? undefined : staticIp.trim(),
    });
  }

  return (
    <Dialog
      open
      onClose={onClose}
      wide
      title={initial ? "Ubah akun PPPoE" : "Akun PPPoE baru"}
      description="Kredensial PPPoE terpisah dari login portal (PRD §5.2)."
    >
      <form onSubmit={onSubmit} className="flex flex-col gap-4" noValidate>
        <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
          <Field
            label="Pelanggan"
            htmlFor="pppoe-customer"
            required
            error={clientErrors["customer_id"] ?? fieldError(serverFields, "customer_id")}
          >
            <Select
              id="pppoe-customer"
              value={customerId}
              onChange={(e) => setCustomerId(e.target.value)}
              disabled={customers.isPending}
              invalid={clientErrors["customer_id"] !== undefined}
            >
              <option value="">Pilih pelanggan…</option>
              {(customers.data ?? []).map((option) => (
                <option key={option.id} value={option.id}>
                  {option.label}
                </option>
              ))}
            </Select>
          </Field>
          <Field
            label="Paket"
            htmlFor="pppoe-package"
            required
            error={clientErrors["package_id"] ?? fieldError(serverFields, "package_id")}
          >
            <Select
              id="pppoe-package"
              value={packageId}
              onChange={(e) => setPackageId(e.target.value)}
              disabled={packages.isPending}
              invalid={clientErrors["package_id"] !== undefined}
            >
              <option value="">Pilih paket…</option>
              {(packages.data ?? []).map((option) => (
                <option key={option.id} value={option.id}>
                  {option.label}
                </option>
              ))}
            </Select>
          </Field>
          <Field label="NAS (opsional)" htmlFor="pppoe-nas">
            <Select
              id="pppoe-nas"
              value={nasId}
              onChange={(e) => setNasId(e.target.value)}
              disabled={nasList.isPending}
            >
              <option value="">Semua NAS…</option>
              {(nasList.data ?? []).map((option) => (
                <option key={option.id} value={option.id}>
                  {option.label}
                </option>
              ))}
            </Select>
          </Field>
          <Field
            label="Username PPPoE"
            htmlFor="pppoe-username"
            required
            error={clientErrors["username"] ?? fieldError(serverFields, "username")}
          >
            <Input
              id="pppoe-username"
              autoComplete="off"
              value={username}
              onChange={(e) => setUsername(e.target.value)}
              invalid={clientErrors["username"] !== undefined}
            />
          </Field>
          <Field
            label={initial ? "Sandi baru (opsional)" : "Kata sandi PPPoE"}
            htmlFor="pppoe-password"
            required={!initial}
            hint={initial ? "Kosongkan jika tidak diubah." : undefined}
            error={clientErrors["password"] ?? fieldError(serverFields, "password")}
          >
            <div className="flex gap-2">
              <Input
                id="pppoe-password"
                type={showPassword ? "text" : "password"}
                autoComplete="new-password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                invalid={clientErrors["password"] !== undefined}
              />
              <Button
                variant="secondary"
                size="sm"
                onClick={() => setShowPassword((show) => !show)}
                aria-label={showPassword ? "Sembunyikan sandi" : "Tampilkan sandi"}
                aria-pressed={showPassword}
              >
                {showPassword ? (
                  <EyeOff className="size-4" aria-hidden="true" />
                ) : (
                  <Eye className="size-4" aria-hidden="true" />
                )}
              </Button>
            </div>
          </Field>
          <Field label="Status" htmlFor="pppoe-status">
            <Select id="pppoe-status" value={status} onChange={(e) => setStatus(e.target.value)}>
              <option value="active">active</option>
              <option value="isolated">isolated</option>
              <option value="disabled">disabled</option>
            </Select>
          </Field>
          <Field label="Berakhir pada" htmlFor="pppoe-expires">
            <Input
              id="pppoe-expires"
              type="date"
              value={expiresAt}
              onChange={(e) => setExpiresAt(e.target.value)}
            />
          </Field>
          <Field label="IP statis (opsional)" htmlFor="pppoe-static-ip">
            <Input
              id="pppoe-static-ip"
              inputMode="decimal"
              placeholder="mis. 10.0.0.5"
              value={staticIp}
              onChange={(e) => setStaticIp(e.target.value)}
            />
          </Field>
        </div>
        {(customers.isError || packages.isError || nasList.isError) && (
          <FormAlert message="Gagal memuat sebagian daftar referensi (pelanggan/paket/NAS)." />
        )}
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
