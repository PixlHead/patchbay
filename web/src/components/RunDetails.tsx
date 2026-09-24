import type { CheckOutput, Run, StepRun } from '../api';
import { hasTcpStep, outputPresentation, runHelp, runSummary, runTiming } from '../format';
import StatusBadge from './StatusBadge';

export default function RunDetails({ run }: { run: Run }) {
  return (
    <section className="panel run-details" aria-label="Run results" aria-live="polite">
      <div className="panel-heading">
        <h3>Run results</h3>
        <StatusBadge status={run.status} />
      </div>
      <div className="run-summary">
        <span>{runTiming(run)}</span>
        <span>{runSummary(run)}</span>
      </div>
      {run.error && (
        <p className="error-banner run-error" role="alert">
          {run.error}
        </p>
      )}
      {run.finalSaveFailed && (
        <p className="error-banner save-warning" role="alert">
          Execution finished, but its final result could not be saved. This result is temporary and
          may be lost when Patchbay restarts or clears older runs from memory.
        </p>
      )}
      {run.steps.map((step) => (
        <StepResult key={step.id} step={step} />
      ))}
      <p className="result-help">{runHelp(run)}</p>
      {hasTcpStep(run) && (
        <p className="result-help">
          A TCP connection confirms the port accepts connections; it does not verify application
          health.
        </p>
      )}
      <details className="definition">
        <summary>View execution JSON</summary>
        <pre>{JSON.stringify(run, null, 2)}</pre>
      </details>
    </section>
  );
}

function StepResult({ step }: { step: StepRun }) {
  return (
    <article className="step-result">
      <div className="step-result-heading">
        <h4>{step.name}</h4>
        {step.output ? (
          <span className={`badge ${step.output.healthy ? 'healthy' : 'unhealthy'}`}>
            {outputPresentation(step.output).badge}
          </span>
        ) : (
          <StatusBadge status={step.status} />
        )}
      </div>
      {step.output && <OutputMetrics output={step.output} />}
      {step.error && <p className="step-error">{step.error}</p>}
    </article>
  );
}

function OutputMetrics({ output }: { output: CheckOutput }) {
  const view = outputPresentation(output);
  return (
    <>
      <p>{output.reason}</p>
      {view.endpoint && <code className="endpoint">{view.endpoint}</code>}
      <div className="result-metrics">
        <span>
          {view.metricLabel} <strong>{view.metricValue}</strong>
        </span>
        <span>
          {view.timeLabel} <strong>{output.durationMs} ms</strong>
        </span>
      </div>
    </>
  );
}
