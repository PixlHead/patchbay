import type { ReactNode } from 'react';
import type { Run, StepRun } from '../api';

export type BadgeTone = 'neutral' | 'healthy' | 'unhealthy' | Run['status'] | StepRun['status'];

// Every class string is a complete literal so Tailwind can find it.
const toneClass: Record<BadgeTone, string> = {
  neutral: 'bg-badge-bg text-badge',
  pending: 'bg-badge-bg text-badge',
  skipped: 'bg-badge-bg text-badge',
  healthy: 'bg-healthy-bg text-healthy',
  unhealthy: 'bg-status-queued-bg text-status-queued',
  queued: 'bg-status-queued-bg text-status-queued',
  running: 'bg-status-running-bg text-status-running',
  succeeded: 'bg-status-succeeded-bg text-status-succeeded',
  failed: 'bg-status-failed-bg text-status-failed',
  canceled: 'bg-status-failed-bg text-status-failed',
  interrupted: 'bg-status-failed-bg text-status-failed',
};

export function badgeTone(status: string): BadgeTone {
  return status in toneClass ? (status as BadgeTone) : 'neutral';
}

export default function Badge({ tone, children }: { tone: BadgeTone; children: ReactNode }) {
  return (
    <span
      className={`inline-block rounded-sm px-1.75 py-1 text-2xs leading-tight font-medium whitespace-nowrap ${toneClass[tone]}`}
    >
      {children}
    </span>
  );
}
