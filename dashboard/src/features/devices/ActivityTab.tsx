import { useMemo } from 'react';
import { useDeviceActivity } from '@/api/devices';
import { EmptyState, ErrorState, LoadMore, PageLoader } from '@/components/ui/feedback';
import { AuditTable } from '@/features/audit/AuditTable';

export function ActivityTab({ deviceId }: { deviceId: string }) {
  const q = useDeviceActivity(deviceId);
  const entries = useMemo(() => q.data?.pages.flatMap((p) => p.items) ?? [], [q.data]);

  if (q.isPending) return <PageLoader label="Loading activity…" />;
  if (q.isError) return <ErrorState error={q.error} onRetry={() => void q.refetch()} />;
  if (entries.length === 0) {
    return <EmptyState title="No activity yet" description="Actions on this device will appear here." />;
  }
  return (
    <div className="-mx-4">
      <AuditTable entries={entries} showTarget={false} />
      <LoadMore
        hasNextPage={q.hasNextPage}
        isFetchingNextPage={q.isFetchingNextPage}
        fetchNextPage={q.fetchNextPage}
      />
    </div>
  );
}
