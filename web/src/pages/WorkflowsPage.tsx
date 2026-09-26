import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { request } from '../api';
import type { Run, Workflow } from '../api';
import { draftFromWorkflow } from '../canvasDraft';
import type { LocalWorkflow } from '../canvasDraft';
import AppHeader from '../components/AppHeader';
import Breadcrumb from '../components/Breadcrumb';
import ErrorBanner from '../components/ErrorBanner';
import ExecutionHistory from '../components/ExecutionHistory';
import PageHeading from '../components/PageHeading';
import Sidebar from '../components/Sidebar';
import StepList from '../components/StepList';
import WorkflowCanvas from '../components/WorkflowCanvas';
import { errorMessage, scheduleSummary } from '../format';
import { useWorkspace } from '../workspace';

const emptyStateClass = 'px-5 py-[30px] text-center leading-[1.7] text-text-muted md:p-[38px]';

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
  const scheduleLine = savedWorkflow ? scheduleSummary(savedWorkflow) : '';

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
    <div>
      <AppHeader page="workflows" loading={loading} disconnected={!!connectionError} />

      <div className="flex min-h-[calc(100vh-76px)] flex-col md:grid md:grid-cols-[234px_minmax(0,1fr)] lg:grid-cols-[276px_minmax(0,1fr)]">
        <Sidebar
          workflows={availableWorkflows}
          selectedId={selected?.id}
          canvasDrafts={canvasDrafts}
          onSelect={chooseWorkflow}
        />

        <main className="mx-auto w-full max-w-[1320px] self-start px-[18px] py-[22px] md:p-7 lg:px-[42px] lg:pt-[30px] lg:pb-5 2xl:pt-[42px]">
          {connectionError && <ErrorBanner>{connectionError} Retrying automatically.</ErrorBanner>}
          {actionError && <ErrorBanner>{actionError}</ErrorBanner>}
          {loading && (
            <div className={emptyStateClass} role="status">
              Connecting to your workspace…
            </div>
          )}
          {!loading && !selected && !connectionError && (
            <div className={emptyStateClass}>No workflows are available.</div>
          )}
          {selected && (
            <>
              <Breadcrumb page="Workflows" />
              <PageHeading
                eyebrow={
                  savedWorkflow
                    ? savedWorkflow.schedule
                      ? 'SCHEDULED WORKFLOW'
                      : 'MANUAL WORKFLOW'
                    : 'LOCAL DRAFT'
                }
                title={selected.name}
                description={
                  (selected.description || scheduleLine) && (
                    <>
                      {selected.description}
                      {scheduleLine && (
                        <span className="mt-1.5 block font-mono text-2xs text-text-secondary">
                          {scheduleLine}
                        </span>
                      )}
                    </>
                  )
                }
                action={
                  savedWorkflow && (
                    <button
                      className="flex w-full shrink-0 items-center justify-center gap-2.5 rounded-[7px] border border-accent bg-accent px-[18px] py-3 text-xs font-medium text-white shadow-button enabled:hover:bg-accent-hover md:w-auto"
                      disabled={starting || !!connectionError}
                      onClick={startRun}
                    >
                      <span className="text-2xs" aria-hidden="true">
                        ▶
                      </span>
                      {starting ? 'Starting…' : 'Run workflow'}
                    </button>
                  )
                }
              />

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
          <footer className="mt-[30px] text-center text-2xs text-text-subtle">
            Built for your homelab. Runs on your machine.
          </footer>
        </main>
      </div>
    </div>
  );
}
