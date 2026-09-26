import type { CheckOutput, Run, Workflow, WorkflowStep } from './api';

const statusLabels: Record<string, string> = {
  queued: 'Queued',
  running: 'Running',
  succeeded: 'Completed',
  failed: 'Failed',
  canceled: 'Canceled',
  interrupted: 'Interrupted',
  pending: 'Waiting',
  skipped: 'Skipped',
};

// An unknown status renders as its raw value, so a newer server never hides a state.
export function statusLabel(status: string): string {
  return statusLabels[status] ?? status;
}

export function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : 'Something went wrong. Please try again.';
}

export function triggerLabel(workflow: Workflow): string {
  return workflow.schedule ? 'Scheduled' : 'Manual trigger';
}

// The next run shows in the browser's zone, named so it cannot be confused
// with the schedule's own zone.
export function scheduleSummary(workflow: Workflow): string {
  if (!workflow.schedule) return '';
  const summary = `Cron ${workflow.schedule.cron} (${workflow.schedule.timezone})`;
  if (!workflow.nextRunAt) return summary;
  const next = new Date(workflow.nextRunAt).toLocaleString(undefined, { timeZoneName: 'short' });
  return `${summary} · Next run ${next}`;
}

export function tcpEndpoint(host: string, port: number): string {
  // Configuration stores IPv6 without brackets; add them around the host for display.
  return `${host.includes(':') ? `[${host}]` : host}:${port}`;
}

export type StepDefinition = {
  typeLabel: string;
  endpoint: string;
  expected: string;
};

export function stepDefinition(step: WorkflowStep): StepDefinition {
  if (step.type === 'tcp.check') {
    return {
      typeLabel: 'TCP CHECK',
      endpoint: tcpEndpoint(step.config.host, step.config.port),
      expected: 'TCP connection',
    };
  }
  return {
    typeLabel: 'HTTP CHECK',
    endpoint: `GET ${step.config.url}`,
    expected: `HTTP ${step.config.expectedStatus}`,
  };
}

export type OutputPresentation = {
  badge: string;
  endpoint?: string;
  metricLabel: string;
  metricValue: string;
  timeLabel: string;
};

// A result saved before TCP support has no type field and is an HTTP result.
export function outputPresentation(output: CheckOutput): OutputPresentation {
  if (output.type === 'tcp.check') {
    return {
      badge: output.healthy ? 'Connected' : 'Unreachable',
      endpoint: tcpEndpoint(output.host, output.port),
      metricLabel: 'Connection',
      metricValue: output.healthy ? 'Accepted' : 'Not established',
      timeLabel: 'Time to connect',
    };
  }
  return {
    badge: output.healthy ? 'Healthy' : 'Unhealthy',
    metricLabel: 'Response',
    metricValue: output.statusCode ? `HTTP ${output.statusCode}` : 'No response',
    timeLabel: 'Time to headers',
  };
}

export function hasTcpStep(run: Run): boolean {
  return run.steps.some((step) => step.output?.type === 'tcp.check');
}

export function runTiming(run: Run): string {
  if (run.status === 'queued') return 'Waiting for an execution slot';
  if (!run.startedAt) return 'Never started';
  if (run.finishedAt) {
    const elapsed = new Date(run.finishedAt).getTime() - new Date(run.startedAt).getTime();
    return `${Math.max(0, elapsed)} ms total`;
  }
  if (run.status === 'interrupted') return 'Finish time unknown';
  return 'No completion recorded yet';
}

export function runSummary(run: Run): string {
  const unhealthy = run.steps.filter((step) => step.output && !step.output.healthy).length;
  const tcp = hasTcpStep(run);
  if (unhealthy > 0) {
    return tcp
      ? `${unhealthy} ${unhealthy === 1 ? 'check did' : 'checks did'} not pass`
      : `${unhealthy} unhealthy ${unhealthy === 1 ? 'service' : 'services'}`;
  }
  if (run.status === 'succeeded') return tcp ? 'All checks passed' : 'All services healthy';
  return '';
}

export function runHelp(run: Run): string {
  if (run.status === 'queued') {
    return 'This run will start automatically when an execution slot is available.';
  }
  if (run.status === 'interrupted') {
    return 'Patchbay restarted before this run’s completion was recorded. Saved results are preserved; steps were not resumed.';
  }
  return hasTcpStep(run)
    ? 'Completed means the checks finished. Each check has its own result.'
    : 'Completed means the checks finished. Each service has its own health result.';
}
