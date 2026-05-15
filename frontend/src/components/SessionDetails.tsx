import { useEffect, useState, useSyncExternalStore } from "react";
import { isPinned, togglePin, subscribe as subscribePins } from "../lib/pins";
import { isNotifyEnabled, toggleNotify, subscribeNotify } from "../lib/notify";
import { deleteSession } from "../lib/deleteSession";

interface Tokens {
  input: number;
  output: number;
  cache_creation: number;
  cache_read: number;
}
interface Counts {
  user_prompts: number;
  assistant_turns: number;
  tool_calls: number;
  errors: number;
}
interface Stats {
  hostname: string;
  id: string;
  name: string;
  project: string;
  project_dir_encoded?: string;
  model?: string;
  started_at: string;
  last_event_at: string;
  wall_clock_seconds: number;
  tokens: Tokens;
  counts: Counts;
  tool_breakdown: Record<string, number>;
}

interface Props {
  sessionId: string;
  hostname: string;
  lastEventAt: string;
  backendHttpBase: string;
  pollerOnline?: boolean;
  onDeleted?: () => void;
}

export function SessionDetails({
  sessionId,
  hostname,
  lastEventAt,
  backendHttpBase,
  pollerOnline,
  onDeleted,
}: Props) {
  const [stats, setStats] = useState<Stats | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [deleteState, setDeleteState] = useState<
    | { phase: "idle" }
    | { phase: "confirming" }
    | { phase: "deleting" }
    | { phase: "error"; message: string }
  >({ phase: "idle" });

  const settingsKey = `${hostname}:${sessionId}`;
  const pinned = useSyncExternalStore(subscribePins, () => isPinned(settingsKey));
  const notify = useSyncExternalStore(subscribeNotify, () => isNotifyEnabled(settingsKey));

  useEffect(() => {
    let cancelled = false;
    fetch(
      `${backendHttpBase}/sessions/${encodeURIComponent(hostname)}/${encodeURIComponent(sessionId)}/stats?_=${Date.now()}`,
      { cache: "no-store" },
    )
      .then(async (r) => {
        if (!r.ok) throw new Error(`HTTP ${r.status}`);
        return (await r.json()) as Stats;
      })
      .then((d) => {
        if (!cancelled) setStats(d);
      })
      .catch((e) => {
        if (!cancelled) setError(String(e));
      });
    return () => {
      cancelled = true;
    };
  }, [sessionId, hostname, lastEventAt, backendHttpBase]);

  // Auto-revert confirmation after 5s.
  useEffect(() => {
    if (deleteState.phase !== "confirming") return;
    const t = setTimeout(() => setDeleteState({ phase: "idle" }), 5000);
    return () => clearTimeout(t);
  }, [deleteState]);

  if (error) {
    return <div className="font-hud text-rd p-3">// STATS LOST: {error}</div>;
  }
  if (!stats) {
    return <div className="font-hud text-dim p-3">// LOADING…</div>;
  }

  const fmtDur = (s: number) => {
    const h = Math.floor(s / 3600);
    const m = Math.floor((s % 3600) / 60);
    const sec = s % 60;
    if (h > 0) return `${h}h ${m}m ${sec}s`;
    if (m > 0) return `${m}m ${sec}s`;
    return `${sec}s`;
  };
  const fmtNum = (n: number) => n.toLocaleString();
  const breakdown = Object.entries(stats.tool_breakdown).sort((a, b) => b[1] - a[1]);
  const maxTool = breakdown.reduce((m, [, v]) => Math.max(m, v), 0) || 1;

  const onDeleteClick = async () => {
    if (deleteState.phase === "idle") {
      setDeleteState({ phase: "confirming" });
      return;
    }
    if (deleteState.phase === "confirming") {
      setDeleteState({ phase: "deleting" });
      try {
        await deleteSession(backendHttpBase, hostname, sessionId);
        onDeleted?.();
      } catch (e) {
        setDeleteState({ phase: "error", message: e instanceof Error ? e.message : String(e) });
      }
    }
  };

  const buttonLabel =
    deleteState.phase === "deleting"
      ? "DELETING…"
      : deleteState.phase === "confirming"
      ? "CONFIRM DELETE"
      : "DELETE SESSION";
  const deleteDisabled =
    pollerOnline !== true || deleteState.phase === "deleting";
  const fullPath = stats?.project_dir_encoded
    ? `~/.claude/projects/${stats.project_dir_encoded}/${sessionId}.jsonl`
    : "(path unavailable)";

  return (
    <div className="space-y-4 font-hud text-sm">
      <Section title="OVERVIEW">
        <Row k="PROJECT" v={stats.project} />
        <Row k="HOST" v={stats.hostname} />
        {stats.model && <Row k="MODEL" v={stats.model} />}
        <Row k="STARTED" v={new Date(stats.started_at).toLocaleString()} />
        <Row k="LAST EVENT" v={new Date(stats.last_event_at).toLocaleString()} />
        <Row k="DURATION" v={fmtDur(stats.wall_clock_seconds)} />
      </Section>

      <Section title="TOKENS">
        <Row k="Input" v={fmtNum(stats.tokens.input)} />
        <Row k="Output" v={fmtNum(stats.tokens.output)} />
        <Row k="Cache create" v={fmtNum(stats.tokens.cache_creation)} />
        <Row k="Cache read" v={fmtNum(stats.tokens.cache_read)} />
      </Section>

      <Section title="COUNTS">
        <Row k="User prompts" v={fmtNum(stats.counts.user_prompts)} />
        <Row k="Assistant turns" v={fmtNum(stats.counts.assistant_turns)} />
        <Row k="Tool calls" v={fmtNum(stats.counts.tool_calls)} />
        <Row k="Errors" v={fmtNum(stats.counts.errors)} />
      </Section>

      <Section title="TOOL BREAKDOWN">
        {breakdown.length === 0 && <div className="text-dim">// NONE</div>}
        {breakdown.map(([name, count]) => (
          <div key={name} className="flex items-center gap-2">
            <span className="w-24 text-cy">{name}</span>
            <div className="relative h-3 flex-1 bg-cy/10">
              <div
                className="absolute inset-y-0 left-0 bg-cy/60"
                style={{ width: `${(count / maxTool) * 100}%` }}
              />
            </div>
            <span className="w-10 text-right text-yl">{count}</span>
          </div>
        ))}
      </Section>

      <Section title="SETTINGS">
        <SwitchRow
          label="📌 Pin to top"
          checked={pinned}
          onChange={() => togglePin(settingsKey)}
        />
        <SwitchRow
          label="🔔 Notify on turn complete"
          checked={notify}
          onChange={() => {
            // First-time enable triggers Notification permission prompt
            // (best-effort; failure is silently swallowed).
            if (!notify && typeof window !== "undefined" && "Notification" in window) {
              if (Notification.permission === "default") {
                Notification.requestPermission().catch(() => {});
              }
            }
            toggleNotify(settingsKey);
          }}
        />
      </Section>

      <Section title="DANGER ZONE">
        <Row k="Path on host" v={fullPath} />
        {deleteState.phase === "error" && (
          <div className="mt-2 border border-rd bg-rd/10 p-2 text-rd text-xs">
            // {deleteState.message}
          </div>
        )}
        <button
          type="button"
          onClick={onDeleteClick}
          disabled={deleteDisabled}
          aria-label="Delete session"
          title={pollerOnline ? "" : `Poller offline — start the poller on ${hostname} to enable`}
          className={`mt-2 w-full border px-3 py-2 font-hud text-xs uppercase tracking-widest
                      touch-manipulation transition-colors
                      ${deleteDisabled ? "border-dim text-dim cursor-not-allowed" : "border-rd text-rd hover:bg-rd/10"}`}
        >
          {buttonLabel}
        </button>
      </Section>
    </div>
  );
}

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <section>
      <div className="mb-2 border-b border-cy/20 pb-1 text-[11px] uppercase tracking-widest text-cy">
        {title}
      </div>
      <div className="space-y-1">{children}</div>
    </section>
  );
}

function Row({ k, v }: { k: string; v: string }) {
  return (
    <div className="flex items-center justify-between gap-3">
      <span className="text-dim">{k}</span>
      <span className="text-txt break-all text-right">{v}</span>
    </div>
  );
}

function SwitchRow({
  label,
  checked,
  onChange,
}: {
  label: string;
  checked: boolean;
  onChange: () => void;
}) {
  return (
    <div className="flex items-center justify-between gap-3">
      <span className="text-txt">{label}</span>
      <button
        type="button"
        role="switch"
        aria-label={label}
        aria-checked={checked}
        onClick={onChange}
        className={`h-6 w-12 border touch-manipulation transition-colors
                    ${checked ? "border-cy bg-cy/30" : "border-cy/30 bg-bg-panel"}`}
      >
        <span
          aria-hidden
          className={`block h-full w-1/2 transition-transform
                      ${checked ? "translate-x-full bg-cy" : "translate-x-0 bg-dim"}`}
        />
      </button>
    </div>
  );
}
