export function ScanlineOverlay() {
  return (
    <div
      aria-hidden
      className="pointer-events-none fixed inset-0 z-50 opacity-[0.06]"
      style={{
        background:
          "repeating-linear-gradient(to bottom, transparent 0px, transparent 2px, #00f0ff 2px, #00f0ff 3px)",
      }}
    />
  );
}
