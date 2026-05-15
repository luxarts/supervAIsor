import { useEffect, useRef, useState } from "react";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import { shortProject } from "../lib/path";
import { SessionDetails } from "./SessionDetails";

interface ContentBlock {
  type: string;
  text?: string;
  name?: string;
  id?: string;
  tool_use_id?: string;
}

interface RawLine {
  type?: string;
  message?: {
    role?: string;
    content?: string | ContentBlock[];
  };
}

interface Event {
  ts: string;
  type: string;
  payload: RawLine;
}

interface Props {
  sessionId: string;
  hostname: string;
  sessionName: string;
  project: string;
  lastEventAt: string;
  backendHttpBase: string;
  onClose: () => void;
  pollerOnline?: boolean;
}

export function ConversationModal({
  sessionId,
  hostname,
  sessionName,
  project,
  lastEventAt,
  backendHttpBase,
  onClose,
  pollerOnline,
}: Props) {
  const [events, setEvents] = useState<Event[] | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [tab, setTab] = useState<"conversation" | "details">("conversation");
  const bodyRef = useRef<HTMLDivElement>(null);
  const rootRef = useRef<HTMLDivElement>(null);

  // Reset loading state only when switching to a different session.
  useEffect(() => {
    setEvents(null);
    setError(null);
    setTab("conversation");
  }, [sessionId, hostname]);

  // Refetch on mount, on session change, and whenever the underlying session
  // gets a new event (lastEventAt changes via the WS-driven props).
  useEffect(() => {
    let cancelled = false;
    fetch(
      `${backendHttpBase}/sessions/${encodeURIComponent(hostname)}/${encodeURIComponent(sessionId)}/events?limit=500&_=${Date.now()}`,
      { cache: "no-store" },
    )
      .then(async (r) => {
        if (!r.ok) throw new Error(`HTTP ${r.status}`);
        return (await r.json()) as Event[];
      })
      .then((data) => {
        if (!cancelled) setEvents(data);
      })
      .catch((e) => {
        if (!cancelled) setError(String(e));
      });
    return () => {
      cancelled = true;
    };
  }, [sessionId, hostname, lastEventAt, backendHttpBase]);

  useEffect(() => {
    if (tab === "conversation" && events && bodyRef.current) {
      bodyRef.current.scrollTop = bodyRef.current.scrollHeight;
    }
  }, [events, tab]);

  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
    };
    window.addEventListener("keydown", handler);
    return () => window.removeEventListener("keydown", handler);
  }, [onClose]);

  useEffect(() => {
    if (typeof window === "undefined") return;
    try {
      if (typeof window.matchMedia !== "function") return;
      if (!window.matchMedia("(pointer: coarse)").matches) return;
    } catch {
      return;
    }
    const root = rootRef.current;
    const body = bodyRef.current;
    if (!root || !body) return;

    let startY = 0;
    let active = false;

    const onStart = (e: TouchEvent) => {
      if (e.touches.length !== 1) return;
      if (body.scrollTop > 0) return;
      active = true;
      startY = e.touches[0].clientY;
    };
    const onMove = (e: TouchEvent) => {
      if (!active) return;
      const dy = e.touches[0].clientY - startY;
      if (dy > 0) {
        root.style.transform = `translateY(${dy}px)`;
        root.style.transition = "none";
      }
    };
    const onEnd = (e: TouchEvent) => {
      if (!active) return;
      active = false;
      const dy = (e.changedTouches[0]?.clientY ?? startY) - startY;
      root.style.transition = "transform 150ms ease-out";
      if (dy > 80) {
        root.style.transform = `translateY(100vh)`;
        setTimeout(onClose, 150);
      } else {
        root.style.transform = "translateY(0)";
      }
    };

    root.addEventListener("touchstart", onStart, { passive: true });
    root.addEventListener("touchmove", onMove, { passive: true });
    root.addEventListener("touchend", onEnd);
    return () => {
      root.removeEventListener("touchstart", onStart);
      root.removeEventListener("touchmove", onMove);
      root.removeEventListener("touchend", onEnd);
    };
  }, [onClose, events]);

  return (
    <div
      className="fixed inset-0 z-50 flex items-stretch sm:items-center sm:justify-center"
      onClick={onClose}
    >
      <div className="absolute inset-0 bg-black/70" />
      <div
        ref={rootRef}
        onClick={(e) => e.stopPropagation()}
        className="relative z-10 flex h-full w-full flex-col bg-bg-panel
                   sm:h-auto sm:max-h-[90vh] sm:w-full sm:max-w-3xl
                   border border-cy/40 shadow-[0_0_30px_rgba(0,240,255,0.2)]"
      >
        <div className="grid place-items-center pt-2 sm:hidden">
          <div className="h-1 w-12 rounded bg-cy/40" aria-hidden />
        </div>

        <header className="flex items-start justify-between gap-3 border-b border-cy/20 p-3">
          <div className="min-w-0">
            <div className="truncate font-hud text-base uppercase tracking-wider text-txt">
              {sessionName}
            </div>
            <div className="truncate font-hud text-[10px] text-dim">
              {shortProject(project)}
            </div>
          </div>
          <button
            type="button"
            aria-label="Close"
            onClick={onClose}
            className="grid h-12 w-12 flex-shrink-0 place-items-center
                       font-hud text-2xl text-cy hover:bg-cy/10 touch-manipulation"
          >
            ×
          </button>
        </header>

        <div role="tablist" className="flex border-b border-cy/20">
          <button
            type="button"
            role="tab"
            aria-selected={tab === "conversation"}
            onClick={() => setTab("conversation")}
            className={`flex-1 py-2 font-hud text-xs uppercase tracking-widest min-h-[44px] touch-manipulation
                        ${tab === "conversation" ? "text-cy border-b-2 border-cy" : "text-dim hover:text-cy"}`}
          >
            CONVERSATION
          </button>
          <button
            type="button"
            role="tab"
            aria-selected={tab === "details"}
            onClick={() => setTab("details")}
            className={`flex-1 py-2 font-hud text-xs uppercase tracking-widest min-h-[44px] touch-manipulation
                        ${tab === "details" ? "text-cy border-b-2 border-cy" : "text-dim hover:text-cy"}`}
          >
            DETAILS
          </button>
        </div>

        <div
          ref={bodyRef}
          className="flex-1 overflow-y-auto overflow-x-hidden overscroll-contain p-3 space-y-3"
        >
          {tab === "conversation" ? (
            <>
              {error && (
                <div className="font-hud text-rd">// EVENT FEED LOST: {error}</div>
              )}
              {!error && events === null && (
                <div className="font-hud text-dim">// LOADING…</div>
              )}
              {!error && events && events.length === 0 && (
                <div className="font-hud text-dim">// NO MESSAGES</div>
              )}
              {!error && events && events.map((ev, i) => (
                <MessageView key={i} ev={ev} />
              ))}
            </>
          ) : (
            <SessionDetails
              sessionId={sessionId}
              hostname={hostname}
              lastEventAt={lastEventAt}
              backendHttpBase={backendHttpBase}
              pollerOnline={pollerOnline}
              onDeleted={onClose}
            />
          )}
        </div>
      </div>
    </div>
  );
}

function MessageView({ ev }: { ev: Event }) {
  const role = ev.payload?.message?.role ?? ev.payload?.type ?? ev.type;
  const raw = ev.payload?.message?.content;

  // Plain user prompts come as a single string; normalize to a text block.
  const blocks: ContentBlock[] =
    typeof raw === "string"
      ? raw.trim()
        ? [{ type: "text", text: raw }]
        : []
      : raw ?? [];

  if (blocks.length === 0) return null;

  // Keep only conversational text. Tool calls/results are noisy and not
  // useful in a chat-style view.
  const renderable = blocks.filter((b) => b.type === "text");
  if (renderable.length === 0) return null;

  const isAssistant = role === "assistant";
  const alignment = isAssistant ? "ml-auto" : "mr-auto";
  const border = isAssistant ? "border-yl/40" : "border-cy/40";
  const text = isAssistant ? "text-yl/90" : "text-cy/90";

  return (
    <div
      className={`max-w-full border ${border} ${alignment} bg-bg-panel/60 p-2
                  break-words min-w-0 sm:max-w-[85%]`}
    >
      <div className={`mb-2 font-hud text-[10px] uppercase ${text}`}>{role}</div>
      <div className="space-y-2 min-w-0">
        {renderable.map((b, i) => (
          <div key={i} className="md text-sm break-words min-w-0">
            <ReactMarkdown remarkPlugins={[remarkGfm]}>
              {b.text ?? ""}
            </ReactMarkdown>
          </div>
        ))}
      </div>
    </div>
  );
}
