import { useNavigate, useSearch } from '@tanstack/react-router';
import AppHeader from '../components/AppHeader';
import Breadcrumb from '../components/Breadcrumb';
import {
  fieldLabelClass,
  inputClass,
  pageShellClass,
  wideColumnClass,
} from '../components/classes';
import PageHeading from '../components/PageHeading';
import ScrollColumn from '../components/ScrollColumn';
import ViewTabs, { viewPanelId, viewTabId } from '../components/ViewTabs';
import type { WorkflowView } from '../components/ViewTabs';
import WorkflowCanvas from '../components/WorkflowCanvas';
import { useWorkspace } from '../workspace';

export default function CanvasPage() {
  const { newWorkflow: draft, setNewWorkflow: onChange, createDraft } = useWorkspace();
  const { view } = useSearch({ from: '/canvas' });
  const navigate = useNavigate();

  function showView(next: WorkflowView) {
    void navigate({ to: '/canvas', search: { view: next }, replace: true });
  }

  return (
    <div className={pageShellClass}>
      <AppHeader page="canvas" />
      <ScrollColumn>
        <ViewTabs view={view} onChange={showView} />
        <div
          role="tabpanel"
          id={viewPanelId}
          aria-labelledby={viewTabId(view)}
          className="flex min-h-0 flex-1 flex-col"
        >
          {view === 'canvas' ? (
            <WorkflowCanvas draft={draft} onChange={onChange} />
          ) : (
            <div className={wideColumnClass}>
              <Breadcrumb page="New workflow" />
              <PageHeading
                eyebrow="VISUAL WORKFLOWS"
                title="New workflow"
                description="Name the workflow and create its draft. Arrange its nodes in the Canvas view."
              />
              <form
                className="mb-6 flex flex-wrap items-end gap-4"
                onSubmit={(event) => {
                  event.preventDefault();
                  if (draft.name.trim()) createDraft();
                }}
              >
                <label
                  className={`${fieldLabelClass} min-w-[200px] flex-1 basis-full md:basis-auto`}
                >
                  Workflow name
                  <input
                    className={inputClass}
                    name="workflowName"
                    value={draft.name}
                    onChange={(event) => {
                      const name = event.target.value;
                      onChange((current) => ({ ...current, name }));
                    }}
                    placeholder="e.g. Nightly server checks"
                    maxLength={120}
                    required
                  />
                </label>
                <button
                  type="submit"
                  className="rounded-[7px] border border-accent bg-accent px-5 py-2.75 text-sm font-medium text-white enabled:hover:bg-accent-hover disabled:cursor-not-allowed"
                  disabled={!draft.name.trim()}
                >
                  Create draft
                </button>
              </form>
              <p className="text-xs leading-[1.6] text-text-muted">
                Create draft adds this workflow to your sidebar for this tab session. Drafts and
                canvas edits are cleared when you reload.
              </p>
            </div>
          )}
        </div>
      </ScrollColumn>
    </div>
  );
}
