export type Status = "working" | "waiting_input" | "idle" | "stale";

export interface Session {
  id: string;
  name: string;
  project: string;
  status: Status;
  started_at: string;       // ISO-8601
  last_prompt_at?: string;
  current_action: string;
  last_event_at: string;
}

export type Frame =
  | { kind: "snapshot"; sessions: Session[] }
  | { kind: "update"; session: Session }
  | { kind: "delete"; session_id: string };
