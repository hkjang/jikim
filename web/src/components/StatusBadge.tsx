import { Badge } from '@mantine/core';
import { statusLabel } from '../lib/format';

export function StatusBadge({ status }: { status?: string }) {
  const normalized = (status || '').toLowerCase();
  const color = normalized === 'active' || normalized === 'enabled' || normalized === 'healthy' || normalized === 'success' || normalized === 'approved'
    ? 'teal' : normalized === 'pending' || normalized === 'warning' ? 'yellow' : normalized === 'failed' || normalized === 'critical' || normalized === 'rejected' ? 'red' : 'gray';
  return <Badge variant="light" color={color} size="md">{statusLabel(status)}</Badge>;
}
