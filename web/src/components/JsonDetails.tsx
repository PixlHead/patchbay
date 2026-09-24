// The browser tests read one <pre> per panel, so each panel renders exactly one JsonDetails.
export default function JsonDetails({ summary, value }: { summary: string; value: unknown }) {
  return (
    <details className="border-t border-border-muted bg-surface-muted text-2xs">
      <summary className="cursor-pointer px-4 py-3.5 text-text-muted md:px-5.5">{summary}</summary>
      <pre className="max-h-82.5 overflow-auto px-4 pt-2 pb-5.5 text-xs leading-[1.7] wrap-anywhere whitespace-pre-wrap text-text-secondary md:px-5.5">
        {JSON.stringify(value, null, 2)}
      </pre>
    </details>
  );
}
