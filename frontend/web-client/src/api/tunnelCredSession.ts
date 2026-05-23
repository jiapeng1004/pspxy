/** 仅存 sessionStorage（关闭标签页失效），按服务端 Origin 分区。单列 api_key。 */

import {
  resolveServerHttpOrigin,
  canonicalWebSocketServerUrl,
} from './server-auth';

export interface TunnelStoredCreds {
  api_key?: string;
}

const PREFIX_V2 = 'pspxy_tunnel_sess:v2';
const PREFIX_V1 = 'pspxy_tunnel_sess:v1';

function candidateStorageKeysV2(serverBase: string): string[] {
  const t = serverBase.trim();
  if (!t) return [];
  const variants = new Set<string>([t, canonicalWebSocketServerUrl(t)]);
  const keys: string[] = [];
  for (const v of variants) {
    const origin = resolveServerHttpOrigin(v);
    if (origin) keys.push(`${PREFIX_V2}:${origin}`);
  }
  return keys;
}

export function loadTunnelCreds(serverBase: string): TunnelStoredCreds | null {
  for (const key of candidateStorageKeysV2(serverBase)) {
    try {
      const raw = sessionStorage.getItem(key);
      if (!raw) continue;
      const o = JSON.parse(raw) as Record<string, unknown>;
      const api = typeof o.api_key === 'string' ? o.api_key : '';
      if (api.trim()) return { api_key: api.trim() };
    } catch {
      /* ignore */
    }
  }
  return null;
}

export function saveTunnelCreds(serverBase: string, creds: TunnelStoredCreds): void {
  clearTunnelCreds(serverBase);
  const canon = canonicalWebSocketServerUrl(serverBase);
  const origin = resolveServerHttpOrigin(canon);
  if (!origin) return;

  const key = `${PREFIX_V2}:${origin}`;
  const api = typeof creds.api_key === 'string' ? creds.api_key.trim() : '';
  if (api) {
    sessionStorage.setItem(key, JSON.stringify({ api_key: api }));
  }
}

export function clearTunnelCreds(serverBase: string): void {
  const keys = new Set<string>();
  for (const k of candidateStorageKeysV2(serverBase)) keys.add(k);
  const t = serverBase.trim();
  if (t) {
    for (const v of [t, canonicalWebSocketServerUrl(t)]) {
      const origin = resolveServerHttpOrigin(v);
      if (origin) {
        keys.add(`${PREFIX_V1}:${origin}`);
        keys.add(`${PREFIX_V2}:${origin}`);
      }
    }
  }
  for (const k of keys) {
    sessionStorage.removeItem(k);
  }
}
