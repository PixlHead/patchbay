// These small API types mirror internal/workflow and internal/engine in Go.
// Keep them explicit until the API is large enough to justify code generation.
export type WorkflowStep = { id: string; name: string } & (
  | {
      type: 'http.check';
      config: { url: string; expectedStatus: number; timeoutMs: number };
    }
  | {
      type: 'tcp.check';
      config: { host: string; port: number; timeoutMs: number };
    }
);

export type Workflow = {
  schemaVersion: number;
  id: string;
  name: string;
  description: string;
  steps: WorkflowStep[];
};

export type CheckOutput = {
  healthy: boolean;
  durationMs: number;
  reason: string;
} & (
  | {
      // HTTP history written before TCP support has no type field.
      type?: 'http.check';
      url: string;
      expectedStatus: number;
      statusCode?: number;
    }
  | { type: 'tcp.check'; host: string; port: number }
);

export type StepRun = {
  id: string;
  name: string;
  status: 'pending' | 'running' | 'succeeded' | 'failed' | 'canceled' | 'interrupted' | 'skipped';
  startedAt?: string;
  finishedAt?: string;
  error?: string;
  output?: CheckOutput;
};

export type Run = {
  id: string;
  workflowId: string;
  workflowName: string;
  status: 'queued' | 'running' | 'succeeded' | 'failed' | 'canceled' | 'interrupted';
  error?: string;
  finalSaveFailed?: boolean;
  createdAt: string;
  startedAt?: string;
  finishedAt?: string;
  steps: StepRun[];
};

export async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(`/api${path}`, init);
  if (!response.headers.get('content-type')?.includes('application/json')) {
    throw new Error('The backend is unavailable. Check that the Go server is running.');
  }
  const data = await response.json();
  if (!response.ok) throw new Error(data.error ?? `Request failed (${response.status})`);
  return data as T;
}
