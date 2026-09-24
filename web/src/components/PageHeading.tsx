import type { ReactNode } from 'react';

type PageHeadingProps = {
  eyebrow: string;
  title: string;
  description?: ReactNode;
  action?: ReactNode;
};

export default function PageHeading({ eyebrow, title, description, action }: PageHeadingProps) {
  return (
    <div className="mb-5.5 flex flex-col items-start gap-4.5 md:mb-7.5 lg:flex-row lg:items-center lg:justify-between lg:gap-6">
      <div>
        <div className="mb-2.5 text-2xs font-semibold tracking-[1.8px] text-text-muted">
          {eyebrow}
        </div>
        <h1 className="text-2xl lg:text-3xl">{title}</h1>
        {description && (
          <p className="mt-3 max-w-[540px] text-xs leading-[1.7] text-text-muted">{description}</p>
        )}
      </div>
      {action}
    </div>
  );
}
