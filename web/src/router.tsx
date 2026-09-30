import {
  createHashHistory,
  createRootRoute,
  createRoute,
  createRouter,
  redirect,
} from '@tanstack/react-router';
import type { SearchSchemaInput } from '@tanstack/react-router';
import App from './App';
import type { WorkflowView } from './components/ViewTabs';
import CanvasPage from './pages/CanvasPage';
import ErrorPage from './pages/ErrorPage';
import NotFoundPage from './pages/NotFoundPage';
import WorkflowsPage from './pages/WorkflowsPage';

const rootRoute = createRootRoute({ component: App, notFoundComponent: NotFoundPage });

const indexRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/',
  beforeLoad: () => {
    throw redirect({ to: '/workflows', replace: true });
  },
});

// The open view lives in the URL (?view=details), so a reload keeps it. SearchSchemaInput
// makes `view` optional for links while the validated output is always set.
function validateView(search: { view?: WorkflowView } & SearchSchemaInput): {
  view: WorkflowView;
} {
  return { view: search.view === 'details' ? 'details' : 'canvas' };
}

const workflowsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/workflows',
  validateSearch: validateView,
  component: WorkflowsPage,
});

const canvasRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/canvas',
  validateSearch: validateView,
  component: CanvasPage,
});

const routeTree = rootRoute.addChildren([indexRoute, workflowsRoute, canvasRoute]);

// Hash history keeps every page load at "/", which the Go file server serves without a
// fallback to index.html. Browser history needs that fallback in internal/httpapi first.
export const router = createRouter({
  routeTree,
  history: createHashHistory(),
  defaultErrorComponent: ErrorPage,
});

// Registering the router gives Link, useNavigate, and redirect the route paths as types.
declare module '@tanstack/react-router' {
  interface Register {
    router: typeof router;
  }
}
