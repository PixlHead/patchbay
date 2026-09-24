import type { Workflow } from '../api';
import { stepDefinition } from '../format';

export default function StepList({ workflow }: { workflow: Workflow }) {
  return (
    <section className="panel workflow-panel" aria-labelledby="steps-title">
      <div className="panel-heading">
        <h2 id="steps-title">Saved workflow steps</h2>
        <span className="subtle">Execute in order</span>
      </div>
      <ol className="step-list">
        {workflow.steps.map((step, index) => {
          const definition = stepDefinition(step);
          return (
            <li key={step.id}>
              <span className="step-index">{index + 1}</span>
              <div className="step-definition">
                <div className="step-title">
                  <h3>{step.name}</h3>
                  <span className="type-label">{definition.typeLabel}</span>
                </div>
                <code className="endpoint">{definition.endpoint}</code>
                <div className="step-settings">
                  <span>
                    Expected <b>{definition.expected}</b>
                  </span>
                  <span>
                    Timeout <b>{step.config.timeoutMs.toLocaleString()} ms</b>
                  </span>
                </div>
              </div>
            </li>
          );
        })}
      </ol>
      <details className="definition">
        <summary>View workflow JSON</summary>
        <pre>{JSON.stringify(workflow, null, 2)}</pre>
      </details>
    </section>
  );
}
