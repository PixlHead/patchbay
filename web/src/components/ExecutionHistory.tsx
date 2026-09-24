import type { Run } from '../api';
import { panelClass } from './classes';
import RunDetails from './RunDetails';
import StatusBadge from './StatusBadge';

type ExecutionHistoryProps = {
  history: Run[];
  inspectedRun?: Run;
  onInspect: (id: string) => void;
};

export default function ExecutionHistory({
  history,
  inspectedRun,
  onInspect,
}: ExecutionHistoryProps) {
  return (
    <>
      <div className="mt-8 mb-[15px] flex items-center justify-between">
        <h2>Execution history</h2>
        <span className="text-2xs text-text-faint">
          {history.length} {history.length === 1 ? 'run' : 'runs'} shown
        </span>
      </div>
      {history.length === 0 ? (
        <div
          className={`${panelClass} px-5 py-[30px] text-center leading-[1.7] text-text-muted md:p-[38px]`}
        >
          <div
            className="m-auto grid size-[38px] place-items-center rounded-[10px] bg-surface-inset text-[25px] text-text-faint"
            aria-hidden="true"
          >
            ↳
          </div>
          <h3 className="mt-3 font-medium text-text-secondary">No recent runs</h3>
          <p className="mt-2 text-xs leading-[1.7]">
            Run this workflow to see each check’s status,
            <br className="hidden md:inline" /> response time, and result.
          </p>
        </div>
      ) : (
        <div className="grid grid-cols-1 gap-4 lg:grid-cols-[204px_minmax(0,1fr)]">
          <div
            className={`${panelClass} flex overflow-x-auto lg:block lg:max-h-[462px] lg:self-start lg:overflow-x-hidden lg:overflow-y-auto`}
            aria-label="Past runs"
          >
            {history.map((run) => (
              <button
                key={run.id}
                className="block w-[180px] min-w-[180px] shrink-0 border-r border-border-muted bg-surface p-4 text-left hover:bg-surface-hover aria-pressed:bg-surface-active aria-pressed:shadow-[inset_0_-3px_var(--color-accent-bar)] lg:w-full lg:min-w-0 lg:shrink lg:border-r-0 lg:border-b lg:aria-pressed:shadow-[inset_3px_0_var(--color-accent-bar)]"
                aria-pressed={inspectedRun?.id === run.id}
                onClick={() => onInspect(run.id)}
              >
                <div className="flex items-center justify-between gap-[5px]">
                  <StatusBadge status={run.status} />
                  <time dateTime={run.createdAt} className="text-2xs text-text-faint">
                    {new Date(run.createdAt).toLocaleString([], {
                      dateStyle: 'short',
                      timeStyle: 'medium',
                    })}
                  </time>
                </div>
                <small className="mt-2.5 flex justify-between font-mono text-2xs text-text-faint">
                  Run {run.id.slice(0, 8)} <span>→</span>
                </small>
                {run.finalSaveFailed && (
                  <span className="mt-2 inline-block text-2xs text-status-queued">
                    Final result not saved
                  </span>
                )}
              </button>
            ))}
          </div>
          {inspectedRun && <RunDetails run={inspectedRun} />}
        </div>
      )}
    </>
  );
}
