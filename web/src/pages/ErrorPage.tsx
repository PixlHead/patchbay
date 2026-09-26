import { Link } from '@tanstack/react-router';
import type { ErrorComponentProps } from '@tanstack/react-router';
import AppHeader from '../components/AppHeader';
import { wideMainClass } from '../components/classes';
import PageHeading from '../components/PageHeading';
import { errorMessage } from '../format';

// The router renders this in place of a page that threw while rendering, so it must not
// depend on that page's state.
export default function ErrorPage({ error, reset }: ErrorComponentProps) {
  return (
    <div>
      <AppHeader page="workflows" />
      <main className={wideMainClass}>
        <PageHeading
          eyebrow="ERROR"
          title="Something went wrong"
          description={
            <>
              {errorMessage(error)}{' '}
              <button type="button" className="text-accent underline" onClick={reset}>
                Try again
              </button>{' '}
              or <Link to="/workflows">go to workflows</Link>.
            </>
          }
        />
      </main>
    </div>
  );
}
