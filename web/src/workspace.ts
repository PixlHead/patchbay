import { createContext, useContext } from 'react';
import type { Dispatch, SetStateAction } from 'react';
import type { CanvasDraft, CanvasDrafts, LocalWorkflow } from './canvasDraft';

export type Workspace = {
  workflowId: string;
  selectWorkflow: (id: string) => void;
  localWorkflows: LocalWorkflow[];
  canvasDrafts: CanvasDrafts;
  setCanvasDrafts: Dispatch<SetStateAction<CanvasDrafts>>;
  newWorkflow: CanvasDraft;
  setNewWorkflow: Dispatch<SetStateAction<CanvasDraft>>;
  createDraft: () => void;
};

export const WorkspaceContext = createContext<Workspace | null>(null);

export function useWorkspace(): Workspace {
  const workspace = useContext(WorkspaceContext);
  if (!workspace) throw new Error('useWorkspace needs the App route component above it.');
  return workspace;
}
