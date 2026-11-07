import { useState } from "react";
import { formatBytes, formatDate } from "../../../shared/lib/billing-format";
import type { PortalUsagePoint } from "../api";

const W = 640;
const H = 240;
const PAD_LEFT = 56;
const PAD_RIGHT = 12;
const PAD_TOP = 12;
const PAD_BOTTOM = 28;

/** Grafik garis SVG tulisan tangan: unduh vs unggah per hari, tanpa dep chart. */
export function UsageChart({ points }: { points: PortalUsagePoint[] }) {
  const [showDown, setShowDown] = useState(true);
  const [showUp, setShowUp] = useState(true);

  if (points.length === 0) {
    return <p className="m-0 text-sm text-slate-500">Belum ada data pemakaian.</p>;
  }
  const maxValue = Math.max(1, ...points.map((p) => Math.max(p.bytes_in, p.bytes_out)));
  const innerW = W - PAD_LEFT - PAD_RIGHT;
  const innerH = H - PAD_TOP - PAD_BOTTOM;
  const x = (i: number): number =>
    PAD_LEFT + (points.length === 1 ? innerW / 2 : (i / (points.length - 1)) * innerW);
  const y = (v: number): number => PAD_TOP + innerH - (v / maxValue) * innerH;

  const downPath = points.map((p, i) => `${i === 0 ? "M" : "L"}${x(i).toFixed(1)},${y(p.bytes_out).toFixed(1)}`).join(" ");
  const upPath = points.map((p, i) => `${i === 0 ? "M" : "L"}${x(i).toFixed(1)},${y(p.bytes_in).toFixed(1)}`).join(" ");

  const ticks = [0, 1, 2, 3, 4].map((t) => (maxValue * t) / 4);
  const labelEvery = Math.max(1, Math.ceil(points.length / 6));
  const totalDown = points.reduce((sum, p) => sum + p.bytes_out, 0);
  const totalUp = points.reduce((sum, p) => sum + p.bytes_in, 0);

  return (
    <div className="flex flex-col gap-3">
      <div className="flex flex-wrap gap-2" role="group" aria-label="Pilih seri data">
        <button
          type="button"
          onClick={() => setShowDown((v) => !v)}
          aria-pressed={showDown}
          className={`inline-flex items-center gap-2 text-sm font-semibold rounded-full border px-3 py-1 min-h-[36px] cursor-pointer focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-emerald-700 ${
            showDown
              ? "bg-emerald-700 border-emerald-700 text-white"
              : "bg-white border-slate-300 text-slate-600"
          }`}
        >
          <span className={`inline-block size-2.5 rounded-full ${showDown ? "bg-white" : "bg-emerald-600"}`} aria-hidden="true" />
          Unduh
        </button>
        <button
          type="button"
          onClick={() => setShowUp((v) => !v)}
          aria-pressed={showUp}
          className={`inline-flex items-center gap-2 text-sm font-semibold rounded-full border px-3 py-1 min-h-[36px] cursor-pointer focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-emerald-700 ${
            showUp ? "bg-sky-700 border-sky-700 text-white" : "bg-white border-slate-300 text-slate-600"
          }`}
        >
          <span className={`inline-block size-2.5 rounded-full ${showUp ? "bg-white" : "bg-sky-600"}`} aria-hidden="true" />
          Unggah
        </button>
      </div>
      <svg
        viewBox={`0 0 ${W} ${H}`}
        role="img"
        aria-label={`Grafik pemakaian harian: total unduh ${formatBytes(totalDown)}, unggah ${formatBytes(totalUp)}.`}
        className="w-full h-auto bg-white border border-slate-200 rounded-lg"
      >
        {ticks.map((tick, i) => (
          <g key={tick}>
            <line
              x1={PAD_LEFT}
              x2={W - PAD_RIGHT}
              y1={y(tick)}
              y2={y(tick)}
              stroke={i === 0 ? "#cbd5e1" : "#e2e8f0"}
              strokeWidth="1"
            />
            <text x={PAD_LEFT - 6} y={y(tick) + 4} textAnchor="end" fontSize="10" fill="#64748b">
              {formatBytes(tick)}
            </text>
          </g>
        ))}
        {points.map((p, i) =>
          i % labelEvery === 0 || i === points.length - 1 ? (
            <text
              key={p.date}
              x={x(i)}
              y={H - 8}
              textAnchor="middle"
              fontSize="10"
              fill="#64748b"
            >
              {p.date.slice(8)}
              {i === 0 || p.date.endsWith("-01") ? `/${p.date.slice(5, 7)}` : ""}
            </text>
          ) : null,
        )}
        {showDown && <path d={downPath} fill="none" stroke="#047857" strokeWidth="2" strokeLinejoin="round" />}
        {showUp && <path d={upPath} fill="none" stroke="#0369a1" strokeWidth="2" strokeDasharray="5 3" strokeLinejoin="round" />}
        {points.map((p, i) => (
          <g key={p.date}>
            <title>
              {formatDate(p.date)}: unduh {formatBytes(p.bytes_out)}, unggah {formatBytes(p.bytes_in)}
            </title>
            {showDown && <circle cx={x(i)} cy={y(p.bytes_out)} r="3" fill="#047857" />}
            {showUp && <circle cx={x(i)} cy={y(p.bytes_in)} r="3" fill="#ffffff" stroke="#0369a1" strokeWidth="2" />}
          </g>
        ))}
      </svg>
    </div>
  );
}
