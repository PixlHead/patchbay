import { Link } from '@tanstack/react-router';
import AppHeader from '../components/AppHeader';
import { wideColumnClass } from '../components/classes';
import PageHeading from '../components/PageHeading';

export default function NotFoundPage() {
  return (
    <div>
      <AppHeader page="workflows" />
      <main className={wideColumnClass}>
        <PageHeading
          eyebrow="NOT FOUND"
          title="Page not found"
          description={
            <>
              This address does not match a page. <Link to="/workflows">Go to workflows</Link>.
            </>
          }
        />
      </main>
    </div>
  );
}
