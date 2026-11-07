/** Sisa jatah sebagai meter aksesibel (progress + angka tabular). */
export function QuotaMeter({
  label,
  usedLabel,
  totalLabel,
  remainingLabel,
  used,
  limit,
  remaining,
}: {
  label: string;
  usedLabel: string;
  totalLabel: string;
  remainingLabel: string;
  used: number;
  limit: number | null;
  remaining: number | null;
}) {
  if (limit === null || limit <= 0 || remaining === null) {
    return (
      <div>
        <p className="m-0 text-sm font-semibold text-slate-800">{label}</p>
        <p className="m-0 mt-1 text-sm text-slate-500 tabular-nums">
          Terpakai {usedLabel} (tanpa batas)
        </p>
      </div>
    );
  }
  const pct = Math.min(100, Math.max(0, Math.round((used / limit) * 100)));
  return (
    <div>
      <div className="flex items-baseline justify-between gap-2">
        <p className="m-0 text-sm font-semibold text-slate-800">{label}</p>
        <p className="m-0 text-sm text-slate-500 tabular-nums">
          Sisa <span className="font-bold text-slate-800">{remainingLabel}</span>
        </p>
      </div>
      <div
        role="progressbar"
        aria-label={label}
        aria-valuenow={pct}
        aria-valuemin={0}
        aria-valuemax={100}
        className="mt-2 h-2.5 rounded-full bg-slate-200 overflow-hidden"
      >
        <div
          className={`h-full rounded-full ${pct >= 90 ? "bg-red-600" : pct >= 75 ? "bg-amber-500" : "bg-emerald-600"}`}
          style={{ width: `${pct}%` }}
        />
      </div>
      <p className="m-0 mt-1 text-xs text-slate-500 tabular-nums">
        Terpakai {usedLabel} dari {totalLabel} — {pct}%
      </p>
    </div>
  );
}
