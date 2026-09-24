import type { ReactNode } from 'react';

type ErrorBannerProps = {
  children: ReactNode;
  // Inside a panel the banner keeps the panel's horizontal inset.
  inset?: boolean;
};

// The only component that renders role="alert"; the browser tests count alerts.
export default function ErrorBanner({ children, inset = false }: ErrorBannerProps) {
  return (
    <div
      role="alert"
      className={`rounded-[7px] border border-danger-border bg-danger-bg px-4.5 py-3.5 text-xs leading-[1.6] text-danger ${
        inset ? 'mx-4 mb-4 md:mx-5.5' : 'mb-5'
      }`}
    >
      {children}
    </div>
  );
}
