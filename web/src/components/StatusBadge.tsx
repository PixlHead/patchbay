import { statusLabel } from '../format';

export default function StatusBadge({ status }: { status: string }) {
  return <span className={`badge status-${status}`}>{statusLabel(status)}</span>;
}
