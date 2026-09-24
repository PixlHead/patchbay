export default function Breadcrumb({ page }: { page: string }) {
  return (
    <div className="mb-5.5 text-xs text-text-faint md:mb-7.5">
      Workspace <span className="px-2.5 text-text-separator">/</span> {page}
    </div>
  );
}
