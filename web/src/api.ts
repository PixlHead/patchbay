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

export type WorkflowSchedule = { cron: string; timezone: string };

export type Workflow = {
  schemaVersion: number;
  id: string;
  name: string;
  description: string;
  schedule?: WorkflowSchedule;
  steps: WorkflowStep[];
  // Present only while the server has a next occurrence for the schedule.
  nextRunAt?: string;
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
  const isJson = response.headers.get('content-type')?.includes('application/json') ?? false;
  if (!response.ok) {
    if (isJson) {
      const data = (await response.json()) as { error?: string };
      throw new Error(data.error ?? `Request failed (${response.status})`);
    }
    throw new Error(`Request failed (${response.status}). Check that the Go server is running.`);
  }
  if (!isJson) {
    throw new Error('The backend is unavailable. Check that the Go server is running.');
  }
  return (await response.json()) as T;
}
