import { useState } from 'react';
import { Outlet, useNavigate } from '@tanstack/react-router';
import type { CanvasDraft, CanvasDrafts, LocalWorkflow } from './canvasDraft';
import { WorkspaceContext } from './workspace';

// The router renders App once at the root and swaps pages inside Outlet, so the state kept
// here survives navigation and API polling. Unmounting the runner stops polling while the
// New workflow page is open.
export default function App() {
  const navigate = useNavigate();
  const [workflowId, setWorkflowId] = useState('');
  const [localWorkflows, setLocalWorkflows] = useState<LocalWorkflow[]>([]);
  const [canvasDrafts, setCanvasDrafts] = useState<CanvasDrafts>({});
  const [newWorkflow, setNewWorkflow] = useState<CanvasDraft>({ name: '', nodes: [] });

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
    void navigate({ to: '/workflows' });
  }

  return (
    <WorkspaceContext
      value={{
        workflowId,
        selectWorkflow: setWorkflowId,
        localWorkflows,
        canvasDrafts,
        setCanvasDrafts,
        newWorkflow,
        setNewWorkflow,
        createDraft,
      }}
    >
      <Outlet />
    </WorkspaceContext>
  );
}
