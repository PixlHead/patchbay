// These small API types mirror internal/workflow and internal/engine in Go.
// Keep them explicit until the API is large enough to justify code generation.
export type Workflow = {
  schemaVersion: number;
  id: string;
  name: string;
  description: string;
  steps: {
    id: string;
    name: string;
    type: string;
    config: { url: string; expectedStatus: number; timeoutMs: number };
  }[];
};

export type StepRun = {
  id: string;
  name: string;
  status: 'pending' | 'running' | 'succeeded' | 'failed' | 'canceled' | 'skipped';
  startedAt?: string;
  finishedAt?: string;
  error?: string;
  output?: {
    healthy: boolean;
    url: string;
    expectedStatus: number;
    statusCode?: number;
    durationMs: number;
    reason: string;
  };
};

export type Run = {
  id: string;
  workflowId: string;
  workflowName: string;
  status: 'running' | 'succeeded' | 'failed' | 'canceled';
  startedAt: string;
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
