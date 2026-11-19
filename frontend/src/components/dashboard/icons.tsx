// Inline stroke icons (24×24 grid), so the dashboard ships no icon library.
const PATHS = {
  check: "M9 12l2 2 4-4M21 12a9 9 0 1 1-18 0 9 9 0 0 1 18 0Z",
  cross: "M15 9l-6 6m0-6 6 6M21 12a9 9 0 1 1-18 0 9 9 0 0 1 18 0Z",
  retry: "M3 12a9 9 0 1 0 3-6.7M3 4v5h5",
  layers: "m12 3 9 5-9 5-9-5 9-5Zm-9 9 9 5 9-5M3 16l9 5 9-5",
  workers: "M16 19v-1a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v1M9 10a3 3 0 1 0 0-6 3 3 0 0 0 0 6Zm13 9v-1a4 4 0 0 0-3-3.9M16 4.1a3 3 0 0 1 0 5.8",
  clock: "M12 7v5l3 2M21 12a9 9 0 1 1-18 0 9 9 0 0 1 18 0Z",
  dead: "M12 3a7 7 0 0 0-4 12.7V19h8v-3.3A7 7 0 0 0 12 3Zm-2 9h.01M14 12h.01M10 19v2m4-2v2",
  pulse: "M3 12h4l3-8 4 16 3-8h4",
  ban: "M5.6 5.6l12.8 12.8M21 12a9 9 0 1 1-18 0 9 9 0 0 1 18 0Z",
  send: "M22 2 11 13M22 2l-7 20-4-9-9-4 20-7Z",
  shuffle: "M16 3h5v5M4 20 21 3M21 16v5h-5M15 15l6 6M4 4l5 5",
  arrow: "M5 12h14m-6-6 6 6-6 6",
  play: "M6 4l14 8-14 8V4Z",
} as const;

export type IconName = keyof typeof PATHS;

export function Icon({ name, className = "size-5" }: { name: IconName; className?: string }) {
  return (
    <svg
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth={2}
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
      className={className}
    >
      <path d={PATHS[name]} />
    </svg>
  );
}
