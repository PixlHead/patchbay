import { Link } from '@tanstack/react-router';
import AppHeader from '../components/AppHeader';

export default function NotFoundPage() {
  return (
    <div className="app-shell">
      <AppHeader page="workflows" />
      <main className="canvas-page">
        <div className="canvas-heading">
          <div>
            <div className="eyebrow">NOT FOUND</div>
            <h1>Page not found</h1>
            <p>
              This address does not match a page. <Link to="/workflows">Go to workflows</Link>.
            </p>
          </div>
        </div>
      </main>
    </div>
  );
}
