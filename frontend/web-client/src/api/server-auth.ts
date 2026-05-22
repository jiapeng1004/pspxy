/**
 * 从用户填写的「服务端基址」推导 HTTP Origin，用于探测 /api/v1/auth/enabled。
 * 支持 ws(s):// / http(s)://；路径部分忽略（API 假定在根路径）。
 */
export function resolveServerHttpOrigin(raw: string): string | null {
  const trimmed = raw.trim();
  if (!trimmed) return null;
  try {
    const u = new URL(trimmed);
    if (u.protocol === 'http:' || u.protocol === 'https:') {
      return `${u.protocol}//${u.host}`;
    }
    if (u.protocol === 'ws:') {
      return `http://${u.host}`;
    }
    if (u.protocol === 'wss:') {
      return `https://${u.host}`;
    }
  } catch {
    return null;
  }
  return null;
}

/** GET {origin}/api/v1/auth/enabled（公开，无签名） */
export async function fetchRemoteAuthRequired(
  serverWsOrHttpBaseUrl: string
): Promise<{ ok: true; authRequired: boolean } | { ok: false; reason: string }> {
  const origin = resolveServerHttpOrigin(serverWsOrHttpBaseUrl);
  if (!origin) {
    return { ok: false, reason: '无法解析服务端地址，请填写 ws(s):// 或 http(s)://' };
  }
  const url = `${origin.replace(/\/$/, '')}/api/v1/auth/enabled`;
  try {
    const res = await fetch(url, {
      method: 'GET',
      mode: 'cors',
      cache: 'no-store',
    });
    if (!res.ok) {
      return { ok: false, reason: `探测鉴权接口失败 (${res.status})，请核对地址与跨域访问` };
    }
    const data = (await res.json()) as { auth_required?: boolean };
    return { ok: true, authRequired: !!data.auth_required };
  } catch (e: unknown) {
    const msg = e instanceof Error ? e.message : String(e);
    return { ok: false, reason: `无法访问 ${url}: ${msg}` };
  }
}
