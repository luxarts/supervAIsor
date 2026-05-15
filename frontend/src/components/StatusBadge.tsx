import type { Status } from "../types";

const STYLES: Record<Status, { label: string; color: string; bg: string }> = {
  working: { label: "WORKING", color: "text-cy",  bg: "border-cy/60" },
  done:    { label: "DONE",    color: "text-yl",  bg: "border-yl/60" },
  stale:   { label: "STALE",   color: "text-rd",  bg: "border-rd/60" },
};

export function StatusBadge({ status }: { status: Status }) {
  const s = STYLES[status];
  return (
    <span className={`inline-flex items-center gap-2 border ${s.bg} ${s.color} px-2 py-1 text-xs font-hud tracking-widest`}>
      <span
        aria-hidden
        className={`h-2 w-2 rounded-full bg-current ${status === "working" ? "animate-pulseDot" : ""}`}
      />
      {s.label}
    </span>
  );
}
