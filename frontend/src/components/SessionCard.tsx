import { useEffect, useState } from "react";
import type { Session } from "../types";
import { StatusBadge } from "./StatusBadge";
import { formatDuration } from "../lib/time";

export function SessionCard({ session }: { session: Session }) {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const t = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(t);
  }, []);

  const total = now - new Date(session.started_at).getTime();
  const prompt = session.last_prompt_at
    ? now - new Date(session.last_prompt_at).getTime()
    : null;

  return (
    <article
      key={session.id}
      className={`relative min-h-[160px] border bg-bg-panel p-4 transition-colors
                  border-cy/30 hover:border-cy active:scale-[0.99]
                  ${session.status === "working" ? "shadow-[0_0_18px_rgba(0,240,255,0.25)]" : ""}`}
    >
      <header className="flex items-start justify-between gap-2">
        <StatusBadge status={session.status} />
        <div className="font-hud text-[10px] text-dim truncate max-w-[55%]">{session.project}</div>
      </header>

      <h2 className="mt-3 font-hud text-xl uppercase tracking-wider text-txt truncate">
        {session.name}
      </h2>

      <p className="mt-2 line-clamp-2 font-mono text-xs text-cy/80">
        {session.current_action || "—"}
      </p>

      <footer className="mt-auto flex items-end justify-between pt-4 font-hud text-xs">
        <div>
          <div className="text-dim">TOTAL</div>
          <div className="text-cy text-base">{formatDuration(total)}</div>
        </div>
        {prompt !== null && session.status === "working" && (
          <div className="text-right">
            <div className="text-dim">PROMPT</div>
            <div className="text-yl text-base">{formatDuration(prompt)}</div>
          </div>
        )}
      </footer>
    </article>
  );
}
