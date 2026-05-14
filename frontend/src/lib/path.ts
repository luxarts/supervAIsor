// Returns a short, human-friendly label for a project absolute path.
// - Empty input → "<NONE>".
// - If the basename is "Projects" (e.g. "/Users/x/Projects"), treat as
//   no-project-context and return "<NONE>". Edge case: a project literally
//   named "Projects" would collide; acceptable for v1.
// - Otherwise: basename.
export function shortProject(path: string): string {
  if (!path) return "<NONE>";
  const trimmed = path.replace(/\/+$/, "");
  if (!trimmed) return "<NONE>";
  const idx = trimmed.lastIndexOf("/");
  const base = idx === -1 ? trimmed : trimmed.slice(idx + 1);
  if (!base) return "<NONE>";
  if (base === "Projects") return "<NONE>";
  return base;
}
