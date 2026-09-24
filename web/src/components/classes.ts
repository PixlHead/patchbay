// Class strings shared by several components. Tailwind reads them from this file.
export const panelClass = 'overflow-hidden rounded-[10px] border border-border bg-surface';

export const panelHeadingClass =
  'flex items-center justify-between gap-3 border-b border-border-muted p-4 md:px-[22px] md:py-[19px]';

export const focusRingClass =
  'focus-visible:outline-3 focus-visible:outline-offset-[3px] focus-visible:outline-focus';

export const fieldLabelClass = 'grid gap-1.75 text-xs font-medium text-text-secondary';

export const inputClass = `min-w-0 rounded-[7px] border border-border-strong bg-surface px-3 py-2.5 text-sm text-ink-strong ${focusRingClass}`;

// The canvas and not-found pages use a wider column than the runner.
export const wideMainClass =
  'mx-auto w-full max-w-[1600px] px-4.5 py-5.5 md:p-7 lg:px-10.5 lg:pt-7.5 lg:pb-5 2xl:pt-10.5';
