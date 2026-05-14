import { useEffect, useMemo, useState } from "react";
import { useSessionsSocket } from "./useSessionsSocket";
import { SessionCard } from "./components/SessionCard";
import { ScanlineOverlay } from "./components/ScanlineOverlay";
import { HeaderControls } from "./components/HeaderControls";
import { ConversationModal } from "./components/ConversationModal";
import { shortProject } from "./lib/path";
import { backendHttpBase } from "./lib/backendUrl";

const WS_URL =
  (import.meta.env.VITE_BACKEND_WS as string | undefined) ??
  "ws://localhost:8080/ws/clients";

const HTTP_BASE = backendHttpBase(WS_URL);
const HIDE_STALE_KEY = "sv:hideStale";

export default function App() {
  const { sessions, connected } = useSessionsSocket(WS_URL);

  const [hideStale, setHideStale] = useState<boolean>(() => {
    const v = localStorage.getItem(HIDE_STALE_KEY);
    return v === null ? true : v === "1";
  });
  const [query, setQuery] = useState("");
  const [openId, setOpenId] = useState<string | null>(null);

  useEffect(() => {
    localStorage.setItem(HIDE_STALE_KEY, hideStale ? "1" : "0");
  }, [hideStale]);

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    return sessions.filter((s) => {
      if (hideStale && s.status === "stale") return false;
      if (!q) return true;
      const project = shortProject(s.project).toLowerCase();
      const name = s.name.toLowerCase();
      return name.includes(q) || project.includes(q);
    });
  }, [sessions, hideStale, query]);

  const openSession = openId
    ? sessions.find((s) => s.id === openId) ?? null
    : null;

  return (
    <div className="min-h-full p-4">
      <ScanlineOverlay />
      <header className="mb-4">
        <div className="flex items-center justify-between">
          <h1 className="font-hud text-2xl tracking-widest text-cy">
            SUPERV<span className="text-yl">AI</span>SOR
          </h1>
          <span
            className={`font-hud text-xs ${connected ? "text-cy" : "text-rd animate-glitch"}`}
          >
            {connected ? "// LINK OK" : "// DISCONNECTED"}
          </span>
        </div>
        <HeaderControls
          hideStale={hideStale}
          onToggleHideStale={() => setHideStale((v) => !v)}
          query={query}
          onQueryChange={setQuery}
        />
      </header>

      {filtered.length === 0 ? (
        <div className="mt-12 text-center font-hud text-dim">
          {sessions.length === 0 ? "NO SESSIONS DETECTED" : "NO MATCHES"}
        </div>
      ) : (
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">
          {filtered.map((s) => (
            <SessionCard key={s.id} session={s} onOpen={setOpenId} />
          ))}
        </div>
      )}

      {openSession && (
        <ConversationModal
          sessionId={openSession.id}
          sessionName={openSession.name}
          project={openSession.project}
          backendHttpBase={HTTP_BASE}
          onClose={() => setOpenId(null)}
        />
      )}
    </div>
  );
}
