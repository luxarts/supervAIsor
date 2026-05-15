export type Status = "working" | "done" | "stale";

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
  last_error_at?: string;
  project_dir_encoded?: string;
}

export type Frame =
  | { kind: "snapshot"; sessions: Session[] }
  | { kind: "update"; session: Session }
  | { kind: "delete"; session_id: string; hostname: string }
  | { kind: "pollers"; online: Record<string, boolean> }
  | { kind: "session_removed"; hostname: string; id: string };
