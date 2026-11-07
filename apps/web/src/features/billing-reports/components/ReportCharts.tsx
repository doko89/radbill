import { useState } from "react";
import { formatBytes, formatDate, formatIDR } from "../../../shared/lib/billing-format";
import type { GrowthPoint, ReportUsagePoint, RevenuePoint } from "../api";

const W = 640;
const H = 240;
const PAD_LEFT = 64;
const PAD_RIGHT = 12;
const PAD_TOP = 12;
const PAD_BOTTOM = 28;

/** Grafik SVG tulisan tangan untuk laporan (tanpa dep chart). */

function formatCompactIDR(value: number): string {
  if (value >= 1_000_000) {
    const jt = value / 1_000_000;
    return `Rp${jt >= 100 ? Math.round(jt).toString() : jt.toFixed(1)} jt`;
  }
  if (value >= 1000) {
    return `Rp${Math.round(value / 1000)} rb`;
  }
  return `Rp${Math.round(value)}`;
}

/** Label sumbu-X untuk `YYYY-MM-DD` (harian) maupun `YYYY-MM` (bulanan). */
function periodLabel(period: string, index: number): string {
  if (/^\d{4}-\d{2}$/.test(period)) {
    return `${period.slice(5, 7)}/${period.slice(2, 4)}`;
  }
  if (/^\d{4}-\d{2}-\d{2}$/.test(period)) {
    const day = period.slice(8, 10);
    if (index === 0 || period.endsWith("-01")) {
      return `${day}/${period.slice(5, 7)}`;
    }
    return day;
  }
  return period.length > 10 ? period.slice(0, 10) : period;
}

export function RevenueBarChart({ points }: { points: RevenuePoint[] }) {
  if (points.length === 0) {
    return <p className="m-0 text-sm text-slate-500">Belum ada data pendapatan.</p>;
  }
  const maxValue = Math.max(1, ...points.map((p) => p.total));
  const total = points.reduce((sum, p) => sum + p.total, 0);
  const innerW = W - PAD_LEFT - PAD_RIGHT;
  const innerH = H - PAD_TOP - PAD_BOTTOM;
  const slot = innerW / points.length;
  const barW = Math.max(4, Math.min(48, slot * 0.6));
  const barX = (i: number): number => PAD_LEFT + slot * i + (slot - barW) / 2;
  const y = (v: number): number => PAD_TOP + innerH - (v / maxValue) * innerH;
  const ticks = [0, 1, 2, 3, 4].map((t) => (maxValue * t) / 4);
  const labelEvery = Math.max(1, Math.ceil(points.length / 8));

  return (
    <svg
      viewBox={`0 0 ${W} ${H}`}
      role="img"
      aria-label={`Grafik batang pendapatan: total ${formatIDR(total)} dari ${points.length} periode.`}
      className="w-full h-auto bg-white border border-slate-200 rounded-lg"
    >
      {ticks.map((tick) => (
        <g key={tick}>
          <line
            x1={PAD_LEFT}
            x2={W - PAD_RIGHT}
            y1={y(tick)}
            y2={y(tick)}
            stroke={tick === 0 ? "#cbd5e1" : "#e2e8f0"}
            strokeWidth="1"
          />
          <text x={PAD_LEFT - 6} y={y(tick) + 4} textAnchor="end" fontSize="10" fill="#64748b">
            {formatCompactIDR(tick)}
          </text>
        </g>
      ))}
      {points.map((p, i) =>
        i % labelEvery === 0 || i === points.length - 1 ? (
          <text
            key={p.period}
            x={barX(i) + barW / 2}
            y={H - 8}
            textAnchor="middle"
            fontSize="10"
            fill="#64748b"
          >
            {periodLabel(p.period, i)}
          </text>
        ) : null,
      )}
      {points.map((p, i) => (
        <g key={p.period}>
          <title>
            {p.period}: {formatIDR(p.total)}
          </title>
          <rect
            x={barX(i)}
            y={y(p.total)}
            width={barW}
            height={Math.max(1, PAD_TOP + innerH - y(p.total))}
            rx="2"
            fill="#047857"
          />
        </g>
      ))}
    </svg>
  );
}

export function UsageLineChart({ points }: { points: ReportUsagePoint[] }) {
  const [showDown, setShowDown] = useState(true);
  const [showUp, setShowUp] = useState(true);

  if (points.length === 0) {
    return <p className="m-0 text-sm text-slate-500">Belum ada data pemakaian.</p>;
  }
  const maxValue = Math.max(1, ...points.map((p) => Math.max(p.bytes_in, p.bytes_out)));
  const totalDown = points.reduce((sum, p) => sum + p.bytes_out, 0);
  const totalUp = points.reduce((sum, p) => sum + p.bytes_in, 0);
  const innerW = W - PAD_LEFT - PAD_RIGHT;
  const innerH = H - PAD_TOP - PAD_BOTTOM;
  const x = (i: number): number =>
    PAD_LEFT + (points.length === 1 ? innerW / 2 : (i / (points.length - 1)) * innerW);
  const y = (v: number): number => PAD_TOP + innerH - (v / maxValue) * innerH;
  const downPath = points
    .map((p, i) => `${i === 0 ? "M" : "L"}${x(i).toFixed(1)},${y(p.bytes_out).toFixed(1)}`)
    .join(" ");
  const upPath = points
    .map((p, i) => `${i === 0 ? "M" : "L"}${x(i).toFixed(1)},${y(p.bytes_in).toFixed(1)}`)
    .join(" ");
  const ticks = [0, 1, 2, 3, 4].map((t) => (maxValue * t) / 4);
  const labelEvery = Math.max(1, Math.ceil(points.length / 6));

  return (
    <div className="flex flex-col gap-3">
      <div className="flex flex-wrap gap-2" role="group" aria-label="Pilih seri data">
        <button
          type="button"
          onClick={() => setShowDown((v) => !v)}
          aria-pressed={showDown}
          className={`inline-flex min-h-[36px] cursor-pointer items-center gap-2 rounded-full border px-3 py-1 text-sm font-semibold focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-emerald-700 ${
            showDown
              ? "border-emerald-700 bg-emerald-700 text-white"
              : "border-slate-300 bg-white text-slate-600"
          }`}
        >
          <span
            className={`inline-block size-2.5 rounded-full ${showDown ? "bg-white" : "bg-emerald-600"}`}
            aria-hidden="true"
          />
          Unduh
        </button>
        <button
          type="button"
          onClick={() => setShowUp((v) => !v)}
          aria-pressed={showUp}
          className={`inline-flex min-h-[36px] cursor-pointer items-center gap-2 rounded-full border px-3 py-1 text-sm font-semibold focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-emerald-700 ${
            showUp
              ? "border-sky-700 bg-sky-700 text-white"
              : "border-slate-300 bg-white text-slate-600"
          }`}
        >
          <span
            className={`inline-block size-2.5 rounded-full ${showUp ? "bg-white" : "bg-sky-600"}`}
            aria-hidden="true"
          />
          Unggah
        </button>
      </div>
      <svg
        viewBox={`0 0 ${W} ${H}`}
        role="img"
        aria-label={`Grafik pemakaian: total unduh ${formatBytes(totalDown)}, unggah ${formatBytes(totalUp)}.`}
        className="w-full h-auto bg-white border border-slate-200 rounded-lg"
      >
        {ticks.map((tick) => (
          <g key={tick}>
            <line
              x1={PAD_LEFT}
              x2={W - PAD_RIGHT}
              y1={y(tick)}
              y2={y(tick)}
              stroke={tick === 0 ? "#cbd5e1" : "#e2e8f0"}
              strokeWidth="1"
            />
            <text x={PAD_LEFT - 6} y={y(tick) + 4} textAnchor="end" fontSize="10" fill="#64748b">
              {formatBytes(tick)}
            </text>
          </g>
        ))}
        {points.map((p, i) =>
          i % labelEvery === 0 || i === points.length - 1 ? (
            <text key={p.date} x={x(i)} y={H - 8} textAnchor="middle" fontSize="10" fill="#64748b">
              {periodLabel(p.date, i)}
            </text>
          ) : null,
        )}
        {showDown && (
          <path d={downPath} fill="none" stroke="#047857" strokeWidth="2" strokeLinejoin="round" />
        )}
        {showUp && (
          <path
            d={upPath}
            fill="none"
            stroke="#0369a1"
            strokeWidth="2"
            strokeDasharray="5 3"
            strokeLinejoin="round"
          />
        )}
        {points.map((p, i) => (
          <g key={p.date}>
            <title>
              {formatDate(p.date)}: unduh {formatBytes(p.bytes_out)}, unggah {formatBytes(p.bytes_in)}
            </title>
            {showDown && <circle cx={x(i)} cy={y(p.bytes_out)} r="3" fill="#047857" />}
            {showUp && (
              <circle cx={x(i)} cy={y(p.bytes_in)} r="3" fill="#ffffff" stroke="#0369a1" strokeWidth="2" />
            )}
          </g>
        ))}
      </svg>
    </div>
  );
}

export function GrowthChart({ points }: { points: GrowthPoint[] }) {
  if (points.length === 0) {
    return <p className="m-0 text-sm text-slate-500">Belum ada data pertumbuhan.</p>;
  }
  const maxValue = Math.max(1, ...points.map((p) => Math.max(p.new, p.churn, p.active)));
  const innerW = W - PAD_LEFT - PAD_RIGHT;
  const innerH = H - PAD_TOP - PAD_BOTTOM;
  const slot = innerW / points.length;
  const barW = Math.max(3, Math.min(20, slot * 0.22));
  const gap = Math.max(1, barW * 0.25);
  const groupW = barW * 2 + gap;
  const groupX = (i: number): number => PAD_LEFT + slot * i + (slot - groupW) / 2;
  const x = (i: number): number =>
    PAD_LEFT + (points.length === 1 ? innerW / 2 : (i / (points.length - 1)) * innerW);
  const y = (v: number): number => PAD_TOP + innerH - (v / maxValue) * innerH;
  const activePath = points
    .map((p, i) => `${i === 0 ? "M" : "L"}${x(i).toFixed(1)},${y(p.active).toFixed(1)}`)
    .join(" ");
  const ticks = [0, 1, 2, 3, 4].map((t) => Math.round((maxValue * t) / 4));
  const labelEvery = Math.max(1, Math.ceil(points.length / 8));
  const last = points[points.length - 1];

  return (
    <div className="flex flex-col gap-3">
      <div className="flex flex-wrap gap-4 text-sm" role="group" aria-label="Legenda">
        <span className="inline-flex items-center gap-1.5 font-semibold text-slate-700">
          <span className="inline-block h-3 w-3 rounded-sm bg-emerald-600" aria-hidden="true" />
          Baru
        </span>
        <span className="inline-flex items-center gap-1.5 font-semibold text-slate-700">
          <span
            className="inline-block h-3 w-3 rounded-sm border border-red-700 bg-red-200"
            aria-hidden="true"
          />
          Churn
        </span>
        <span className="inline-flex items-center gap-1.5 font-semibold text-slate-700">
          <span className="inline-block h-0.5 w-4 bg-sky-700" aria-hidden="true" />
          Aktif
        </span>
      </div>
      <svg
        viewBox={`0 0 ${W} ${H}`}
        role="img"
        aria-label={
          last
            ? `Grafik pertumbuhan: periode terakhir ${last.period}, aktif ${last.active}, baru ${last.new}, churn ${last.churn}.`
            : "Grafik pertumbuhan."
        }
        className="w-full h-auto bg-white border border-slate-200 rounded-lg"
      >
        <defs>
          <pattern
            id="growth-churn-hatch"
            width="6"
            height="6"
            patternUnits="userSpaceOnUse"
            patternTransform="rotate(45)"
          >
            <rect width="6" height="6" fill="#fecaca" />
            <line x1="0" y1="0" x2="0" y2="6" stroke="#b91c1c" strokeWidth="2" />
          </pattern>
        </defs>
        {ticks.map((tick) => (
          <g key={tick}>
            <line
              x1={PAD_LEFT}
              x2={W - PAD_RIGHT}
              y1={y(tick)}
              y2={y(tick)}
              stroke={tick === 0 ? "#cbd5e1" : "#e2e8f0"}
              strokeWidth="1"
            />
            <text x={PAD_LEFT - 6} y={y(tick) + 4} textAnchor="end" fontSize="10" fill="#64748b">
              {tick}
            </text>
          </g>
        ))}
        {points.map((p, i) =>
          i % labelEvery === 0 || i === points.length - 1 ? (
            <text
              key={p.period}
              x={groupX(i) + groupW / 2}
              y={H - 8}
              textAnchor="middle"
              fontSize="10"
              fill="#64748b"
            >
              {periodLabel(p.period, i)}
            </text>
          ) : null,
        )}
        {points.map((p, i) => (
          <g key={p.period}>
            <title>
              {p.period}: baru {p.new}, churn {p.churn}, aktif {p.active}
            </title>
            <rect
              x={groupX(i)}
              y={y(p.new)}
              width={barW}
              height={Math.max(1, PAD_TOP + innerH - y(p.new))}
              rx="2"
              fill="#059669"
            />
            <rect
              x={groupX(i) + barW + gap}
              y={y(p.churn)}
              width={barW}
              height={Math.max(1, PAD_TOP + innerH - y(p.churn))}
              rx="2"
              fill="url(#growth-churn-hatch)"
              stroke="#b91c1c"
              strokeWidth="1"
            />
          </g>
        ))}
        <path d={activePath} fill="none" stroke="#0369a1" strokeWidth="2" strokeLinejoin="round" />
        {points.map((p, i) => (
          <circle key={p.period} cx={x(i)} cy={y(p.active)} r="3" fill="#0369a1" />
        ))}
      </svg>
    </div>
  );
}
