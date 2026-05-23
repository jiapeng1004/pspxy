import axios, { type AxiosError } from 'axios';
import { loadAccessCredentials, clearAccessCredentials } from '../auth/session';
import { signAccessPayload } from '../auth/sign';

const api = axios.create({
  baseURL: '/api/v1',
  timeout: 10000,
});

function normalizeRelV1Url(rawUrl?: string): string {
  if (!rawUrl) return '';
  return rawUrl.split('?')[0].replace(/^\//, '');
}

/** 不进行签名的公开鉴权路由（仅占位，登录当前用 fetch）。 */
function isUnsignedAuthProbePath(rawUrl?: string): boolean {
  const p = normalizeRelV1Url(rawUrl);
  return p === 'auth/enabled' || p === 'auth/login';
}

/** 与服务端对齐的 Header 名称 */
const HDR_AK = 'X-Psp-Ak';
const HDR_TS = 'X-Psp-Timestamp';
const HDR_SIG = 'X-Psp-Signature';

api.interceptors.request.use(async (config) => {
  if (isUnsignedAuthProbePath(config.url)) {
    return config;
  }

  const cred = loadAccessCredentials();
  if (!cred?.apiKey.trim()) return config;

  const ts = String(Math.floor(Date.now() / 1000));
  const akHeader = cred.apiKey.trim();
  const sig = await signAccessPayload(akHeader, ts, akHeader);
  config.headers.set(HDR_AK, akHeader);
  config.headers.set(HDR_TS, ts);
  config.headers.set(HDR_SIG, sig);

  return config;
});

api.interceptors.response.use(undefined, (error: AxiosError) => {
  const status = error.response?.status;
  const rel = normalizeRelV1Url(error.config?.url as string | undefined);
  if (status === 401 && rel !== 'auth/login') {
    clearAccessCredentials();
    if (typeof window !== 'undefined') {
      const full = `${window.location.pathname}${window.location.search}`;
      const next =
        full !== '/login' ? `?next=${encodeURIComponent(full)}` : '';
      window.location.replace(`/login${next}`);
    }
  }
  return Promise.reject(error);
});

/** POST /api/v1/auth/login 校验 api_key（明文 JSON，不参与后续请求签名通道）。 */
export async function verifyAccessLogin(apiKey: string): Promise<{ ok: boolean; auth_required: boolean }> {
  const k = apiKey.trim();
  const r = await fetch('/api/v1/auth/login', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      api_key: k,
    }),
  });
  let data: { ok?: boolean; auth_required?: boolean; message?: string; error?: string } =
    {};
  try {
    data = (await r.json()) as typeof data;
  } catch {
    /* ignore */
  }
  if (!r.ok) {
    throw new Error(data.message ?? data.error ?? `校验失败 (${r.status})`);
  }
  return {
    ok: !!data.ok,
    auth_required: !!data.auth_required,
  };
}

/** 探测服务端是否配置了 AK/SK（公开路由，不参与签名）。 */
export async function fetchAuthRequired(): Promise<boolean> {
  const r = await fetch('/api/v1/auth/enabled', { cache: 'no-store' });
  if (!r.ok) {
    throw new Error(`auth probe ${r.status}`);
  }
  const data = (await r.json()) as { auth_required?: boolean };
  return !!data.auth_required;
}

/** 配置中的代理条目（不包含服务端监听端口——入口统一在服务 global port 的 WS 路径上） */
export interface ProxyConfig {
  id: string;
  name: string;
  remote_address: string;
  enabled: boolean;
}

export interface ProxyStatus extends ProxyConfig {
  ws_path: string;
  status: 'running' | 'stopped' | 'error';
  connections: number;
  started_at: string;
  error?: string;
}

export interface CreateProxyRequest {
  name: string;
  remote_address: string;
  enabled: boolean;
}

export interface HealthStatus {
  status: string;
  uptime: number;
  proxies_running: number;
  proxies_total: number;
}

// 代理 CRUD
export async function listProxies(params?: {
  enabled?: string;
}): Promise<ProxyStatus[]> {
  const { data } = await api.get<{ proxies: ProxyStatus[] }>('/proxies', { params });
  return data.proxies;
}

export async function createProxy(req: CreateProxyRequest): Promise<ProxyConfig> {
  const { data } = await api.post<ProxyConfig>('/proxies', req);
  return data;
}

export async function getProxy(id: string): Promise<ProxyStatus> {
  const { data } = await api.get<ProxyStatus>(`/proxies/${id}`);
  return data;
}

export async function updateProxy(
  id: string,
  req: CreateProxyRequest
): Promise<ProxyConfig> {
  const { data } = await api.put<ProxyConfig>(`/proxies/${id}`, req);
  return data;
}

export async function deleteProxy(id: string): Promise<void> {
  await api.delete(`/proxies/${id}`);
}

export async function reloadConfig(): Promise<{ status: string; proxies: number }> {
  const { data } = await api.post<{ status: string; proxies: number }>(
    '/config/reload'
  );
  return data;
}

export async function healthCheck(): Promise<HealthStatus> {
  const { data } = await api.get<HealthStatus>('/health');
  return data;
}
