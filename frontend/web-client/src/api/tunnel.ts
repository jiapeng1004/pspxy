import axios from 'axios';

const api = axios.create({
  baseURL: '',
  timeout: 30000,
});

export interface TunnelConfig {
  id?: string;
  server_url: string;
  proxy_id: string;
  local_port: number;
  access_key?: string;
  secret_key?: string;
}

export interface TunnelPublicLastConfig {
  server_url: string;
  proxy_id: string;
  local_port: number;
  access_auth_configured?: boolean;
}

export interface ProxyRow {
  id: string;
  running: boolean;
  error?: string;
  listen?: string;
  message?: string;
  active_connections?: number;
  last_config: TunnelPublicLastConfig;
}

export interface MultiTunnelStatus {
  proxies: ProxyRow[];
  client_config_file?: string;
  persist_warning?: string;
}

export async function fetchTunnelStatus(): Promise<MultiTunnelStatus> {
  const { data } = await api.get<MultiTunnelStatus>('/api/tunnel/status');
  return data;
}

export async function startProxy(cfg: TunnelConfig): Promise<MultiTunnelStatus> {
  const { data } = await api.post<MultiTunnelStatus>('/api/tunnel/proxy/start', cfg);
  return data;
}

export async function stopProxy(opts: {
  id?: string;
  stop_all?: boolean;
}): Promise<MultiTunnelStatus> {
  const { data } = await api.post<MultiTunnelStatus>('/api/tunnel/stop', opts);
  return data;
}

export async function removeProxy(id: string): Promise<MultiTunnelStatus> {
  const { data } = await api.post<MultiTunnelStatus>('/api/tunnel/proxy/remove', {
    id,
  });
  return data;
}
