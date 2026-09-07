import { useEffect, useState } from 'react';
import { request } from './api';
import type { Run, StepRun, Workflow } from './api';

const statusLabel: Record<string, string> = {
  running: 'Running',
  succeeded: 'Completed',
  failed: 'Failed',
  canceled: 'Canceled',
  pending: 'Waiting',
  skipped: 'Skipped',
};

export default function App() {
  const [workflows, setWorkflows] = useState<Workflow[]>([]);
  const [runs, setRuns] = useState<Run[]>([]);
  const [workflowId, setWorkflowId] = useState('');
  const [runId, setRunId] = useState('');
  const [loading, setLoading] = useState(true);
  const [starting, setStarting] = useState(false);
  const [connectionError, setConnectionError] = useState('');
  const [actionError, setActionError] = useState('');

  useEffect(() => {
    const controller = new AbortController();
    let timer: ReturnType<typeof setTimeout>;
    // A recursive timeout avoids overlapping requests. Abort cleans up on unmount.
    async function refresh() {
      try {
        const [definitions, history] = await Promise.all([
          request<Workflow[]>('/workflows', { signal: controller.signal }),
          request<Run[]>('/runs', { signal: controller.signal }),
        ]);
        if (controller.signal.aborted) return;
        setWorkflows(definitions);
        setRuns(history);
        setConnectionError('');
      } catch (error) {
        if (!controller.signal.aborted) setConnectionError(errorMessage(error));
      } finally {
        if (!controller.signal.aborted) {
          setLoading(false);
          timer = setTimeout(refresh, 1000);
        }
      }
    }
    void refresh();
    return () => {
      controller.abort();
      clearTimeout(timer);
    };
  }, []);

  const selected = workflows.find((w) => w.id === workflowId) ?? workflows[0];
  const history = runs.filter((run) => run.workflowId === selected?.id);
  const inspectedRun = history.find((run) => run.id === runId) ?? history[0];
  const busy = runs.some((run) => run.status === 'running');

  async function startRun() {
    if (!selected) return;
    setStarting(true);
    setActionError('');
    try {
      const run = await request<Run>(`/workflows/${encodeURIComponent(selected.id)}/runs`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
      });
      setRuns((previous) => [run, ...previous.filter((item) => item.id !== run.id)]);
      setRunId(run.id);
    } catch (error) {
      setActionError(errorMessage(error));
    } finally {
      setStarting(false);
    }
  }

  return (
    <div className="app-shell">
      <header className="topbar">
        <div className="brand">
          <span className="brand-mark" aria-hidden="true">
            h<span>f</span>
          </span>
          Patchbay
        </div>
        <div className="topbar-meta">
          <span className="milestone">M0</span>
          <span className={`connection ${connectionError ? 'disconnected' : ''}`}>
            <i />
            {connectionError ? 'Disconnected' : loading ? 'Connecting' : 'Local workspace'}
          </span>
        </div>
      </header>

      <div className="workspace">
        <aside className="sidebar" aria-label="Workflow navigation">
          <div className="section-label">
            WORKFLOWS <span>{workflows.length}</span>
          </div>
          <p className="sidebar-description">Small tasks. A clear execution trail.</p>
          <nav aria-label="Workflows">
            {workflows.map((workflow, index) => (
              <button
                key={workflow.id}
                className={`workflow-link ${selected?.id === workflow.id ? 'selected' : ''}`}
                aria-current={selected?.id === workflow.id ? 'page' : undefined}
                onClick={() => {
                  setWorkflowId(workflow.id);
                  setRunId('');
                  setActionError('');
                }}
              >
                <span className="workflow-number">{String(index + 1).padStart(2, '0')}</span>
                <span>
                  <strong>{workflow.name}</strong>
                  <small>
                    {workflow.steps.length} {workflow.steps.length === 1 ? 'step' : 'steps'} ·
                    Manual trigger
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
            Session history
            <br />
            <span>Last 100 runs · Cleared on server restart</span>
          </div>
        </aside>

        <main>
          {connectionError && (
            <div className="error-banner" role="alert">
              {connectionError} Retrying automatically.
            </div>
          )}
          {actionError && (
            <div className="error-banner" role="alert">
              {actionError}
            </div>
          )}
          {loading && (
            <div className="empty-state" role="status">
              Connecting to your workspace…
            </div>
          )}
          {!loading && !selected && !connectionError && (
            <div className="empty-state">No workflows are available.</div>
          )}
          {selected && (
            <>
              <div className="breadcrumb">
                Workspace <span>/</span> Workflows
              </div>
              <div className="page-heading">
                <div>
                  <div className="eyebrow">MANUAL WORKFLOW</div>
                  <h1>{selected.name}</h1>
                  <p>{selected.description}</p>
                </div>
                <button
                  className="run-button"
                  disabled={starting || busy || !!connectionError}
                  onClick={() => void startRun()}
                >
                  <span aria-hidden="true">▶</span>
                  {starting ? 'Starting…' : busy ? 'Workflow running…' : 'Run workflow'}
                </button>
              </div>

              <section className="panel workflow-panel" aria-labelledby="steps-title">
                <div className="panel-heading">
                  <h2 id="steps-title">Workflow steps</h2>
                  <span className="subtle">Execute in order</span>
                </div>
                <ol className="step-list">
                  {selected.steps.map((step, index) => (
                    <li key={step.id}>
                      <span className="step-index">{index + 1}</span>
                      <div className="step-definition">
                        <div className="step-title">
                          <h3>{step.name}</h3>
                          <span className="type-label">HTTP CHECK</span>
                        </div>
                        <code className="endpoint">GET {step.config.url}</code>
                        <div className="step-settings">
                          <span>
                            Expected <b>HTTP {step.config.expectedStatus}</b>
                          </span>
                          <span>
                            Timeout <b>{step.config.timeoutMs.toLocaleString()} ms</b>
                          </span>
                        </div>
                      </div>
                    </li>
                  ))}
                </ol>
                <details className="definition">
                  <summary>View workflow JSON</summary>
                  <pre>{JSON.stringify(selected, null, 2)}</pre>
                </details>
              </section>

              <div className="results-heading">
                <h2>Execution history</h2>
                <span className="subtle">
                  {history.length} {history.length === 1 ? 'run' : 'runs'} this session
                </span>
              </div>
              {history.length === 0 ? (
                <div className="panel empty-state">
                  <div className="empty-symbol" aria-hidden="true">
                    ↳
                  </div>
                  <h3>Ready for its first run</h3>
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
                        onClick={() => setRunId(run.id)}
                      >
                        <div>
                          <Status status={run.status} />
                          <time dateTime={run.startedAt}>
                            {new Date(run.startedAt).toLocaleTimeString([], {
                              hour: '2-digit',
                              minute: '2-digit',
                              second: '2-digit',
                            })}
                          </time>
                        </div>
                        <small>
                          Run {run.id.slice(0, 8)} <span>→</span>
                        </small>
                      </button>
                    ))}
                  </div>
                  {inspectedRun && <RunDetails run={inspectedRun} />}
                </div>
              )}
            </>
          )}
          <footer>Built for your homelab. Runs on your machine.</footer>
        </main>
      </div>
    </div>
  );
}

function RunDetails({ run }: { run: Run }) {
  const unhealthy = run.steps.filter((step) => step.output && !step.output.healthy).length;
  return (
    <section className="panel run-details" aria-label="Run results" aria-live="polite">
      <div className="panel-heading">
        <h3>Run results</h3>
        <Status status={run.status} />
      </div>
      <div className="run-summary">
        <span>
          {run.finishedAt
            ? `${Math.max(0, new Date(run.finishedAt).getTime() - new Date(run.startedAt).getTime())} ms total`
            : 'Checks are in progress…'}
        </span>
        <span>
          {unhealthy > 0
            ? `${unhealthy} unhealthy ${unhealthy === 1 ? 'service' : 'services'}`
            : run.status === 'succeeded'
              ? 'All services healthy'
              : ''}
        </span>
      </div>
      {run.steps.map((step) => (
        <StepResult key={step.id} step={step} />
      ))}
      <p className="result-help">
        Completed means the checks finished. Each service has its own health result.
      </p>
      <details className="definition">
        <summary>View execution JSON</summary>
        <pre>{JSON.stringify(run, null, 2)}</pre>
      </details>
    </section>
  );
}

function StepResult({ step }: { step: StepRun }) {
  return (
    <article className="step-result">
      <div className="step-result-heading">
        <h4>{step.name}</h4>
        {step.output ? (
          <span className={`badge ${step.output.healthy ? 'healthy' : 'unhealthy'}`}>
            {step.output.healthy ? 'Healthy' : 'Unhealthy'}
          </span>
        ) : (
          <Status status={step.status} />
        )}
      </div>
      {step.output && (
        <>
          <p>{step.output.reason}</p>
          <div className="result-metrics">
            <span>
              Response{' '}
              <strong>
                {step.output.statusCode ? `HTTP ${step.output.statusCode}` : 'No response'}
              </strong>
            </span>
            <span>
              Time to headers <strong>{step.output.durationMs} ms</strong>
            </span>
          </div>
        </>
      )}
      {step.error && <p className="step-error">{step.error}</p>}
    </article>
  );
}

function Status({ status }: { status: string }) {
  return <span className={`badge status-${status}`}>{statusLabel[status] ?? status}</span>;
}

function errorMessage(error: unknown) {
  return error instanceof Error ? error.message : 'Something went wrong. Please try again.';
}
