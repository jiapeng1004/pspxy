/** 仅存 sessionStorage（关闭标签页失效），并按服务端 HTTP Origin 分区，不把 AK/SK 写入 localStorage */

import {
  resolveServerHttpOrigin,
  canonicalWebSocketServerUrl,
} from './server-auth';

export interface TunnelStoredCreds {
  access_key: string;
  secret_key: string;
}

const PREFIX = 'pspxy_tunnel_sess:v1';

function candidateStorageKeys(serverBase: string): string[] {
  const t = serverBase.trim();
  if (!t) return [];
  const variants = new Set<string>([t, canonicalWebSocketServerUrl(t)]);
  const keys: string[] = [];
  for (const v of variants) {
    const origin = resolveServerHttpOrigin(v);
    if (origin) keys.push(`${PREFIX}:${origin}`);
  }
  return keys;
}

export function loadTunnelCreds(serverBase: string): TunnelStoredCreds | null {
  for (const key of candidateStorageKeys(serverBase)) {
    try {
      const raw = sessionStorage.getItem(key);
      if (!raw) continue;
      const o = JSON.parse(raw) as Record<string, unknown>;
      const ak = typeof o.access_key === 'string' ? o.access_key : '';
      const sk = typeof o.secret_key === 'string' ? o.secret_key : '';
      if (!ak || !sk) continue;
      return { access_key: ak, secret_key: sk };
    } catch {
      /* try next */
    }
  }
  return null;
}

/** 始终以规范 ws 地址对应的 Origin 保存，并清理该地址的旧键位 */
export function saveTunnelCreds(serverBase: string, accessKey: string, secretKey: string): void {
  clearTunnelCreds(serverBase);
  const canon = canonicalWebSocketServerUrl(serverBase);
  const origin = resolveServerHttpOrigin(canon);
  if (!origin) return;

  const key = `${PREFIX}:${origin}`;
  const ak = accessKey.trim();
  const sk = secretKey.trim();
  if (!ak || !sk) {
    sessionStorage.removeItem(key);
    return;
  }
  sessionStorage.setItem(key, JSON.stringify({ access_key: ak, secret_key: sk }));
}

export function clearTunnelCreds(serverBase: string): void {
  for (const key of candidateStorageKeys(serverBase)) {
    sessionStorage.removeItem(key);
  }
}
