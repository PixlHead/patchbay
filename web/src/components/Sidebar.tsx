import type { Workflow } from '../api';
import type { CanvasDrafts, LocalWorkflow } from '../canvasDraft';
import { triggerLabel } from '../format';

type SidebarProps = {
  workflows: (Workflow | LocalWorkflow)[];
  selectedId?: string;
  canvasDrafts: CanvasDrafts;
  onSelect: (id: string) => void;
};

export default function Sidebar({ workflows, selectedId, canvasDrafts, onSelect }: SidebarProps) {
  return (
    <aside
      className="flex flex-col gap-2 border-b border-border bg-surface-muted px-4 pt-5 pb-2.5 md:gap-2.5 md:border-r md:border-b-0 md:px-3 md:pt-8 md:pb-6 lg:px-4.5"
      aria-label="Workflow navigation"
    >
      <div className="mx-0.75 mb-1 flex items-center justify-between text-2xs font-bold tracking-[1.5px] text-text-secondary md:mx-3 md:mb-0">
        WORKFLOWS{' '}
        <span className="hidden rounded-sm bg-surface-chip px-1.25 py-0.5 tracking-normal md:inline">
          {workflows.length}
        </span>
      </div>
      <p className="mx-3 mb-5 hidden text-xs leading-normal text-text-muted md:block md:max-w-40 lg:max-w-none">
        Small tasks. A clear execution trail.
      </p>
      <nav
        className="flex gap-1.5 overflow-x-auto pb-[7px] md:block md:overflow-visible md:pb-0"
        aria-label="Workflows"
      >
        {workflows.map((workflow, index) => (
          <button
            key={workflow.id}
            className="flex w-[200px] min-w-[200px] shrink-0 items-start gap-[11px] rounded-lg border border-transparent bg-transparent p-3 text-left text-text-secondary hover:bg-surface-hover aria-[current=page]:border-border-strong aria-[current=page]:bg-surface-selected aria-[current=page]:text-ink-selected md:mb-1.5 md:w-full md:min-w-0 md:px-3 md:py-[15px]"
            aria-current={selectedId === workflow.id ? 'page' : undefined}
            onClick={() => onSelect(workflow.id)}
          >
            <span className="mt-[3px] font-mono text-2xs text-text-faint">
              {String(index + 1).padStart(2, '0')}
            </span>
            <span>
              <strong className="block text-xs leading-normal font-semibold">
                {workflow.name}
              </strong>
              <small className="mt-[5px] block text-2xs text-text-muted">
                {'steps' in workflow
                  ? `${workflow.steps.length} ${workflow.steps.length === 1 ? 'step' : 'steps'} · ${triggerLabel(workflow)}`
                  : `${canvasDrafts[workflow.id]?.nodes.length ?? 0} nodes · Local draft`}
              </small>
            </span>
          </button>
        ))}
      </nav>
      <div className="mx-2.5 mt-8.75 hidden border-t border-border pt-6.25 md:block">
        <span className="text-[22px] text-text-muted" aria-hidden="true">
          ◎
        </span>
        <strong className="mt-3 block text-xs font-medium">Your first workflow runner</strong>
        <p className="mt-2 text-xs leading-[1.7] text-text-muted">
          Choose an example, run its checks, and explore what happened.
        </p>
      </div>
      <div className="mx-3 mt-9 hidden text-2xs leading-[1.9] text-text-secondary md:block">
        Run history
        <br />
        <span className="text-text-faint">Latest 100 runs · Saved results survive restarts</span>
      </div>
    </aside>
  );
}
