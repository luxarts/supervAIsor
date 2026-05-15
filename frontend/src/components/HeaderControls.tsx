import type { SortKey } from "../lib/sort";

interface Props {
  hideStale: boolean;
  onToggleHideStale: () => void;
  query: string;
  onQueryChange: (q: string) => void;
  sort: SortKey;
  onSortChange: (key: SortKey) => void;
}

const SORT_LABEL: Record<SortKey, string> = {
  last_update: "LAST UPDATE",
  name: "NAME",
  host: "HOST",
  status: "STATUS",
};

const SORT_OPTIONS: SortKey[] = ["last_update", "name", "host", "status"];

export function HeaderControls({
  hideStale,
  onToggleHideStale,
  query,
  onQueryChange,
  sort,
  onSortChange,
}: Props) {
  return (
    <div className="mt-3 flex flex-col gap-2 sm:flex-row sm:items-center">
      <div className="relative flex-1">
        <input
          type="search"
          inputMode="search"
          value={query}
          onChange={(e) => onQueryChange(e.target.value)}
          placeholder="SEARCH SESSIONS…"
          className="h-12 w-full bg-bg-panel pl-3 pr-12 font-hud text-base text-txt
                     border border-cy/30 focus:border-cy focus:outline-none
                     placeholder:text-dim"
        />
        {query.length > 0 && (
          <button
            type="button"
            aria-label="Clear search"
            onClick={() => onQueryChange("")}
            className="absolute right-0 top-0 grid h-12 w-11 place-items-center
                       font-hud text-lg text-dim hover:text-cy touch-manipulation"
          >
            ×
          </button>
        )}
      </div>

      <label className="flex items-center gap-2 font-hud text-xs uppercase tracking-widest text-dim">
        <span className="hidden sm:inline">SORT</span>
        <select
          aria-label="Sort sessions"
          value={sort}
          onChange={(e) => onSortChange(e.target.value as SortKey)}
          className="h-11 bg-bg-panel border border-cy/30 px-2 font-hud text-xs uppercase
                     tracking-widest text-cy focus:border-cy focus:outline-none touch-manipulation"
        >
          {SORT_OPTIONS.map((k) => (
            <option key={k} value={k}>
              {SORT_LABEL[k]}
            </option>
          ))}
        </select>
      </label>

      <button
        type="button"
        onClick={onToggleHideStale}
        aria-pressed={hideStale}
        className={`h-11 px-4 font-hud text-xs uppercase tracking-widest border
                    touch-manipulation
                    ${
                      hideStale
                        ? "border-cy text-cy bg-cy/10"
                        : "border-cy/30 text-dim hover:border-cy/60"
                    }`}
      >
        {hideStale ? "HIDE STALE" : "SHOW ALL"}
      </button>
    </div>
  );
}
