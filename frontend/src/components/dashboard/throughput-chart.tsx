"use client";

import { useEffect, useRef, useState } from "react";

import type { Stats } from "@/lib/api/client";
import { formatCount } from "@/lib/format";

type Bucket = Stats["throughput"][number];

// Stack order bottom→top. Colours are the validated categorical slots 1–3 (globals.css).
const STACK = [
  { key: "succeeded", label: "Completed", color: "var(--series-completed)" },
  { key: "failed", label: "Dead letter", color: "var(--series-dead)" },
  { key: "cancelled", label: "Cancelled", color: "var(--series-cancelled)" },
] as const;
const SUBMITTED = { label: "Submitted", color: "var(--series-submitted)" };

const HEIGHT = 220;
const PAD = { top: 12, right: 8, bottom: 26, left: 40 };
const GAP = 2; // surface gap between stacked segments and between adjacent bars
const MAX_BAR = 24;
const RADIUS = 4;

const timeFmt = new Intl.DateTimeFormat("en", { hour: "2-digit", minute: "2-digit" });

/** Rounds a max up to 1/2/5×10ⁿ and returns 4 evenly spaced ticks. */
function niceTicks(max: number): number[] {
  if (max <= 0) return [0, 1];
  const step0 = max / 4;
  const mag = 10 ** Math.floor(Math.log10(step0));
  const step = [1, 2, 5, 10].map((m) => m * mag).find((s) => s >= step0) ?? 10 * mag;
  const step1 = Math.max(1, step);
  return Array.from({ length: Math.ceil(max / step1) + 1 }, (_, i) => i * step1);
}

/** A column segment with a rounded data-end (top) and a square baseline end. */
function topRounded(x: number, y: number, w: number, h: number, r: number) {
  r = Math.min(r, w / 2, h);
  return `M${x},${y + h}V${y + r}Q${x},${y} ${x + r},${y}H${x + w - r}Q${x + w},${y} ${x + w},${y + r}V${y + h}Z`;
}

export function ThroughputChart({ buckets, bucketSeconds }: { buckets: Bucket[]; bucketSeconds: number }) {
  const box = useRef<HTMLDivElement>(null);
  const [width, setWidth] = useState<number | null>(null);
  const [active, setActive] = useState<number | null>(null);

  useEffect(() => {
    const el = box.current;
    if (!el) return;
    const ro = new ResizeObserver(([e]) => e && setWidth(Math.max(280, e.contentRect.width)));
    ro.observe(el);
    return () => ro.disconnect();
  }, []);

  const n = Math.max(buckets.length, 1);
  const w = width ?? 0;
  const innerW = w - PAD.left - PAD.right;
  const innerH = HEIGHT - PAD.top - PAD.bottom;
  const band = innerW / n;
  const barW = Math.max(1, Math.min(MAX_BAR, band - GAP));
  const max = Math.max(0, ...buckets.map((b) => Math.max(b.succeeded + b.failed + b.cancelled, b.submitted)));
  const ticks = niceTicks(max);
  const top = ticks[ticks.length - 1] ?? 1;
  const y = (v: number) => PAD.top + innerH - (v / top) * innerH;
  const cx = (i: number) => PAD.left + band * i + band / 2;
  const labelEvery = Math.ceil(n / Math.max(2, Math.floor(innerW / 70)));
  const line = buckets.map((b, i) => `${i ? "L" : "M"}${cx(i)},${y(b.submitted)}`).join("");
  const hovered = active === null ? undefined : buckets[active];
  const total = (b: Bucket) => b.succeeded + b.failed + b.cancelled;

  return (
    <div>
      <ul className="mb-3 flex flex-wrap gap-x-4 gap-y-1 text-xs text-zinc-400" aria-label="Legend">
        {STACK.map((s) => (
          <li key={s.key} className="flex items-center gap-1.5">
            <span className="size-2.5 rounded-sm" style={{ background: s.color }} aria-hidden="true" />
            {s.label}
          </li>
        ))}
        <li className="flex items-center gap-1.5">
          <span className="h-0.5 w-3 rounded" style={{ background: SUBMITTED.color }} aria-hidden="true" />
          {SUBMITTED.label}
        </li>
      </ul>

      <div ref={box} className="relative overflow-hidden" style={{ height: HEIGHT }}>
        {width !== null && (
        <svg
          width={w}
          height={HEIGHT}
          role="img"
          aria-label={`Jobs finished per ${bucketSeconds / 60 >= 1 ? `${bucketSeconds / 60} min` : `${bucketSeconds} s`}, by outcome, with submissions as a line. Use the arrow keys to read each bucket.`}
          tabIndex={0}
          className="block outline-none"
          onKeyDown={(e) => {
            if (e.key === "ArrowRight") setActive((a) => Math.min(n - 1, (a ?? -1) + 1));
            if (e.key === "ArrowLeft") setActive((a) => Math.max(0, (a ?? n) - 1));
            if (e.key === "Escape") setActive(null);
          }}
          onBlur={() => setActive(null)}
          onPointerLeave={() => setActive(null)}
        >
          {ticks.map((t) => (
            <g key={t}>
              <line x1={PAD.left} x2={w - PAD.right} y1={y(t)} y2={y(t)} stroke="var(--chart-grid)" strokeWidth={1} />
              <text x={PAD.left - 6} y={y(t)} dy="0.32em" textAnchor="end" fontSize={11} fill="var(--chart-text)" className="tabular-nums">
                {formatCount(t)}
              </text>
            </g>
          ))}

          {active !== null && (
            <rect x={PAD.left + band * active} y={PAD.top} width={band} height={innerH} fill="#ffffff" opacity={0.05} />
          )}

          {buckets.map((b, i) => {
            const x = cx(i) - barW / 2;
            let base = 0;
            const drawn = STACK.filter((s) => b[s.key] > 0);
            return (
              <g key={b.start}>
                {drawn.map((s, j) => {
                  const v = b[s.key];
                  const y0 = y(base + v);
                  const h = y(base) - y0 - (j > 0 ? GAP : 0);
                  base += v;
                  if (h <= 0) return null;
                  const isTop = j === drawn.length - 1;
                  return isTop ? (
                    <path key={s.key} d={topRounded(x, y0, barW, h, RADIUS)} fill={s.color} />
                  ) : (
                    <rect key={s.key} x={x} y={y0} width={barW} height={h} fill={s.color} />
                  );
                })}
                {i % labelEvery === 0 && (
                  <text x={cx(i)} y={HEIGHT - 8} textAnchor={cx(i) > w - 40 ? "end" : "middle"} fontSize={11} fill="var(--chart-text)">
                    {timeFmt.format(new Date(b.start))}
                  </text>
                )}
              </g>
            );
          })}

          <path d={line} fill="none" stroke={SUBMITTED.color} strokeWidth={2} strokeLinejoin="round" strokeLinecap="round" />
          {hovered && (
            <circle cx={cx(active!)} cy={y(hovered.submitted)} r={4} fill={SUBMITTED.color} stroke="var(--chart-surface)" strokeWidth={2} />
          )}

          {/* Hit targets: the whole band, larger than any mark. */}
          {buckets.map((b, i) => (
            <rect
              key={`hit-${b.start}`}
              x={PAD.left + band * i}
              y={PAD.top}
              width={band}
              height={innerH}
              fill="transparent"
              onPointerEnter={() => setActive(i)}
              onPointerMove={() => setActive(i)}
            />
          ))}
        </svg>
        )}

        {hovered && (
          <div
            className="pointer-events-none absolute top-2 z-10 w-44 rounded-lg border border-zinc-700 bg-zinc-950/95 p-2.5 text-xs shadow-lg"
            style={{ left: Math.min(Math.max(cx(active!) - 88, 0), w - 176) }}
            role="status"
          >
            <div className="mb-1.5 text-zinc-400">{timeFmt.format(new Date(hovered.start))}</div>
            {[...STACK].reverse().map((s) => (
              <div key={s.key} className="flex items-center gap-2">
                <span className="h-0.5 w-3 rounded" style={{ background: s.color }} aria-hidden="true" />
                <span className="font-semibold text-zinc-100 tabular-nums">{formatCount(hovered[s.key])}</span>
                <span className="text-zinc-400">{s.label}</span>
              </div>
            ))}
            <div className="flex items-center gap-2">
              <span className="h-0.5 w-3 rounded" style={{ background: SUBMITTED.color }} aria-hidden="true" />
              <span className="font-semibold text-zinc-100 tabular-nums">{formatCount(hovered.submitted)}</span>
              <span className="text-zinc-400">Submitted</span>
            </div>
            <div className="mt-1 border-t border-zinc-800 pt-1 text-zinc-400">
              <span className="font-semibold text-zinc-100 tabular-nums">{formatCount(hovered.retried)}</span> retries started
            </div>
          </div>
        )}
      </div>

      <details className="mt-3 text-sm">
        <summary className="cursor-pointer text-zinc-400 hover:text-zinc-200">Show as table</summary>
        <div className="mt-2 max-h-64 overflow-auto rounded-lg border border-zinc-800">
          <table className="w-full text-right text-xs tabular-nums">
            <thead className="sticky top-0 bg-zinc-900 text-zinc-400">
              <tr>
                <th className="px-3 py-1.5 text-left font-medium">Time</th>
                <th className="px-3 py-1.5 font-medium">Submitted</th>
                <th className="px-3 py-1.5 font-medium">Completed</th>
                <th className="px-3 py-1.5 font-medium">Dead letter</th>
                <th className="px-3 py-1.5 font-medium">Cancelled</th>
                <th className="px-3 py-1.5 font-medium">Retries</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-zinc-800 text-zinc-200">
              {buckets.filter((b) => total(b) || b.submitted || b.retried).map((b) => (
                <tr key={b.start}>
                  <td className="px-3 py-1 text-left">{timeFmt.format(new Date(b.start))}</td>
                  <td className="px-3 py-1">{b.submitted}</td>
                  <td className="px-3 py-1">{b.succeeded}</td>
                  <td className="px-3 py-1">{b.failed}</td>
                  <td className="px-3 py-1">{b.cancelled}</td>
                  <td className="px-3 py-1">{b.retried}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </details>
    </div>
  );
}
