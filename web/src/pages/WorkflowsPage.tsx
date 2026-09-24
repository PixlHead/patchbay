import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { request } from '../api';
import type { Run, Workflow } from '../api';
import { draftFromWorkflow } from '../canvasDraft';
import type { LocalWorkflow } from '../canvasDraft';
import AppHeader from '../components/AppHeader';
import ExecutionHistory from '../components/ExecutionHistory';
import Sidebar from '../components/Sidebar';
import StepList from '../components/StepList';
import WorkflowCanvas from '../components/WorkflowCanvas';
import { errorMessage } from '../format';
import { useWorkspace } from '../workspace';

export default function WorkflowsPage() {
  const { workflowId, selectWorkflow, localWorkflows, canvasDrafts, setCanvasDrafts } =
    useWorkspace();
  const queryClient = useQueryClient();
  const [runId, setRunId] = useState('');

  // Both lists refresh once per second while this page is mounted. A refresh still in
  // flight is reused, so requests never overlap. Leaving the page stops the polling.
  const workflowsQuery = useQuery({
    queryKey: ['workflows'],
    queryFn: ({ signal }) => request<Workflow[]>('/workflows', { signal }),
    refetchInterval: 1000,
  });
  const runsQuery = useQuery({
    queryKey: ['runs'],
    queryFn: ({ signal }) => request<Run[]>('/runs', { signal }),
    refetchInterval: 1000,
  });

  // Saved running statuses can outlive a server process; Start enforces capacity.
  const startMutation = useMutation({
    mutationFn: (id: string) =>
      request<Run>(`/workflows/${encodeURIComponent(id)}/runs`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
      }),
    onSuccess: (run) => {
      // Show the new run at once; the next poll replaces the list with the server order.
      queryClient.setQueryData<Run[]>(['runs'], (previous = []) => [
        run,
        ...previous.filter((item) => item.id !== run.id),
      ]);
      setRunId(run.id);
    },
  });

  const workflows = workflowsQuery.data ?? [];
  const runs = runsQuery.data ?? [];
  const loading = workflowsQuery.isPending || runsQuery.isPending;
  const connectionFailure = workflowsQuery.error ?? runsQuery.error;
  const connectionError = connectionFailure ? errorMessage(connectionFailure) : '';
  const starting = startMutation.isPending;
  const actionError = startMutation.error ? errorMessage(startMutation.error) : '';

  const availableWorkflows: (Workflow | LocalWorkflow)[] = [...workflows, ...localWorkflows];
  const selected = availableWorkflows.find((w) => w.id === workflowId) ?? availableWorkflows[0];
  const savedWorkflow = selected && 'steps' in selected ? selected : undefined;
  const history = runs.filter((run) => run.workflowId === selected?.id);
  const inspectedRun = history.find((run) => run.id === runId) ?? history[0];

  function chooseWorkflow(id: string) {
    selectWorkflow(id);
    setRunId('');
    startMutation.reset();
  }

  function startRun() {
    if (!savedWorkflow) return;
    startMutation.mutate(savedWorkflow.id);
  }

  return (
    <div className="app-shell">
      <AppHeader page="workflows" loading={loading} disconnected={!!connectionError} />

      <div className="workspace">
        <Sidebar
          workflows={availableWorkflows}
          selectedId={selected?.id}
          canvasDrafts={canvasDrafts}
          onSelect={chooseWorkflow}
        />

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
                    onClick={startRun}
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
                  <StepList workflow={savedWorkflow} />
                  <ExecutionHistory
                    history={history}
                    inspectedRun={inspectedRun}
                    onInspect={setRunId}
                  />
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
