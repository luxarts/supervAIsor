// Derives the HTTP base URL of the backend from the WS URL the app uses.
// E.g. "ws://localhost:8080/ws/clients" -> "http://localhost:8080".
export function backendHttpBase(wsUrl: string): string {
  let s = wsUrl;
  if (s.startsWith("wss://")) s = "https://" + s.slice("wss://".length);
  else if (s.startsWith("ws://")) s = "http://" + s.slice("ws://".length);
  try {
    const u = new URL(s);
    return `${u.protocol}//${u.host}`;
  } catch {
    return s;
  }
}
