import type { Workflow } from '../api';
import type { CanvasDrafts, LocalWorkflow } from '../canvasDraft';

type SidebarProps = {
  workflows: (Workflow | LocalWorkflow)[];
  selectedId?: string;
  canvasDrafts: CanvasDrafts;
  onSelect: (id: string) => void;
};

export default function Sidebar({ workflows, selectedId, canvasDrafts, onSelect }: SidebarProps) {
  return (
    <aside className="sidebar" aria-label="Workflow navigation">
      <div className="section-label">
        WORKFLOWS <span>{workflows.length}</span>
      </div>
      <p className="sidebar-description">Small tasks. A clear execution trail.</p>
      <nav aria-label="Workflows">
        {workflows.map((workflow, index) => (
          <button
            key={workflow.id}
            className={`workflow-link ${selectedId === workflow.id ? 'selected' : ''}`}
            aria-current={selectedId === workflow.id ? 'page' : undefined}
            onClick={() => onSelect(workflow.id)}
          >
            <span className="workflow-number">{String(index + 1).padStart(2, '0')}</span>
            <span>
              <strong>{workflow.name}</strong>
              <small>
                {'steps' in workflow
                  ? `${workflow.steps.length} ${workflow.steps.length === 1 ? 'step' : 'steps'} · Manual trigger`
                  : `${canvasDrafts[workflow.id]?.nodes.length ?? 0} nodes · Local draft`}
              </small>
            </span>
          </button>
        ))}
      </nav>
      <div className="sidebar-note">
        <span className="note-icon" aria-hidden="true">
          ◎
        </span>
        <strong>Your first workflow runner</strong>
        <p>Choose an example, run its checks, and explore what happened.</p>
      </div>
      <div className="memory-note">
        Run history
        <br />
        <span>Latest 100 runs · Saved results survive restarts</span>
      </div>
    </aside>
  );
}
