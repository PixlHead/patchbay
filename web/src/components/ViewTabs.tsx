import type { KeyboardEvent } from 'react';

export type WorkflowView = 'canvas' | 'details';

export const viewPanelId = 'workflow-view-panel';

export function viewTabId(view: WorkflowView): string {
  return `workflow-view-tab-${view}`;
}

const views: { view: WorkflowView; label: string }[] = [
  { view: 'canvas', label: 'Canvas' },
  { view: 'details', label: 'Details' },
];

const tabClass =
  'rounded-[7px] px-3.5 py-2 text-xs font-medium text-text-secondary hover:bg-surface-hover aria-selected:bg-surface-selected aria-selected:text-ink-selected';

type ViewTabsProps = {
  view: WorkflowView;
  onChange: (view: WorkflowView) => void;
};

export default function ViewTabs({ view, onChange }: ViewTabsProps) {
  // The ARIA tabs pattern moves focus with the arrow keys and selects the focused tab.
  function onKeyDown(event: KeyboardEvent<HTMLButtonElement>) {
    const sibling =
      event.key === 'ArrowRight'
        ? event.currentTarget.nextElementSibling
        : event.key === 'ArrowLeft'
          ? event.currentTarget.previousElementSibling
          : null;
    if (!(sibling instanceof HTMLButtonElement)) return;
    event.preventDefault();
    sibling.focus();
    sibling.click();
  }

  return (
    <div
      role="tablist"
      aria-label="Workflow views"
      className="flex shrink-0 gap-1 border-b border-border bg-surface px-3 py-2 md:px-5"
    >
      {views.map((item) => (
        <button
          key={item.view}
          type="button"
          role="tab"
          id={viewTabId(item.view)}
          aria-selected={view === item.view}
          aria-controls={viewPanelId}
          tabIndex={view === item.view ? 0 : -1}
          className={tabClass}
          onClick={() => onChange(item.view)}
          onKeyDown={onKeyDown}
        >
          {item.label}
        </button>
      ))}
    </div>
  );
}
