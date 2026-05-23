/** 管理端凭据仅存 sessionStorage；字段为单列 api_key 片段。 */

export const STORAGE_API_KEY = 'pspxy_admin_api_key_unified';

export interface AccessCredentials {
  apiKey: string;
}

export function loadAccessCredentials(): AccessCredentials | null {
  try {
    const k = sessionStorage.getItem(STORAGE_API_KEY)?.trim();
    if (!k) return null;
    return { apiKey: k };
  } catch {
    return null;
  }
}

export function saveApiKey(apiKey: string): void {
  const k = apiKey.trim();
  if (!k) {
    sessionStorage.removeItem(STORAGE_API_KEY);
    return;
  }
  sessionStorage.setItem(STORAGE_API_KEY, k);
}

/** @deprecated 请改用 saveApiKey */
export function saveUnifiedApiKey(apiKey: string): void {
  saveApiKey(apiKey);
}

export function clearAccessCredentials(): void {
  try {
    sessionStorage.removeItem(STORAGE_API_KEY);
    /** 移除旧版本分键存储（若存在） */
    sessionStorage.removeItem('pspxy_admin_access_key');
    sessionStorage.removeItem('pspxy_admin_secret_key');
  } catch {
    /* ignore */
  }
}
