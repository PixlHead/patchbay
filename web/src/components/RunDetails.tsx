import type { CheckOutput, Run, StepRun } from '../api';
import { hasTcpStep, outputPresentation, runHelp, runSummary, runTiming } from '../format';
import Badge from './Badge';
import { panelClass, panelHeadingClass } from './classes';
import ErrorBanner from './ErrorBanner';
import JsonDetails from './JsonDetails';
import StatusBadge from './StatusBadge';

const helpClass = 'mx-4 mt-1 mb-[18px] text-2xs leading-[1.65] text-text-faint md:mx-[22px]';

export default function RunDetails({ run }: { run: Run }) {
  return (
    <section className={panelClass} aria-label="Run results" aria-live="polite">
      <div className={panelHeadingClass}>
        <h3>Run results</h3>
        <StatusBadge status={run.status} />
      </div>
      <div className="flex flex-wrap justify-between gap-2.5 px-4 py-[15px] text-2xs text-text-faint md:px-[22px] md:py-4">
        <span>{runTiming(run)}</span>
        <span>{runSummary(run)}</span>
      </div>
      {run.error && <ErrorBanner inset>{run.error}</ErrorBanner>}
      {run.finalSaveFailed && (
        <ErrorBanner inset>
          Execution finished, but its final result could not be saved. This result is temporary and
          may be lost when Patchbay restarts or clears older runs from memory.
        </ErrorBanner>
      )}
      {run.steps.map((step) => (
        <StepResult key={step.id} step={step} />
      ))}
      <p className={helpClass}>{runHelp(run)}</p>
      {hasTcpStep(run) && (
        <p className={helpClass}>
          A TCP connection confirms the port accepts connections; it does not verify application
          health.
        </p>
      )}
      <JsonDetails summary="View execution JSON" value={run} />
    </section>
  );
}

const stepTextClass = 'mt-2.5 text-xs leading-[1.7] wrap-anywhere text-text-muted';

function StepResult({ step }: { step: StepRun }) {
  return (
    <article className="mx-4 mb-[15px] rounded-[7px] border border-border-muted p-[15px] md:mx-[22px]">
      <div className="flex flex-wrap items-center justify-between gap-3 md:flex-nowrap">
        <h4>{step.name}</h4>
        {step.output ? (
          <Badge tone={step.output.healthy ? 'healthy' : 'unhealthy'}>
            {outputPresentation(step.output).badge}
          </Badge>
        ) : (
          <StatusBadge status={step.status} />
        )}
      </div>
      {step.output && <OutputMetrics output={step.output} />}
      {step.error && <p className={stepTextClass}>{step.error}</p>}
    </article>
  );
}

function OutputMetrics({ output }: { output: CheckOutput }) {
  const view = outputPresentation(output);
  return (
    <>
      <p className={stepTextClass}>{output.reason}</p>
      {view.endpoint && (
        <code className="endpoint leading-[1.7] wrap-anywhere text-text-secondary">
          {view.endpoint}
        </code>
      )}
      <div className="mt-3.5 flex flex-wrap gap-6 text-2xs text-text-subtle">
        <span>
          {view.metricLabel}{' '}
          <strong className="mt-[5px] block text-xs font-medium text-text-secondary">
            {view.metricValue}
          </strong>
        </span>
        <span>
          {view.timeLabel}{' '}
          <strong className="mt-[5px] block text-xs font-medium text-text-secondary">
            {output.durationMs} ms
          </strong>
        </span>
      </div>
    </>
  );
}
