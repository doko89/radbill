import { useEffect, useId, useRef } from "react";
import type {
  ButtonHTMLAttributes,
  InputHTMLAttributes,
  ReactNode,
  SelectHTMLAttributes,
  TextareaHTMLAttributes,
} from "react";
import { X } from "lucide-react";

/** shadcn-style primitives for billing pages (hand-written, no CLI). */

type ButtonVariant = "primary" | "secondary" | "danger" | "ghost";
type ButtonSize = "sm" | "md";

type ButtonProps = ButtonHTMLAttributes<HTMLButtonElement> & {
  variant?: ButtonVariant;
  size?: ButtonSize;
};

const buttonVariants: Record<ButtonVariant, string> = {
  primary: "bg-emerald-700 border-emerald-700 text-white hover:bg-emerald-800",
  secondary: "bg-white border-slate-300 text-slate-800 hover:bg-slate-50",
  danger: "bg-red-700 border-red-700 text-white hover:bg-red-800",
  ghost: "bg-transparent border-transparent text-slate-700 hover:bg-slate-100",
};

const buttonSizes: Record<ButtonSize, string> = {
  sm: "text-sm px-3 py-1.5 min-h-[36px]",
  md: "text-sm px-4 py-2 min-h-[44px]",
};

export function Button({
  variant = "primary",
  size = "md",
  className = "",
  type = "button",
  ...rest
}: ButtonProps) {
  return (
    <button
      type={type}
      className={`inline-flex items-center justify-center gap-2 font-semibold rounded-md border transition-colors cursor-pointer focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-emerald-700 disabled:opacity-60 disabled:cursor-not-allowed ${buttonVariants[variant]} ${buttonSizes[size]} ${className}`}
      {...rest}
    />
  );
}

const controlClass = (invalid: boolean, extra: string): string =>
  `w-full font-normal text-sm text-slate-900 bg-white border rounded-md px-3 py-2 min-h-[44px] placeholder:text-slate-400 focus-visible:outline-2 focus-visible:outline-offset-1 focus-visible:outline-emerald-700 disabled:bg-slate-100 disabled:cursor-not-allowed ${
    invalid ? "border-red-500" : "border-slate-300"
  } ${extra}`;

type InputProps = InputHTMLAttributes<HTMLInputElement> & { invalid?: boolean };

export function Input({ invalid = false, className = "", ...rest }: InputProps) {
  return <input aria-invalid={invalid || undefined} className={controlClass(invalid, className)} {...rest} />;
}

type TextareaProps = TextareaHTMLAttributes<HTMLTextAreaElement> & { invalid?: boolean };

export function Textarea({ invalid = false, className = "", ...rest }: TextareaProps) {
  return (
    <textarea aria-invalid={invalid || undefined} className={controlClass(invalid, className)} {...rest} />
  );
}

type SelectProps = SelectHTMLAttributes<HTMLSelectElement> & { invalid?: boolean };

export function Select({ invalid = false, className = "", children, ...rest }: SelectProps) {
  return (
    <select aria-invalid={invalid || undefined} className={controlClass(invalid, className)} {...rest}>
      {children}
    </select>
  );
}

export function Field({
  label,
  htmlFor,
  required = false,
  hint,
  error,
  children,
}: {
  label: string;
  htmlFor: string;
  required?: boolean | undefined;
  hint?: string | undefined;
  error?: string | undefined;
  children: ReactNode;
}) {
  return (
    <div className="flex flex-col gap-1.5">
      <label htmlFor={htmlFor} className="text-sm font-semibold text-slate-800">
        {label}{" "}
        {required && (
          <span aria-hidden="true" className="text-red-700">
            *
          </span>
        )}
      </label>
      {children}
      {hint && !error && <p className="text-xs text-slate-500">{hint}</p>}
      {error && (
        <p id={`${htmlFor}-error`} role="alert" className="text-xs font-medium text-red-700">
          {error}
        </p>
      )}
    </div>
  );
}

/** Pick the first backend field error matching any key alias. */
export function fieldError(
  fields: Record<string, string> | undefined,
  ...keys: string[]
): string | undefined {
  if (!fields) {
    return undefined;
  }
  for (const key of keys) {
    const hit = fields[key];
    if (hit) {
      return hit;
    }
  }
  return undefined;
}

export function Card({ children, className = "" }: { children: ReactNode; className?: string }) {
  return (
    <section className={`bg-white border border-slate-200 rounded-lg shadow-sm p-5 ${className}`}>
      {children}
    </section>
  );
}

type BadgeTone = "default" | "success" | "warning" | "danger" | "info";

const badgeTones: Record<BadgeTone, string> = {
  default: "bg-slate-100 text-slate-700",
  success: "bg-emerald-100 text-emerald-800",
  warning: "bg-amber-100 text-amber-900",
  danger: "bg-red-100 text-red-800",
  info: "bg-sky-100 text-sky-900",
};

export function Badge({ tone = "default", children }: { tone?: BadgeTone; children: ReactNode }) {
  return (
    <span
      className={`inline-block text-xs font-bold px-2.5 py-0.5 rounded-full whitespace-nowrap ${badgeTones[tone]}`}
    >
      {children}
    </span>
  );
}

export function Dialog({
  open,
  onClose,
  title,
  description,
  wide = false,
  children,
}: {
  open: boolean;
  onClose: () => void;
  title: string;
  description?: string | undefined;
  wide?: boolean;
  children: ReactNode;
}) {
  const titleId = useId();
  const descId = useId();
  const panelRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) {
      return;
    }
    panelRef.current?.focus();
    function onKey(event: KeyboardEvent): void {
      if (event.key === "Escape") {
        onClose();
      }
    }
    document.addEventListener("keydown", onKey);
    const previous = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    return () => {
      document.removeEventListener("keydown", onKey);
      document.body.style.overflow = previous;
    };
  }, [open, onClose]);

  if (!open) {
    return null;
  }
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-4">
      <button
        type="button"
        aria-label="Tutup dialog"
        onClick={onClose}
        className="absolute inset-0 bg-slate-950/50 cursor-pointer"
      />
      <div
        ref={panelRef}
        tabIndex={-1}
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        aria-describedby={description ? descId : undefined}
        className={`relative bg-white rounded-lg shadow-xl w-full ${wide ? "max-w-3xl" : "max-w-lg"} max-h-[90vh] overflow-y-auto p-6 focus:outline-none`}
      >
        <div className="flex items-start justify-between gap-4">
          <h2 id={titleId} className="m-0 text-lg font-bold text-slate-900">
            {title}
          </h2>
          <button
            type="button"
            onClick={onClose}
            aria-label="Tutup"
            className="shrink-0 inline-flex items-center justify-center size-9 rounded-md text-slate-500 hover:bg-slate-100 hover:text-slate-800 cursor-pointer focus-visible:outline-2 focus-visible:outline-emerald-700"
          >
            <X className="size-5" aria-hidden="true" />
          </button>
        </div>
        {description && (
          <p id={descId} className="text-sm text-slate-500 mt-1 mb-4">
            {description}
          </p>
        )}
        <div className="mt-4">{children}</div>
      </div>
    </div>
  );
}

export function ConfirmDialog({
  open,
  title,
  message,
  confirmLabel = "Ya, lanjutkan",
  busy = false,
  danger = false,
  onConfirm,
  onClose,
}: {
  open: boolean;
  title: string;
  message: string;
  confirmLabel?: string;
  busy?: boolean;
  danger?: boolean;
  onConfirm: () => void;
  onClose: () => void;
}) {
  return (
    <Dialog open={open} onClose={onClose} title={title} description={message}>
      <div className="flex justify-end gap-2">
        <Button variant="secondary" onClick={onClose} disabled={busy}>
          Batal
        </Button>
        <Button variant={danger ? "danger" : "primary"} onClick={onConfirm} disabled={busy}>
          {busy ? "Memproses…" : confirmLabel}
        </Button>
      </div>
    </Dialog>
  );
}

export function Table({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="overflow-x-auto bg-white border border-slate-200 rounded-lg shadow-sm">
      <table className="w-full min-w-[640px] border-collapse text-sm">{children}</table>
      <span className="sr-only">{label}</span>
    </div>
  );
}

export function THead({ children }: { children: ReactNode }) {
  return <thead className="bg-slate-50 border-b border-slate-200">{children}</thead>;
}

export function TBody({ children }: { children: ReactNode }) {
  return <tbody className="divide-y divide-slate-100">{children}</tbody>;
}

export function TR({ children }: { children: ReactNode }) {
  return <tr className="align-top">{children}</tr>;
}

export function TH({
  children,
  align = "left",
  ariaLabel,
}: {
  children?: ReactNode;
  align?: "left" | "right";
  ariaLabel?: string;
}) {
  return (
    <th
      scope="col"
      aria-label={ariaLabel}
      className={`px-4 py-3 text-xs font-bold uppercase tracking-wide text-slate-500 whitespace-nowrap ${align === "right" ? "text-right" : "text-left"}`}
    >
      {children}
    </th>
  );
}

export function TD({
  children,
  align = "left",
  muted = false,
}: {
  children?: ReactNode;
  align?: "left" | "right";
  muted?: boolean;
}) {
  return (
    <td
      className={`px-4 py-3 ${align === "right" ? "text-right" : "text-left"} ${muted ? "text-slate-500" : "text-slate-800"}`}
    >
      {children}
    </td>
  );
}

export function Pagination({
  page,
  perPage,
  total,
  onPage,
}: {
  page: number;
  perPage: number;
  total: number;
  onPage: (page: number) => void;
}) {
  const totalPages = Math.max(1, Math.ceil(total / perPage));
  if (totalPages <= 1) {
    return null;
  }
  return (
    <nav aria-label="Navigasi halaman" className="flex flex-wrap items-center gap-3 mt-4">
      <Button variant="secondary" size="sm" disabled={page <= 1} onClick={() => onPage(page - 1)}>
        Sebelumnya
      </Button>
      <p className="text-sm text-slate-500" role="status">
        Halaman {page} dari {totalPages} ({total} data)
      </p>
      <Button
        variant="secondary"
        size="sm"
        disabled={page >= totalPages}
        onClick={() => onPage(page + 1)}
      >
        Berikutnya
      </Button>
    </nav>
  );
}

export function EmptyState({ title, hint }: { title: string; hint?: string }) {
  return (
    <div className="bg-white border border-dashed border-slate-300 rounded-lg p-8 text-center">
      <p className="m-0 font-semibold text-slate-800">{title}</p>
      {hint && <p className="text-sm text-slate-500 mt-1 mb-0">{hint}</p>}
    </div>
  );
}

export function PageHeader({
  title,
  description,
  actions,
}: {
  title: string;
  description?: string;
  actions?: ReactNode;
}) {
  return (
    <div className="flex flex-wrap items-end justify-between gap-3 mb-5">
      <div>
        <h1 className="m-0 text-2xl font-bold text-slate-900">{title}</h1>
        {description && <p className="text-sm text-slate-500 mt-1 mb-0">{description}</p>}
      </div>
      {actions && <div className="flex flex-wrap gap-2">{actions}</div>}
    </div>
  );
}

export function FormAlert({ message }: { message: string }) {
  return (
    <p
      role="alert"
      className="m-0 text-sm font-medium text-red-800 bg-red-50 border border-red-200 rounded-md px-3 py-2"
    >
      {message}
    </p>
  );
}

export function FormNote({ message }: { message: string }) {
  return (
    <p
      role="status"
      className="m-0 text-sm font-medium text-emerald-800 bg-emerald-50 border border-emerald-200 rounded-md px-3 py-2"
    >
      {message}
    </p>
  );
}
