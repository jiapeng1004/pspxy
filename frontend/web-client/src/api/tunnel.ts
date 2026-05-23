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
  /** 与服务端 api_key 中某一段完全一致；签名为 HEX(SHA1(k+"\\n"+ts+"\\n"+k)) */
  api_key?: string;
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
  reverse_providers?: ReverseProviderRow[];
  reverse_consumers?: ReverseConsumerRow[];
  issued_reverse_channel?: string;
  client_config_file?: string;
  persist_warning?: string;
}

export interface ReverseProviderRow {
  id: string;
  running: boolean;
  error?: string;
  channel_id?: string;
  local_host?: string;
  local_port?: number;
  server_url?: string;
  access_auth_configured?: boolean;
  /** 与 YAML enabled 一致：省略时服务端按 true 处理 */
  enabled: boolean;
}

export interface ReverseConsumerRow {
  id: string;
  running: boolean;
  error?: string;
  listen?: string;
  server_url?: string;
  access_auth_configured?: boolean;
  active_connections?: number;
  enabled: boolean;
}

export interface ReverseProviderPayload {
  id?: string;
  server_url: string;
  local_host?: string;
  local_port: number;
  api_key?: string;
  enabled?: boolean;
  /** 持久化的隧道 UUID，重启后优先 reclaim；编辑/开关时由界面传入以免写盘丢失 */
  channel_id?: string;
}

export interface ReverseConsumerPayload {
  /** 隧道 UUID，与 GET /ws/{id}、YAML id 一致 */
  id: string;
  listen: string;
  server_url: string;
  api_key?: string;
  enabled?: boolean;
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

export async function startReverseProvider(
  cfg: ReverseProviderPayload
): Promise<MultiTunnelStatus> {
  const { data } = await api.post<MultiTunnelStatus>('/api/tunnel/reverse/provider/start', cfg);
  return data;
}

export async function stopReverseProvider(id: string): Promise<MultiTunnelStatus> {
  const { data } = await api.post<MultiTunnelStatus>('/api/tunnel/reverse/provider/stop', { id });
  return data;
}

export async function removeReverseProvider(id: string): Promise<MultiTunnelStatus> {
  const { data } = await api.post<MultiTunnelStatus>('/api/tunnel/reverse/provider/remove', { id });
  return data;
}

export async function startReverseConsumer(
  cfg: ReverseConsumerPayload
): Promise<MultiTunnelStatus> {
  const { data } = await api.post<MultiTunnelStatus>('/api/tunnel/reverse/consumer/start', cfg);
  return data;
}

export async function stopReverseConsumer(id: string): Promise<MultiTunnelStatus> {
  const { data } = await api.post<MultiTunnelStatus>('/api/tunnel/reverse/consumer/stop', { id });
  return data;
}

export async function removeReverseConsumer(id: string): Promise<MultiTunnelStatus> {
  const { data } = await api.post<MultiTunnelStatus>('/api/tunnel/reverse/consumer/remove', { id });
  return data;
}

export async function stopAllReverseTunnel(): Promise<MultiTunnelStatus> {
  const { data } = await api.post<MultiTunnelStatus>('/api/tunnel/reverse/stop-all', {});
  return data;
}
