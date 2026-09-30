import { useEffect, useRef } from 'react';
import type { ReactNode } from 'react';

// The page shell keeps the document at viewport height, so this column, not the document,
// scrolls the page content. It takes focus on mount; otherwise the arrow and page keys
// scroll nothing until a click lands inside it.
export default function ScrollColumn({ children }: { children: ReactNode }) {
  const column = useRef<HTMLElement>(null);

  useEffect(() => {
    column.current?.focus({ preventScroll: true });
  }, []);

  return (
    <main
      ref={column}
      tabIndex={-1}
      className="flex min-h-0 min-w-0 flex-1 flex-col overflow-y-auto outline-none"
    >
      {children}
    </main>
  );
}
