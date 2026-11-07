import { useEffect, useRef, useState } from "react";
import type { FormEvent } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Check, Copy } from "lucide-react";
import { ApiError } from "../../../shared/lib/api";
import {
  Button,
  Card,
  EmptyState,
  Field,
  FormAlert,
  FormNote,
  Input,
  PageHeader,
  Select,
  Textarea,
  fieldError,
} from "../../../shared/ui/billing-ui";
import {
  KRISKRIS_MODES,
  KRISKRIS_MODE_META,
  MASKED,
  SETTING_SECTIONS,
  kriskrisWebhookUrl,
  listSettings,
  updateSettings,
} from "../api";
import type { KriskrisMode } from "../api";

function isMode(value: string | undefined): value is KriskrisMode {
  return value === "webhook" || value === "poll";
}

export function SettingsPage() {
  const queryClient = useQueryClient();
  const [draft, setDraft] = useState<Record<string, string>>({});
  const [secretDraft, setSecretDraft] = useState<Record<string, string>>({});
  const [formError, setFormError] = useState<string | null>(null);
  const [serverFields, setServerFields] = useState<Record<string, string> | undefined>(undefined);
  const [savedNote, setSavedNote] = useState<string | null>(null);
  const [copied, setCopied] = useState(false);
  const urlRef = useRef<HTMLInputElement>(null);

  const settings = useQuery({
    queryKey: ["billing", "settings"],
    queryFn: listSettings,
  });

  useEffect(() => {
    if (settings.data !== undefined) {
      const next: Record<string, string> = {};
      for (const [key, value] of Object.entries(settings.data)) {
        if (value !== MASKED) next[key] = value;
      }
      setDraft(next);
      setSecretDraft({});
    }
  }, [settings.data]);

  const save = useMutation({
    mutationFn: updateSettings,
    onSuccess: () => {
      setFormError(null);
      setServerFields(undefined);
      setSavedNote("Pengaturan tersimpan.");
      void queryClient.invalidateQueries({ queryKey: ["billing", "settings"] });
    },
    onError: (err: unknown) => {
      setSavedNote(null);
      setFormError(err instanceof ApiError ? err.message : "Gagal menyimpan pengaturan.");
      setServerFields(err instanceof ApiError ? err.fields : undefined);
    },
  });

  if (settings.isPending) {
    return (
      <div>
        <PageHeader title="Pengaturan" description="Konfigurasi billing, kriskris, dan notifikasi." />
        <Card>
          <p className="m-0 text-sm text-slate-500">Memuat…</p>
        </Card>
      </div>
    );
  }

  if (settings.isError || settings.data === undefined) {
    return (
      <div>
        <PageHeader title="Pengaturan" description="Konfigurasi billing, kriskris, dan notifikasi." />
        <Card>
          <EmptyState title="Gagal memuat pengaturan" hint="Coba muat ulang halaman." />
        </Card>
      </div>
    );
  }

  const stored = settings.data;
  const modeRaw = draft["kriskris_mode"] ?? stored["kriskris_mode"];
  const mode: KriskrisMode = isMode(modeRaw) ? modeRaw : "webhook";
  const webhookUrl = kriskrisWebhookUrl(window.location.origin);

  async function copyWebhookUrl(): Promise<void> {
    try {
      await navigator.clipboard.writeText(webhookUrl);
      setCopied(true);
    } catch {
      // Clipboard API tak tersedia (non-HTTPS): blok URL agar mudah disalin manual.
      urlRef.current?.select();
      setCopied(false);
    }
  }

  function setValue(key: string, value: string): void {
    setSavedNote(null);
    setDraft((prev) => ({ ...prev, [key]: value }));
  }

  function onSubmit(event: FormEvent): void {
    event.preventDefault();
    const payload: Record<string, string> = {};
    for (const section of SETTING_SECTIONS) {
      for (const field of section.fields) {
        if (field.kind === "secret") {
          const typed = (secretDraft[field.key] ?? "").trim();
          if (typed !== "") payload[field.key] = typed;
        } else if (draft[field.key] !== undefined && draft[field.key] !== (stored[field.key] ?? "")) {
          payload[field.key] = draft[field.key] as string;
        }
      }
    }
    const modeValue = draft["kriskris_mode"] ?? stored["kriskris_mode"] ?? "webhook";
    if (modeValue !== (stored["kriskris_mode"] ?? "webhook")) payload["kriskris_mode"] = modeValue;
    if (Object.keys(payload).length === 0) {
      setSavedNote("Tidak ada perubahan.");
      return;
    }
    save.mutate(payload);
  }

  return (
    <div>
      <PageHeader title="Pengaturan" description="Konfigurasi billing, kriskris, dan notifikasi." />
      <form onSubmit={onSubmit} className="flex flex-col gap-4">
        {formError !== null && <FormAlert message={formError} />}
        {savedNote !== null && <FormNote message={savedNote} />}

        <Card>
          <fieldset className="m-0 border-0 p-0">
            <legend className="text-base font-bold text-slate-900 px-0">Mode konfirmasi kriskris</legend>
            <p className="mt-1 mb-3 text-sm text-slate-500">
              Pilih jalur utama konfirmasi pembayaran QRIS.
            </p>
            <div className="grid gap-3 md:grid-cols-2" role="radiogroup" aria-label="Mode konfirmasi kriskris">
              {KRISKRIS_MODES.map((value) => {
                const meta = KRISKRIS_MODE_META[value];
                const checked = mode === value;
                return (
                  <label
                    key={value}
                    className={`flex gap-3 rounded-md border p-3 cursor-pointer ${
                      checked ? "border-emerald-700 bg-emerald-50" : "border-slate-300 bg-white hover:bg-slate-50"
                    }`}
                  >
                    <input
                      type="radio"
                      name="kriskris_mode"
                      value={value}
                      checked={checked}
                      onChange={() => {
                        setSavedNote(null);
                        setCopied(false);
                        setDraft((prev) => ({ ...prev, ["kriskris_mode"]: value }));
                      }}
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
            {mode === "webhook" && (
              <div className="mt-4 rounded-md border border-slate-300 bg-slate-50 p-3">
                <Field
                  label="URL webhook"
                  htmlFor="kriskris-webhook-url"
                  hint="Daftarkan URL ini sebagai callback di dasbor kriskris. Wajib HTTPS dan bisa diakses dari internet."
                >
                  <div className="flex gap-2">
                    <Input
                      id="kriskris-webhook-url"
                      ref={urlRef}
                      type="text"
                      readOnly
                      value={webhookUrl}
                      onFocus={(event) => event.target.select()}
                      className="font-mono text-xs"
                    />
                    <button
                      type="button"
                      onClick={() => void copyWebhookUrl()}
                      className="shrink-0 inline-flex items-center justify-center gap-1.5 text-sm font-semibold rounded-md border border-slate-300 bg-white text-slate-800 px-3 py-1.5 min-h-[36px] hover:bg-slate-50 cursor-pointer focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-emerald-700"
                    >
                      {copied ? (
                        <Check className="size-4 text-emerald-700" aria-hidden="true" />
                      ) : (
                        <Copy className="size-4" aria-hidden="true" />
                      )}
                      {copied ? "Disalin!" : "Salin"}
                    </button>
                  </div>
                </Field>
              </div>
            )}
          </fieldset>
        </Card>

        {SETTING_SECTIONS.map((section) => (
          <Card key={section.title}>
            <h2 className="m-0 text-base font-bold text-slate-900">{section.title}</h2>
            <p className="mt-1 mb-4 text-sm text-slate-500">{section.desc}</p>
            <div className="grid gap-4 md:grid-cols-2">
              {section.fields.map((field) => {
                const id = `setting-${field.key}`;
                const err = fieldError(serverFields, field.key);
                if (field.kind === "secret") {
                  const storedMasked = stored[field.key] === MASKED;
                  return (
                    <Field
                      key={field.key}
                      label={field.label}
                      htmlFor={id}
                      hint={field.hint ?? (storedMasked ? "Tersimpan terenkripsi — kosongkan bila tidak diubah." : undefined)}
                      error={err}
                    >
                      <Input
                        id={id}
                        type="password"
                        autoComplete="new-password"
                        placeholder={storedMasked ? "••••••••" : ""}
                        value={secretDraft[field.key] ?? ""}
                        onChange={(event) => {
                          setSavedNote(null);
                          setSecretDraft((prev) => ({ ...prev, [field.key]: event.target.value }));
                        }}
                        invalid={err !== undefined}
                      />
                    </Field>
                  );
                }
                if (field.kind === "select") {
                  return (
                    <Field key={field.key} label={field.label} htmlFor={id} hint={field.hint} error={err}>
                      <Select
                        id={id}
                        value={draft[field.key] ?? stored[field.key] ?? ""}
                        onChange={(event) => setValue(field.key, event.target.value)}
                        invalid={err !== undefined}
                      >
                        {field.options.map((opt) => (
                          <option key={opt.value} value={opt.value}>
                            {opt.label}
                          </option>
                        ))}
                      </Select>
                    </Field>
                  );
                }
                if (field.kind === "textarea") {
                  return (
                    <div key={field.key} className="md:col-span-2">
                      <Field label={field.label} htmlFor={id} hint={field.hint} error={err}>
                        <Textarea
                          id={id}
                          rows={2}
                          value={draft[field.key] ?? stored[field.key] ?? ""}
                          onChange={(event) => setValue(field.key, event.target.value)}
                          invalid={err !== undefined}
                        />
                      </Field>
                    </div>
                  );
                }
                return (
                  <Field key={field.key} label={field.label} htmlFor={id} hint={field.hint} error={err}>
                    <Input
                      id={id}
                      type="text"
                      value={draft[field.key] ?? stored[field.key] ?? ""}
                      onChange={(event) => setValue(field.key, event.target.value)}
                      invalid={err !== undefined}
                    />
                  </Field>
                );
              })}
            </div>
          </Card>
        ))}

        <div>
          <Button type="submit" disabled={save.isPending}>
            {save.isPending ? "Menyimpan…" : "Simpan pengaturan"}
          </Button>
        </div>
      </form>
    </div>
  );
}
