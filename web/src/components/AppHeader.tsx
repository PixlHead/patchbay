import { Link } from '@tanstack/react-router';
import { focusRingClass } from './classes';

type AppHeaderProps = {
  page: 'workflows' | 'canvas';
  loading?: boolean;
  disconnected?: boolean;
};

const navLinkClass = `rounded-[7px] px-3.5 py-[9px] text-xs font-medium text-text-secondary no-underline hover:bg-surface-hover aria-[current=page]:bg-surface-selected aria-[current=page]:text-ink-selected ${focusRingClass}`;

export default function AppHeader({ page, loading = false, disconnected = false }: AppHeaderProps) {
  return (
    <header className="flex min-h-16 flex-wrap items-center justify-between gap-3 border-b border-border bg-surface px-[18px] py-3.5 md:min-h-[76px] md:gap-6 md:px-[34px]">
      <div className="flex items-center gap-3 text-base font-semibold tracking-[-0.6px] md:text-[19px]">
        <span
          className="relative grid size-[34px] place-content-center rounded-[10px] bg-accent-deep pr-[9px] text-[22px] font-medium text-white"
          aria-hidden="true"
        >
          p<span className="absolute top-2.5 right-1.5 text-[19px] text-accent-glow">b</span>
        </span>
        Patchbay
      </div>
      <nav
        className="order-3 flex w-full items-center gap-1 md:order-none md:mr-auto md:w-auto"
        aria-label="Pages"
      >
        <Link to="/workflows" className={navLinkClass}>
          Workflows
        </Link>
        <Link to="/canvas" className={navLinkClass}>
          New workflow
        </Link>
      </nav>
      <div className="ml-auto flex items-center gap-2.25 text-xs md:ml-0 md:gap-[18px]">
        <span className="hidden rounded-[5px] border border-border bg-surface-muted px-1.5 py-[3px] text-2xs tracking-[1px] md:inline-block">
          M0
        </span>
        {page === 'canvas' ? (
          <span className="text-text-secondary">Local draft</span>
        ) : (
          <span className="flex items-center gap-[7px] text-2xs text-text-secondary md:text-xs">
            <i className={`size-1.5 rounded-full ${disconnected ? 'bg-offline' : 'bg-online'}`} />
            {disconnected ? 'Disconnected' : loading ? 'Connecting' : 'Local workspace'}
          </span>
        )}
      </div>
    </header>
  );
}
