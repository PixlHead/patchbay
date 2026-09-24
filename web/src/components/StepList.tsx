import type { Workflow } from '../api';
import { stepDefinition } from '../format';
import { panelClass, panelHeadingClass } from './classes';
import JsonDetails from './JsonDetails';

export default function StepList({ workflow }: { workflow: Workflow }) {
  return (
    <section className={panelClass} aria-labelledby="steps-title">
      <div className={panelHeadingClass}>
        <h2 id="steps-title">Saved workflow steps</h2>
        <span className="text-2xs text-text-faint">Execute in order</span>
      </div>
      <ol className="divide-y divide-border-muted px-4 py-1 md:px-[22px] md:py-2.5">
        {workflow.steps.map((step, index) => {
          const definition = stepDefinition(step);
          return (
            <li key={step.id} className="relative flex gap-[11px] py-5 md:gap-4">
              <span className="grid size-8 shrink-0 place-items-center rounded-lg border border-border bg-surface-inset font-mono text-xs text-text-secondary">
                {index + 1}
              </span>
              <div className="min-w-0 flex-1">
                <div className="mt-0.5 mb-2.5 flex flex-wrap items-center gap-3">
                  <h3 className="font-medium">{step.name}</h3>
                  <span className="hidden rounded-[3px] border border-border-muted px-[5px] py-[3px] text-2xs tracking-[1px] text-text-muted md:inline">
                    {definition.typeLabel}
                  </span>
                </div>
                <code className="endpoint leading-[1.7] wrap-anywhere text-text-secondary">
                  {definition.endpoint}
                </code>
                <div className="mt-3.5 flex flex-wrap gap-2.5 text-2xs text-text-faint md:gap-[22px]">
                  <span>
                    Expected{' '}
                    <b className="ml-[5px] font-medium text-text-secondary">
                      {definition.expected}
                    </b>
                  </span>
                  <span>
                    Timeout{' '}
                    <b className="ml-[5px] font-medium text-text-secondary">
                      {step.config.timeoutMs.toLocaleString()} ms
                    </b>
                  </span>
                </div>
              </div>
            </li>
          );
        })}
      </ol>
      <JsonDetails summary="View workflow JSON" value={workflow} />
    </section>
  );
}
