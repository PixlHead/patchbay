import { useEffect, useState } from 'react';
import type { Dispatch, SetStateAction } from 'react';
import AppHeader from './AppHeader';
import CanvasPage from './CanvasPage';
import WorkflowCanvas from './WorkflowCanvas';
import { draftFromWorkflow } from './canvasDraft';
import type { CanvasDraft, CanvasDrafts, LocalWorkflow } from './canvasDraft';
import { request } from './api';
import type { Run, StepRun, Workflow } from './api';

const statusLabel: Record<string, string> = {
  queued: 'Queued',
  running: 'Running',
  succeeded: 'Completed',
  failed: 'Failed',
  canceled: 'Canceled',
  interrupted: 'Interrupted',
  pending: 'Waiting',
  skipped: 'Skipped',
};

export default function App() {
  // Hash navigation works with the existing Go file server without backend routes.
  const [page, setPage] = useState(() => window.location.hash);
  const [workflowId, setWorkflowId] = useState('');
  const [localWorkflows, setLocalWorkflows] = useState<LocalWorkflow[]>([]);
  const [canvasDrafts, setCanvasDrafts] = useState<CanvasDrafts>({});
  const [newWorkflow, setNewWorkflow] = useState<CanvasDraft>({ name: '', nodes: [] });

  useEffect(() => {
    const navigate = () => setPage(window.location.hash);
    window.addEventListener('hashchange', navigate);
    return () => window.removeEventListener('hashchange', navigate);
  }, []);

  function createDraft() {
    const name = newWorkflow.name.trim();
    if (!name) return;
    const id = `local-${crypto.randomUUID()}`;
    setLocalWorkflows((current) => [
      ...current,
      {
        local: true,
        id,
        name,
        description: 'A local workflow draft. Its nodes do not run actions yet.',
      },
    ]);
    setCanvasDrafts((current) => ({ ...current, [id]: { ...newWorkflow, name } }));
    setWorkflowId(id);
    setNewWorkflow({ name: '', nodes: [] });
    window.location.hash = '#/workflows';
  }

  // Canvas state lives above both pages, so navigation and API polling cannot reset it.
  // Unmounting the runner stops polling while the New workflow page is open.
  return page === '#/canvas' ? (
    <CanvasPage draft={newWorkflow} onChange={setNewWorkflow} onCreate={createDraft} />
  ) : (
    <WorkflowPage
      localWorkflows={localWorkflows}
      canvasDrafts={canvasDrafts}
      setCanvasDrafts={setCanvasDrafts}
      workflowId={workflowId}
      onSelectWorkflow={setWorkflowId}
    />
  );
}

type WorkflowPageProps = {
  localWorkflows: LocalWorkflow[];
  canvasDrafts: CanvasDrafts;
  setCanvasDrafts: Dispatch<SetStateAction<CanvasDrafts>>;
  workflowId: string;
  onSelectWorkflow: (id: string) => void;
};

function WorkflowPage({
  localWorkflows,
  canvasDrafts,
  setCanvasDrafts,
  workflowId,
  onSelectWorkflow,
}: WorkflowPageProps) {
  const [workflows, setWorkflows] = useState<Workflow[]>([]);
  const [runs, setRuns] = useState<Run[]>([]);
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

  const availableWorkflows: (Workflow | LocalWorkflow)[] = [...workflows, ...localWorkflows];
  const selected = availableWorkflows.find((w) => w.id === workflowId) ?? availableWorkflows[0];
  const savedWorkflow = selected && 'steps' in selected ? selected : undefined;
  const history = runs.filter((run) => run.workflowId === selected?.id);
  const inspectedRun = history.find((run) => run.id === runId) ?? history[0];

  // Saved running statuses can outlive a server process; Start enforces capacity.
  async function startRun() {
    if (!savedWorkflow) return;
    setStarting(true);
    setActionError('');
    try {
      const run = await request<Run>(`/workflows/${encodeURIComponent(savedWorkflow.id)}/runs`, {
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
      <AppHeader page="workflows" loading={loading} disconnected={!!connectionError} />

      <div className="workspace">
        <aside className="sidebar" aria-label="Workflow navigation">
          <div className="section-label">
            WORKFLOWS <span>{availableWorkflows.length}</span>
          </div>
          <p className="sidebar-description">Small tasks. A clear execution trail.</p>
          <nav aria-label="Workflows">
            {availableWorkflows.map((workflow, index) => (
              <button
                key={workflow.id}
                className={`workflow-link ${selected?.id === workflow.id ? 'selected' : ''}`}
                aria-current={selected?.id === workflow.id ? 'page' : undefined}
                onClick={() => {
                  onSelectWorkflow(workflow.id);
                  setRunId('');
                  setActionError('');
                }}
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
                  <div className="eyebrow">{savedWorkflow ? 'MANUAL WORKFLOW' : 'LOCAL DRAFT'}</div>
                  <h1>{selected.name}</h1>
                  <p>{selected.description}</p>
                </div>
                {savedWorkflow && (
                  <button
                    className="run-button"
                    disabled={starting || !!connectionError}
                    onClick={() => void startRun()}
                  >
                    <span aria-hidden="true">▶</span>
                    {starting ? 'Starting…' : 'Run workflow'}
                  </button>
                )}
              </div>

              <WorkflowCanvas
                key={selected.id}
                draft={canvasDrafts[selected.id] ?? draftFromWorkflow(selected)}
                onChange={(update) =>
                  setCanvasDrafts((current) => ({
                    ...current,
                    [selected.id]: update(current[selected.id] ?? draftFromWorkflow(selected)),
                  }))
                }
                hasSavedWorkflow={!!savedWorkflow}
              />

              {savedWorkflow && (
                <>
                  <section className="panel workflow-panel" aria-labelledby="steps-title">
                    <div className="panel-heading">
                      <h2 id="steps-title">Saved workflow steps</h2>
                      <span className="subtle">Execute in order</span>
                    </div>
                    <ol className="step-list">
                      {savedWorkflow.steps.map((step, index) => (
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
                      <pre>{JSON.stringify(savedWorkflow, null, 2)}</pre>
                    </details>
                  </section>

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
                            onClick={() => setRunId(run.id)}
                          >
                            <div>
                              <Status status={run.status} />
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
  let timing = 'No completion recorded yet';
  if (run.status === 'queued') timing = 'Waiting for an execution slot';
  else if (!run.startedAt) timing = 'Never started';
  else if (run.finishedAt) {
    timing = `${Math.max(0, new Date(run.finishedAt).getTime() - new Date(run.startedAt).getTime())} ms total`;
  } else if (run.status === 'interrupted') timing = 'Finish time unknown';
  return (
    <section className="panel run-details" aria-label="Run results" aria-live="polite">
      <div className="panel-heading">
        <h3>Run results</h3>
        <Status status={run.status} />
      </div>
      <div className="run-summary">
        <span>{timing}</span>
        <span>
          {unhealthy > 0
            ? `${unhealthy} unhealthy ${unhealthy === 1 ? 'service' : 'services'}`
            : run.status === 'succeeded'
              ? 'All services healthy'
              : ''}
        </span>
      </div>
      {run.error && (
        <p className="error-banner run-error" role="alert">
          {run.error}
        </p>
      )}
      {run.finalSaveFailed && (
        <p className="error-banner save-warning" role="alert">
          Execution finished, but its final result could not be saved. This result is temporary and
          may be lost when Patchbay restarts or clears older runs from memory.
        </p>
      )}
      {run.steps.map((step) => (
        <StepResult key={step.id} step={step} />
      ))}
      <p className="result-help">
        {run.status === 'queued'
          ? 'This run will start automatically when an execution slot is available.'
          : run.status === 'interrupted'
            ? 'Patchbay restarted before this run’s completion was recorded. Saved results are preserved; steps were not resumed.'
            : 'Completed means the checks finished. Each service has its own health result.'}
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
