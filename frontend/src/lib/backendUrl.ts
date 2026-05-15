// Derives the HTTP base URL of the backend from the WS URL the app uses.
// Keeps any path prefix (so we work behind a reverse proxy that mounts the
// backend under a sub-path), only stripping the trailing /ws/<endpoint>.
// E.g. "ws://host:8080/ws/clients"          -> "http://host:8080"
//      "ws://host/supervaisor/ws/clients"   -> "http://host/supervaisor"
export function backendHttpBase(wsUrl: string): string {
  let s = wsUrl;
  if (s.startsWith("wss://")) s = "https://" + s.slice("wss://".length);
  else if (s.startsWith("ws://")) s = "http://" + s.slice("ws://".length);
  try {
    const u = new URL(s);
    const path = u.pathname.replace(/\/ws\/[^/]+\/?$/, "");
    return `${u.protocol}//${u.host}${path}`;
  } catch {
    return s;
  }
}
