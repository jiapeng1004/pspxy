/** 仅存 sessionStorage（关闭标签页失效），并按服务端 HTTP Origin 分区，不把 AK/SK 写入 localStorage */

import { resolveServerHttpOrigin } from './server-auth';

export interface TunnelStoredCreds {
  access_key: string;
  secret_key: string;
}

const PREFIX = 'pspxy_tunnel_sess:v1';

function storageKey(serverBase: string): string | null {
  const origin = resolveServerHttpOrigin(serverBase);
  return origin ? `${PREFIX}:${origin}` : null;
}

export function loadTunnelCreds(serverBase: string): TunnelStoredCreds | null {
  const key = storageKey(serverBase);
  if (!key) return null;
  try {
    const raw = sessionStorage.getItem(key);
    if (!raw) return null;
    const o = JSON.parse(raw) as Record<string, unknown>;
    const ak = typeof o.access_key === 'string' ? o.access_key : '';
    const sk = typeof o.secret_key === 'string' ? o.secret_key : '';
    if (!ak || !sk) return null;
    return { access_key: ak, secret_key: sk };
  } catch {
    return null;
  }
}

export function saveTunnelCreds(serverBase: string, accessKey: string, secretKey: string): void {
  const key = storageKey(serverBase);
  if (!key) return;
  const ak = accessKey.trim();
  const sk = secretKey.trim();
  if (!ak || !sk) {
    sessionStorage.removeItem(key);
    return;
  }
  sessionStorage.setItem(key, JSON.stringify({ access_key: ak, secret_key: sk }));
}

export function clearTunnelCreds(serverBase: string): void {
  const key = storageKey(serverBase);
  if (key) sessionStorage.removeItem(key);
}
