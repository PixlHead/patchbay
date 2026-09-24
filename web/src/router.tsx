import {
  createHashHistory,
  createRootRoute,
  createRoute,
  createRouter,
  redirect,
} from '@tanstack/react-router';
import App from './App';
import CanvasPage from './pages/CanvasPage';
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

const workflowsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/workflows',
  component: WorkflowsPage,
});

const canvasRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/canvas',
  component: CanvasPage,
});

const routeTree = rootRoute.addChildren([indexRoute, workflowsRoute, canvasRoute]);

// Hash history keeps every page load at "/", which the Go file server serves without a
// fallback to index.html. Browser history needs that fallback in internal/httpapi first.
export const router = createRouter({ routeTree, history: createHashHistory() });

// Registering the router gives Link, useNavigate, and redirect the route paths as types.
declare module '@tanstack/react-router' {
  interface Register {
    router: typeof router;
  }
}
