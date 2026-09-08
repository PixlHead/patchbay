import AppHeader from './AppHeader';
import WorkflowCanvas from './WorkflowCanvas';
import type { CanvasDraft, UpdateCanvasDraft } from './canvasDraft';

type CanvasPageProps = {
  draft: CanvasDraft;
  onChange: UpdateCanvasDraft;
  onCreate: () => void;
};

export default function CanvasPage({ draft, onChange, onCreate }: CanvasPageProps) {
  return (
    <div className="app-shell">
      <AppHeader page="canvas" />
      <main className="canvas-page">
        <div className="breadcrumb">
          Workspace <span>/</span> New workflow
        </div>
        <div className="canvas-heading">
          <div>
            <div className="eyebrow">VISUAL WORKFLOWS</div>
            <h1>New workflow</h1>
            <p>Give your workflow a name, then start arranging its nodes.</p>
          </div>
        </div>
        <form
          className="new-workflow-form"
          onSubmit={(event) => {
            event.preventDefault();
            if (draft.name.trim()) onCreate();
          }}
        >
          <label className="workflow-name-field">
            Workflow name
            <input
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
          <button type="submit" className="canvas-create" disabled={!draft.name.trim()}>
            Create draft
          </button>
        </form>
        <WorkflowCanvas draft={draft} onChange={onChange} />
        <p className="canvas-footnote">
          Create draft adds this workflow to your sidebar for this tab session. Drafts and canvas
          edits are cleared when you reload.
        </p>
      </main>
    </div>
  );
}
