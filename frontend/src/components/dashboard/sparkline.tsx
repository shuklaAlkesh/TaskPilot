/**
 * A tiny trend line for a stat tile: the history in a de-emphasised grey, the latest bucket as a dot in
 * the tile's accent (currentColor). Fixed size, so the dot never distorts.
 */
export function Sparkline({ values, label, className = "" }: { values: number[]; label: string; className?: string }) {
  if (values.length < 2) return null;
  const W = 96;
  const H = 28;
  const max = Math.max(1, ...values);
  const x = (i: number) => (i / (values.length - 1)) * (W - 4) + 2;
  const y = (v: number) => H - 3 - (v / max) * (H - 6);
  const d = values.map((v, i) => `${i ? "L" : "M"}${x(i).toFixed(1)},${y(v).toFixed(1)}`).join("");
  const last = values.length - 1;
  return (
    <svg width={W} height={H} viewBox={`0 0 ${W} ${H}`} role="img" aria-label={label} className={`shrink-0 ${className}`}>
      <title>{label}</title>
      <path d={`${d}L${x(last)},${H}L${x(0)},${H}Z`} fill="currentColor" opacity={0.08} />
      <path d={d} fill="none" stroke="#52525b" strokeWidth={1.5} strokeLinejoin="round" strokeLinecap="round" />
      <circle cx={x(last)} cy={y(values[last]!)} r={3} fill="currentColor" stroke="var(--chart-surface)" strokeWidth={1.5} />
    </svg>
  );
}
