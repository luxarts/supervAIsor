import { useSessionsSocket } from "./useSessionsSocket";
import { SessionCard } from "./components/SessionCard";
import { ScanlineOverlay } from "./components/ScanlineOverlay";

const WS_URL =
  (import.meta.env.VITE_BACKEND_WS as string | undefined) ??
  "ws://localhost:8080/ws/clients";

export default function App() {
  const { sessions, connected } = useSessionsSocket(WS_URL);

  return (
    <div className="min-h-full p-4">
      <ScanlineOverlay />
      <header className="mb-4 flex items-center justify-between">
        <h1 className="font-hud text-2xl tracking-widest text-cy">
          SUPERV<span className="text-yl">AI</span>SOR
        </h1>
        <span className={`font-hud text-xs ${connected ? "text-cy" : "text-rd animate-glitch"}`}>
          {connected ? "// LINK OK" : "// DISCONNECTED"}
        </span>
      </header>

      {sessions.length === 0 ? (
        <div className="mt-12 text-center font-hud text-dim">NO SESSIONS DETECTED</div>
      ) : (
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">
          {sessions.map((s) => (
            <SessionCard key={s.id} session={s} />
          ))}
        </div>
      )}
    </div>
  );
}
