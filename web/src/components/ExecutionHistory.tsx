import type { Run } from '../api';
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
      <div className="results-heading">
        <h2>Execution history</h2>
        <span className="subtle">
          {history.length} {history.length === 1 ? 'run' : 'runs'} shown
        </span>
      </div>
      {history.length === 0 ? (
        <div className="panel empty-state">
          <div className="empty-symbol" aria-hidden="true">
            ↳
          </div>
          <h3>No recent runs</h3>
          <p>
            Run this workflow to see each check’s status,
            <br className="desktop-break" /> response time, and result.
          </p>
        </div>
      ) : (
        <div className="execution-layout">
          <div className="panel history-list" aria-label="Past runs">
            {history.map((run) => (
              <button
                key={run.id}
                className={`history-item ${inspectedRun?.id === run.id ? 'active' : ''}`}
                aria-pressed={inspectedRun?.id === run.id}
                onClick={() => onInspect(run.id)}
              >
                <div>
                  <StatusBadge status={run.status} />
                  <time dateTime={run.createdAt}>
                    {new Date(run.createdAt).toLocaleString([], {
                      dateStyle: 'short',
                      timeStyle: 'medium',
                    })}
                  </time>
                </div>
                <small>
                  Run {run.id.slice(0, 8)} <span>→</span>
                </small>
                {run.finalSaveFailed && (
                  <span className="save-warning-label">Final result not saved</span>
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
