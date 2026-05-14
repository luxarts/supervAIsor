export type Status = "working" | "waiting_input" | "idle" | "stale";

export interface Session {
  id: string;
  hostname: string;
  name: string;
  project: string;
  status: Status;
  started_at: string;
  last_prompt_at?: string;
  current_action: string;
  last_event_at: string;
}

export type Frame =
  | { kind: "snapshot"; sessions: Session[] }
  | { kind: "update"; session: Session }
  | { kind: "delete"; session_id: string; hostname: string };
