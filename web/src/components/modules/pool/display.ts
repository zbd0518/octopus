export type AccountStateInput = {
  status: string;
  schedulable: boolean;
  type: string;
  token_expires_at: number;
  expires_at: number;
  auto_pause_on_expired: boolean;
  temp_unsched_until: number;
  rate_limit_reset_at: number;
  overload_until: number;
};

export function accountState(account: AccountStateInput, now: number) {
  if (account.type === 'oauth' && account.token_expires_at > 0 && account.token_expires_at <= now + 60) return 'tokenExpired';
  if (account.auto_pause_on_expired && account.expires_at > 0 && account.expires_at <= now) return 'expired';
  if (account.temp_unsched_until > now) return 'temporary';
  if (Math.max(account.rate_limit_reset_at, account.overload_until) > now) return 'cooling';
  if (account.status === 'active' && account.schedulable) return 'active';
  return 'unavailable';
}

export function modelNames(raw: string): string[] {
  return [...new Set(raw.split(',').map((name) => name.trim()).filter(Boolean))];
}

export function parseQuota(raw: string): { used: number; total: number; percent: number } | null {
  try {
    const data: unknown = JSON.parse(raw);
    if (!data || typeof data !== 'object' || Array.isArray(data)) return null;
    const { used, total } = data as { used?: unknown; total?: unknown };
    if (typeof used !== 'number' || !Number.isFinite(used) || used < 0) return null;
    const limit = typeof total === 'number' && Number.isFinite(total) && total > 0 ? total : 0;
    return { used, total: limit, percent: limit ? Math.min(100, (used / limit) * 100) : 0 };
  } catch {
    return null;
  }
}

export function parseAccountImport(raw: string): { valid: boolean; count: number } {
  try {
    const data: unknown = JSON.parse(raw);
    if (!Array.isArray(data) || data.length === 0) return { valid: false, count: 0 };
    const valid = data.every((item: unknown) => item !== null && typeof item === 'object' && !Array.isArray(item));
    return { valid, count: valid ? data.length : 0 };
  } catch {
    return { valid: false, count: 0 };
  }
}

export function selectedAccountIds(ids: Set<number>, accounts: { id: number }[]): number[] {
  return accounts.filter((account) => ids.has(account.id)).map((account) => account.id);
}
