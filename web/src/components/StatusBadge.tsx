import { statusLabel } from '../format';
import Badge, { badgeTone } from './Badge';

export default function StatusBadge({ status }: { status: string }) {
  return <Badge tone={badgeTone(status)}>{statusLabel(status)}</Badge>;
}
