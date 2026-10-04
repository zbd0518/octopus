'use client';

import { useState } from 'react';
import { usePoolList } from '@/api/endpoints/pool';
import { PoolList } from './PoolList';
import { PoolDetail } from './PoolDetail';

export function Pool() {
  const [selectedPoolId, setSelectedPoolId] = useState<number | null>(null);
  const { data: pools = [] } = usePoolList();
  const selectedPool = pools.find((pool) => pool.id === selectedPoolId);

  if (selectedPool) {
    return <PoolDetail key={selectedPool.id} pool={selectedPool} onBack={() => setSelectedPoolId(null)} />;
  }
  return <PoolList onSelect={(pool) => setSelectedPoolId(pool.id)} />;
}
