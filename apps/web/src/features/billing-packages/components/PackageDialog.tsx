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
  Select,
  fieldError,
} from "../../../shared/ui/billing-ui";
import {
  PACKAGE_TYPES,
  PACKAGE_TYPE_META,
  createPackage,
  packageFormSchema,
  packageTypeOf,
  updatePackage,
} from "../api";
import type { BillingPackage, PackageFormInput, PackageType } from "../api";

const MB = 1024 * 1024;
const GB = 1024 * 1024 * 1024;

function bytesToMB(bytes: number | null | undefined): string {
  if (bytes === null || bytes === undefined) {
    return "";
  }
  const mb = bytes / MB;
  return Number.isInteger(mb) ? String(mb) : mb.toFixed(2);
}

function secondsToDays(seconds: number | null | undefined): string {
  if (seconds === null || seconds === undefined) {
    return "";
  }
  return String(Math.round((seconds / 86400) * 100) / 100);
}

function secondsToHours(seconds: number | null | undefined): string {
  if (seconds === null || seconds === undefined) {
    return "";
  }
  return String(Math.round((seconds / 3600) * 100) / 100);
}

function priceToString(pkg: BillingPackage | null): string {
  const value = pkg?.harga ?? pkg?.price ?? null;
  return value === null || value === undefined ? "" : String(value);
}

function parsePositive(raw: string): number | null {
  const trimmed = raw.trim().replace(",", ".");
  if (trimmed === "") {
    return null;
  }
  const value = Number(trimmed);
  if (!Number.isFinite(value) || value < 0) {
    return Number.NaN;
  }
  return value;
}

function hasValue(bytes: number | null | undefined): boolean {
  return bytes !== null && bytes !== undefined;
}

export function PackageDialog({
  initial,
  onClose,
  onSaved,
}: {
  initial: BillingPackage | null;
  onClose: () => void;
  onSaved: () => void;
}) {
  const [nama, setNama] = useState(initial ? firstText(initial.nama, initial.name) : "");
  const [pkgType, setPkgType] = useState<PackageType>(initial ? packageTypeOf(initial) : "quota");
  const [harga, setHarga] = useState(priceToString(initial));
  const [periode, setPeriode] = useState(initial ? firstText(initial.periode, initial.period) : "");
  const [rateLimit, setRateLimit] = useState(initial?.rate_limit ?? "");
  const [addressPool, setAddressPool] = useState(initial?.address_pool ?? "");
  const [validityDays, setValidityDays] = useState(secondsToDays(initial?.validity_seconds));
  const [onlineHours, setOnlineHours] = useState(secondsToHours(initial?.online_seconds_limit));
  const [quotaValue, setQuotaValue] = useState(bytesToMB(initial?.quota_bytes));
  const [quotaUnit, setQuotaUnit] = useState("MB");
  const [quotaReset, setQuotaReset] = useState(initial?.quota_reset ?? "");
  const [fupValue, setFupValue] = useState(bytesToMB(initial?.fup_threshold_bytes));
  const [fupUnit, setFupUnit] = useState("MB");
  const [fupRateLimit, setFupRateLimit] = useState(initial?.fup_rate_limit ?? "");
  const [onExhausted, setOnExhausted] = useState(initial?.on_exhausted ?? "");
  const [isPublic, setIsPublic] = useState(initial?.is_public ?? false);
  const [allowSelfService, setAllowSelfService] = useState(initial?.allow_self_service ?? false);
  const [upgradeGroup, setUpgradeGroup] = useState(initial?.upgrade_group ?? "");
  const [clientErrors, setClientErrors] = useState<Record<string, string>>({});

  const mutation = useMutation({
    mutationFn: (input: PackageFormInput) =>
      initial ? updatePackage(initial.id, input) : createPackage(input),
    onSuccess: onSaved,
  });

  const serverFields = mutation.error instanceof ApiError ? mutation.error.fields : undefined;
  const serverMessage =
    mutation.error instanceof ApiError
      ? mutation.error.message
      : mutation.error
        ? "gagal menyimpan paket"
        : null;

  function switchType(next: PackageType): void {
    setPkgType(next);
    setClientErrors({});
    // Kosongkan field khas tipe lain agar tidak ikut tersimpan.
    if (next !== "time") {
      setOnlineHours("");
    }
    if (next !== "quota") {
      setQuotaValue("");
    }
    if (next !== "fup") {
      setFupValue("");
      setFupRateLimit("");
    }
  }

  /** Field bawaan paket yang akan ikut terhapus karena tak relevan dengan tipe. */
  function droppedLabels(): string[] {
    if (!initial) {
      return [];
    }
    const labels: string[] = [];
    if (pkgType !== "time" && hasValue(initial.online_seconds_limit)) {
      labels.push("Jatah online");
    }
    if (pkgType !== "quota" && hasValue(initial.quota_bytes)) {
      labels.push("Kuota");
    }
    if (pkgType !== "fup" && hasValue(initial.fup_threshold_bytes)) {
      labels.push("Ambang FUP");
    }
    if (pkgType !== "fup" && (initial.fup_rate_limit ?? "") !== "") {
      labels.push("Rate limit FUP");
    }
    return labels;
  }

  function onSubmit(event: FormEvent): void {
    event.preventDefault();
    const errors: Record<string, string> = {};
    const parsed = packageFormSchema.safeParse({ nama: nama.trim() });
    if (!parsed.success) {
      Object.assign(errors, zodFieldErrors(parsed.error));
    }
    const hargaValue = parsePositive(harga);
    if (Number.isNaN(hargaValue)) {
      errors["harga"] = "harga harus angka ≥ 0";
    }
    const validityValue = parsePositive(validityDays);
    if (Number.isNaN(validityValue)) {
      errors["validity_days"] = "masa berlaku harus angka ≥ 0";
    }
    const onlineValue = parsePositive(onlineHours);
    if (Number.isNaN(onlineValue)) {
      errors["online_hours"] = "jatah online harus angka ≥ 0";
    } else if (pkgType === "time" && onlineValue === null) {
      errors["online_hours"] = "jatah online wajib diisi untuk time based";
    }
    const quotaParsed = parsePositive(quotaValue);
    if (Number.isNaN(quotaParsed)) {
      errors["quota_value"] = "kuota harus angka ≥ 0";
    } else if (pkgType === "quota" && quotaParsed === null) {
      errors["quota_value"] = "kuota wajib diisi untuk kuota based";
    }
    const fupParsed = parsePositive(fupValue);
    if (Number.isNaN(fupParsed)) {
      errors["fup_value"] = "ambang FUP harus angka ≥ 0";
    } else if (pkgType === "fup" && fupParsed === null) {
      errors["fup_value"] = "ambang FUP wajib diisi untuk FUP based";
    }
    if (pkgType === "fup" && fupRateLimit.trim() === "") {
      errors["fup_rate_limit"] = "rate limit FUP wajib diisi untuk FUP based";
    }
    if (Object.keys(errors).length > 0) {
      setClientErrors(errors);
      return;
    }
    setClientErrors({});

    const quotaBytes =
      quotaParsed === null ? undefined : Math.round(quotaParsed * (quotaUnit === "GB" ? GB : MB));
    const fupBytes =
      fupParsed === null ? undefined : Math.round(fupParsed * (fupUnit === "GB" ? GB : MB));
    mutation.mutate({
      nama: nama.trim(),
      type: pkgType,
      harga: hargaValue === null ? undefined : Math.round(hargaValue),
      periode: periode.trim() === "" ? undefined : periode.trim(),
      rate_limit: rateLimit.trim() === "" ? undefined : rateLimit.trim(),
      address_pool: addressPool.trim() === "" ? undefined : addressPool.trim(),
      validity_seconds: validityValue === null ? undefined : Math.round(validityValue * 86400),
      online_seconds_limit:
        pkgType === "time" && onlineValue !== null ? Math.round(onlineValue * 3600) : undefined,
      quota_bytes: pkgType === "quota" ? quotaBytes : undefined,
      quota_reset: pkgType === "time" || quotaReset === "" ? undefined : quotaReset,
      fup_threshold_bytes: pkgType === "fup" ? fupBytes : undefined,
      fup_rate_limit:
        pkgType === "fup" && fupRateLimit.trim() !== "" ? fupRateLimit.trim() : undefined,
      on_exhausted: pkgType === "time" || onExhausted === "" ? undefined : onExhausted,
      is_public: isPublic,
      allow_self_service: allowSelfService,
      upgrade_group: upgradeGroup.trim() === "" ? undefined : upgradeGroup.trim(),
    });
  }

  const dropped = droppedLabels();

  return (
    <Dialog
      open
      onClose={onClose}
      wide
      title={initial ? "Ubah paket" : "Paket baru"}
      description="Isi nama, harga, dan tipe — field batas mengikuti tipe yang dipilih."
    >
      <form onSubmit={onSubmit} className="flex flex-col gap-4" noValidate>
        <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
          <Field
            label="Nama paket"
            htmlFor="package-nama"
            required
            error={clientErrors["nama"] ?? fieldError(serverFields, "nama", "name")}
          >
            <Input
              id="package-nama"
              value={nama}
              onChange={(e) => setNama(e.target.value)}
              invalid={clientErrors["nama"] !== undefined}
            />
          </Field>
          <Field
            label="Harga (Rp)"
            htmlFor="package-harga"
            error={clientErrors["harga"] ?? fieldError(serverFields, "harga", "price")}
          >
            <Input
              id="package-harga"
              inputMode="numeric"
              placeholder="mis. 100000"
              value={harga}
              onChange={(e) => setHarga(e.target.value)}
              invalid={clientErrors["harga"] !== undefined}
            />
          </Field>
          <fieldset className="m-0 border-0 p-0 sm:col-span-2">
            <legend className="px-0 text-sm font-semibold text-slate-900">
              Tipe paket <span className="text-red-600" aria-hidden="true">*</span>
            </legend>
            <div className="mt-2 grid gap-3 sm:grid-cols-3" role="radiogroup" aria-label="Tipe paket">
              {PACKAGE_TYPES.map((value) => {
                const meta = PACKAGE_TYPE_META[value];
                const checked = pkgType === value;
                return (
                  <label
                    key={value}
                    className={`flex gap-3 rounded-md border p-3 cursor-pointer ${
                      checked
                        ? "border-emerald-700 bg-emerald-50"
                        : "border-slate-300 bg-white hover:bg-slate-50"
                    }`}
                  >
                    <input
                      type="radio"
                      name="package_type"
                      value={value}
                      checked={checked}
                      onChange={() => switchType(value)}
                      className="mt-1 accent-emerald-700 size-4 shrink-0"
                    />
                    <span>
                      <span className="block text-sm font-bold text-slate-900">{meta.title}</span>
                      <span className="mt-1 block text-sm text-slate-500">{meta.desc}</span>
                    </span>
                  </label>
                );
              })}
            </div>
          </fieldset>
          {dropped.length > 0 && (
            <div className="sm:col-span-2">
              <FormNote
                message={`Menyimpan sebagai ${PACKAGE_TYPE_META[pkgType].title} akan mengosongkan: ${dropped.join(", ")}.`}
              />
            </div>
          )}
          <Field label="Periode" htmlFor="package-periode" hint="mis. monthly">
            <Input
              id="package-periode"
              value={periode}
              onChange={(e) => setPeriode(e.target.value)}
            />
          </Field>
          <Field label="Rate limit" htmlFor="package-rate-limit" hint="mis. 10M/10M">
            <Input
              id="package-rate-limit"
              value={rateLimit}
              onChange={(e) => setRateLimit(e.target.value)}
            />
          </Field>
          <Field label="Address pool" htmlFor="package-address-pool">
            <Input
              id="package-address-pool"
              value={addressPool}
              onChange={(e) => setAddressPool(e.target.value)}
            />
          </Field>
          <Field
            label="Masa berlaku (hari)"
            htmlFor="package-validity"
            error={clientErrors["validity_days"]}
          >
            <Input
              id="package-validity"
              inputMode="decimal"
              placeholder="kosong = tanpa batas"
              value={validityDays}
              onChange={(e) => setValidityDays(e.target.value)}
              invalid={clientErrors["validity_days"] !== undefined}
            />
          </Field>
          {pkgType === "time" && (
            <Field
              label="Jatah online (jam)"
              htmlFor="package-online"
              required
              error={clientErrors["online_hours"]}
            >
              <Input
                id="package-online"
                inputMode="decimal"
                placeholder="mis. 100"
                value={onlineHours}
                onChange={(e) => setOnlineHours(e.target.value)}
                invalid={clientErrors["online_hours"] !== undefined}
              />
            </Field>
          )}
          {pkgType === "quota" && (
            <Field
              label="Kuota"
              htmlFor="package-quota"
              required
              error={clientErrors["quota_value"]}
            >
              <div className="flex gap-2">
                <Input
                  id="package-quota"
                  inputMode="decimal"
                  placeholder="mis. 50"
                  value={quotaValue}
                  onChange={(e) => setQuotaValue(e.target.value)}
                  invalid={clientErrors["quota_value"] !== undefined}
                />
                <label htmlFor="package-quota-unit" className="sr-only">
                  Satuan kuota
                </label>
                <Select
                  id="package-quota-unit"
                  value={quotaUnit}
                  onChange={(e) => setQuotaUnit(e.target.value)}
                  className="max-w-28"
                >
                  <option value="MB">MB</option>
                  <option value="GB">GB</option>
                </Select>
              </div>
            </Field>
          )}
          {pkgType === "fup" && (
            <Field
              label="Ambang FUP"
              htmlFor="package-fup"
              required
              error={clientErrors["fup_value"]}
            >
              <div className="flex gap-2">
                <Input
                  id="package-fup"
                  inputMode="decimal"
                  placeholder="mis. 100"
                  value={fupValue}
                  onChange={(e) => setFupValue(e.target.value)}
                  invalid={clientErrors["fup_value"] !== undefined}
                />
                <label htmlFor="package-fup-unit" className="sr-only">
                  Satuan ambang FUP
                </label>
                <Select
                  id="package-fup-unit"
                  value={fupUnit}
                  onChange={(e) => setFupUnit(e.target.value)}
                  className="max-w-28"
                >
                  <option value="MB">MB</option>
                  <option value="GB">GB</option>
                </Select>
              </div>
            </Field>
          )}
          {pkgType === "fup" && (
            <Field
              label="Rate limit FUP"
              htmlFor="package-fup-rate"
              required
              hint="kecepatan setelah ambang, mis. 1M/1M"
              error={
                clientErrors["fup_rate_limit"] ?? fieldError(serverFields, "fup_rate_limit")
              }
            >
              <Input
                id="package-fup-rate"
                value={fupRateLimit}
                onChange={(e) => setFupRateLimit(e.target.value)}
                invalid={clientErrors["fup_rate_limit"] !== undefined}
              />
            </Field>
          )}
          {pkgType !== "time" && (
            <Field label="Reset kuota" htmlFor="package-quota-reset">
              <Select
                id="package-quota-reset"
                value={quotaReset}
                onChange={(e) => setQuotaReset(e.target.value)}
              >
                <option value="">Tidak diatur</option>
                <option value="none">none</option>
                <option value="daily">daily</option>
                <option value="monthly">monthly</option>
                <option value="billing_cycle">billing_cycle</option>
              </Select>
            </Field>
          )}
          {pkgType !== "time" && (
            <Field label="Saat jatah habis" htmlFor="package-on-exhausted">
              <Select
                id="package-on-exhausted"
                value={onExhausted}
                onChange={(e) => setOnExhausted(e.target.value)}
              >
                <option value="">Tidak diatur</option>
                <option value="reject">reject</option>
                <option value="isolate">isolate</option>
                <option value="throttle">throttle</option>
              </Select>
            </Field>
          )}
          <Field label="Upgrade group" htmlFor="package-upgrade-group">
            <Input
              id="package-upgrade-group"
              value={upgradeGroup}
              onChange={(e) => setUpgradeGroup(e.target.value)}
            />
          </Field>
        </div>
        <fieldset className="border border-slate-200 rounded-md p-4">
          <legend className="text-sm font-semibold px-1">Visibilitas portal</legend>
          <div className="flex flex-col gap-2">
            <label htmlFor="package-is-public" className="flex items-center gap-2 text-sm">
              <input
                id="package-is-public"
                type="checkbox"
                checked={isPublic}
                onChange={(e) => setIsPublic(e.target.checked)}
                className="size-4 accent-emerald-700"
              />
              Tampilkan di portal (is_public)
            </label>
            <label htmlFor="package-self-service" className="flex items-center gap-2 text-sm">
              <input
                id="package-self-service"
                type="checkbox"
                checked={allowSelfService}
                onChange={(e) => setAllowSelfService(e.target.checked)}
                className="size-4 accent-emerald-700"
              />
              Boleh pindah paket mandiri (allow_self_service)
            </label>
          </div>
        </fieldset>
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
