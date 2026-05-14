import { useEffect, useRef, useState } from "react";
import type { Session, Frame } from "./types";

export interface SocketState {
  sessions: Session[];
  connected: boolean;
}

export function useSessionsSocket(url: string): SocketState {
  const [sessions, setSessions] = useState<Session[]>([]);
  const [connected, setConnected] = useState(false);
  const wsRef = useRef<WebSocket | null>(null);
  const retryRef = useRef<number>(0);

  useEffect(() => {
    let stopped = false;

    const connect = () => {
      if (stopped) return;
      const ws = new WebSocket(url);
      wsRef.current = ws;

      ws.onopen = () => {
        setConnected(true);
        retryRef.current = 0;
      };

      ws.onmessage = (ev) => {
        try {
          const frame = JSON.parse(ev.data) as Frame;
          if (frame.kind === "snapshot") {
            setSessions(frame.sessions);
          } else if (frame.kind === "update") {
            setSessions((prev) => {
              const idx = prev.findIndex((s) => s.id === frame.session.id);
              if (idx < 0) return [frame.session, ...prev];
              const copy = prev.slice();
              copy[idx] = frame.session;
              return copy;
            });
          } else if (frame.kind === "delete") {
            setSessions((prev) => prev.filter((s) => s.id !== frame.session_id));
          }
        } catch {
          // ignore malformed frames
        }
      };

      ws.onclose = () => {
        setConnected(false);
        if (stopped) return;
        const delay = Math.min(15000, 1000 * Math.pow(2, retryRef.current++));
        setTimeout(connect, delay);
      };

      ws.onerror = () => {
        ws.close();
      };
    };

    connect();
    return () => {
      stopped = true;
      wsRef.current?.close();
    };
  }, [url]);

  return { sessions, connected };
}
