"use client";

import { useEffect, useRef, useState } from "react";

/**
 * Renders children only once the placeholder is near the viewport, so below-the-fold sections
 * (and their lazily imported code) don't count toward the first load. The placeholder keeps the
 * section's height, so nothing shifts when it appears.
 */
export function WhenVisible({ minHeight, children }: { minHeight: number; children: React.ReactNode }) {
  const ref = useRef<HTMLDivElement>(null);
  const [shown, setShown] = useState(false);
  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    const io = new IntersectionObserver(
      ([entry]) => {
        if (entry?.isIntersecting) {
          setShown(true);
          io.disconnect();
        }
      },
      { rootMargin: "400px" },
    );
    io.observe(el);
    return () => io.disconnect();
  }, []);
  return (
    <div ref={ref} style={shown ? undefined : { minHeight }}>
      {shown ? children : null}
    </div>
  );
}
