import { useEffect, useMemo, useState, useSyncExternalStore } from "react";
import { useSessionsSocket } from "./useSessionsSocket";
import { SessionCard } from "./components/SessionCard";
import { ScanlineOverlay } from "./components/ScanlineOverlay";
import { HeaderControls } from "./components/HeaderControls";
import { ConversationModal } from "./components/ConversationModal";
import { shortProject } from "./lib/path";
import { backendHttpBase } from "./lib/backendUrl";
import { partitionAndSort, type SortKey } from "./lib/sort";
import { listPinned, subscribe as subscribePins } from "./lib/pins";
import {
  listNotify,
  subscribeNotify,
  useTurnCompletionNotifier,
  useErrorFlash,
} from "./lib/notify";

function defaultWsUrl(): string {
  if (typeof window === "undefined") return "ws://localhost:8080/ws/clients";
  const proto = window.location.protocol === "https:" ? "wss:" : "ws:";
  return `${proto}//${window.location.hostname}:8080/ws/clients`;
}

const WS_URL =
  (import.meta.env.VITE_BACKEND_WS as string | undefined) ?? defaultWsUrl();

const HTTP_BASE = backendHttpBase(WS_URL);
const HIDE_STALE_KEY = "sv:hideStale";
const SORT_KEY = "sv:sort";

const isSortKey = (v: string | null): v is SortKey =>
  v === "last_update" || v === "name" || v === "host" || v === "status";

const FLASH_MS = 1200;

export default function App() {
  const { sessions, connected } = useSessionsSocket(WS_URL);

  const [hideStale, setHideStale] = useState<boolean>(() => {
    const v = localStorage.getItem(HIDE_STALE_KEY);
    return v === null ? true : v === "1";
  });
  const [sort, setSort] = useState<SortKey>(() => {
    const v = localStorage.getItem(SORT_KEY);
    return isSortKey(v) ? v : "last_update";
  });
  const [query, setQuery] = useState("");
  const [open, setOpen] = useState<{ hostname: string; id: string } | null>(null);
  const [newCompletions, setNewCompletions] = useState(0);
  const [flashes, setFlashes] = useState<Map<string, "complete" | "error">>(
    () => new Map(),
  );

  useEffect(() => {
    localStorage.setItem(HIDE_STALE_KEY, hideStale ? "1" : "0");
  }, [hideStale]);
  useEffect(() => {
    localStorage.setItem(SORT_KEY, sort);
  }, [sort]);

  const pinned = useSyncExternalStore(subscribePins, () => listPinned());
  const notify = useSyncExternalStore(subscribeNotify, () => listNotify());

  const fireFlash = (key: string, kind: "complete" | "error") => {
    setFlashes((prev) => {
      const next = new Map(prev);
      next.set(key, kind);
      return next;
    });
    setTimeout(() => {
      setFlashes((prev) => {
        const next = new Map(prev);
        if (next.get(key) === kind) next.delete(key);
        return next;
      });
    }, FLASH_MS);
  };

  // Track turn completions: beep + completion flash + browser notification + counter bump.
  useTurnCompletionNotifier(sessions, (key) => {
    fireFlash(key, "complete");
    setNewCompletions((n) => n + 1);
    if (
      typeof window !== "undefined" &&
      "Notification" in window &&
      Notification.permission === "granted" &&
      document.hidden
    ) {
      const sess = sessions.find((s) => `${s.hostname}:${s.id}` === key);
      try {
        new Notification(sess?.name ?? "Claude session", {
          body: "Turn complete",
          tag: key,
        });
      } catch {
        // ignore
      }
    }
  });

  // Track error transitions: error flash overrides any in-flight completion flash.
  // useErrorFlash already returns only the delta (newly-entered error-active keys),
  // so we can fire directly without a second dedup layer.
  const { errorActive, freshErrors } = useErrorFlash(sessions);
  useEffect(() => {
    for (const key of freshErrors) {
      fireFlash(key, "error");
    }
  }, [freshErrors]);

  // Counts visible to the user (computed from raw sessions, not the filtered view).
  const counts = useMemo(() => {
    let working = 0;
    let done = 0;
    let stale = 0;
    for (const s of sessions) {
      if (s.status === "working") working++;
      else if (s.status === "done") done++;
      else if (s.status === "stale") stale++;
    }
    return { working, done, stale };
  }, [sessions]);

  const sorted = useMemo(() => {
    const q = query.trim().toLowerCase();
    const filtered = sessions.filter((s) => {
      if (hideStale && s.status === "stale") return false;
      if (!q) return true;
      const project = shortProject(s.project).toLowerCase();
      return s.name.toLowerCase().includes(q) || project.includes(q);
    });
    return partitionAndSort(filtered, pinned, sort);
  }, [sessions, hideStale, query, sort, pinned]);

  const openSession = open
    ? sessions.find((s) => s.id === open.id && s.hostname === open.hostname) ?? null
    : null;

  return (
    <div className="min-h-full p-4" onClick={() => setNewCompletions(0)}>
      <ScanlineOverlay />
      <header className="mb-4">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <h1 className="font-hud text-2xl tracking-widest text-cy">
            SUPERV<span className="text-yl">AI</span>SOR
          </h1>
          <div className="flex items-center gap-3 font-hud text-xs">
            <span className="text-cy">▶ {counts.working}</span>
            <span className="text-yl">✓ {counts.done}</span>
            <span className="text-rd">💀 {counts.stale}</span>
            {newCompletions > 0 && (
              <span className="text-yl animate-pulseDot">🔔 {newCompletions} NEW</span>
            )}
            <span
              className={`${connected ? "text-cy" : "text-rd animate-glitch"}`}
            >
              {connected ? "// LINK OK" : "// DISCONNECTED"}
            </span>
          </div>
        </div>
        <HeaderControls
          hideStale={hideStale}
          onToggleHideStale={() => setHideStale((v) => !v)}
          query={query}
          onQueryChange={setQuery}
          sort={sort}
          onSortChange={setSort}
        />
      </header>

      {sorted.length === 0 ? (
        <div className="mt-12 text-center font-hud text-dim">
          {sessions.length === 0 ? "NO SESSIONS DETECTED" : "NO MATCHES"}
        </div>
      ) : (
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">
          {sorted.map((s) => {
            const key = `${s.hostname}:${s.id}`;
            return (
              <SessionCard
                key={key}
                session={s}
                onOpen={() => setOpen({ hostname: s.hostname, id: s.id })}
                pinned={pinned.has(key)}
                notify={notify.has(key)}
                errorActive={errorActive.has(key)}
                flash={flashes.get(key) ?? null}
              />
            );
          })}
        </div>
      )}

      {openSession && (
        <ConversationModal
          sessionId={openSession.id}
          hostname={openSession.hostname}
          sessionName={openSession.name}
          project={openSession.project}
          lastEventAt={openSession.last_event_at}
          backendHttpBase={HTTP_BASE}
          onClose={() => setOpen(null)}
        />
      )}
    </div>
  );
}
