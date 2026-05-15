export async function deleteSession(
  backendHttpBase: string,
  hostname: string,
  id: string,
): Promise<void> {
  const r = await fetch(
    `${backendHttpBase}/sessions/${encodeURIComponent(hostname)}/${encodeURIComponent(id)}`,
    { method: "DELETE" },
  );
  if (r.ok) return;
  let msg = `HTTP ${r.status}`;
  try {
    const body = await r.text();
    if (body) {
      const parsed = JSON.parse(body) as { error?: string };
      if (parsed.error) msg = parsed.error;
    }
  } catch {
    // body wasn't JSON; fall through with the HTTP status
  }
  throw new Error(msg);
}
