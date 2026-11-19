"use client";

import { useState } from "react";

import { formatCount } from "@/lib/format";

/**
 * A count that glows briefly when it changes between polls, so readers see what just moved.
 * Large standalone values use proportional figures; pass `tabular-nums` in className where numbers align in columns.
 */
export function LiveNumber({ value, className = "" }: { value: number; className?: string }) {
  const [shown, setShown] = useState(value);
  const [change, setChange] = useState<{ n: number; up: boolean }>({ n: 0, up: true });
  if (value !== shown) {
    // Adjusting state during render (not in an effect) is React's pattern for reacting to prop changes.
    setChange({ n: change.n + 1, up: value > shown });
    setShown(value);
  }
  const anim = change.n === 0 ? "" : change.up ? "animate-flash-up" : "animate-flash-down";
  return (
    <span key={change.n} className={`rounded ${anim} ${className}`}>
      {formatCount(value)}
    </span>
  );
}
