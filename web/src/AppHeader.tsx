type AppHeaderProps = {
  page: 'workflows' | 'canvas';
  loading?: boolean;
  disconnected?: boolean;
};

export default function AppHeader({ page, loading = false, disconnected = false }: AppHeaderProps) {
  return (
    <header className="topbar">
      <div className="brand">
        <span className="brand-mark" aria-hidden="true">
          p<span>b</span>
        </span>
        Patchbay
      </div>
      <nav className="page-nav" aria-label="Pages">
        <a href="#/workflows" aria-current={page === 'workflows' ? 'page' : undefined}>
          Workflows
        </a>
        <a href="#/canvas" aria-current={page === 'canvas' ? 'page' : undefined}>
          New workflow
        </a>
      </nav>
      <div className="topbar-meta">
        <span className="milestone">M0</span>
        {page === 'canvas' ? (
          <span className="canvas-preview-label">Local draft</span>
        ) : (
          <span className={`connection ${disconnected ? 'disconnected' : ''}`}>
            <i />
            {disconnected ? 'Disconnected' : loading ? 'Connecting' : 'Local workspace'}
          </span>
        )}
      </div>
    </header>
  );
}
