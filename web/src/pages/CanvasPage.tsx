import AppHeader from '../components/AppHeader';
import Breadcrumb from '../components/Breadcrumb';
import { fieldLabelClass, inputClass, wideMainClass } from '../components/classes';
import PageHeading from '../components/PageHeading';
import WorkflowCanvas from '../components/WorkflowCanvas';
import { useWorkspace } from '../workspace';

export default function CanvasPage() {
  const { newWorkflow: draft, setNewWorkflow: onChange, createDraft } = useWorkspace();
  return (
    <div>
      <AppHeader page="canvas" />
      <main className={wideMainClass}>
        <Breadcrumb page="New workflow" />
        <PageHeading
          eyebrow="VISUAL WORKFLOWS"
          title="New workflow"
          description="Give your workflow a name, then start arranging its nodes."
        />
        <form
          className="mb-6 flex flex-wrap items-end gap-4"
          onSubmit={(event) => {
            event.preventDefault();
            if (draft.name.trim()) createDraft();
          }}
        >
          <label className={`${fieldLabelClass} min-w-[200px] flex-1 basis-full md:basis-auto`}>
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
        <WorkflowCanvas draft={draft} onChange={onChange} />
        <p className="mt-4 text-xs leading-[1.6] text-text-muted">
          Create draft adds this workflow to your sidebar for this tab session. Drafts and canvas
          edits are cleared when you reload.
        </p>
      </main>
    </div>
  );
}
