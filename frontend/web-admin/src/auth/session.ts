/** 管理端 AK/SK 仅存 sessionStorage（与后端 AK/SK 鉴权对齐） */

export const STORAGE_ACCESS_KEY = 'pspxy_admin_access_key';
export const STORAGE_SECRET_KEY = 'pspxy_admin_secret_key';

export interface AccessCredentials {
  accessKey: string;
  secretKey: string;
}

export function loadAccessCredentials(): AccessCredentials | null {
  try {
    const ak = sessionStorage.getItem(STORAGE_ACCESS_KEY);
    const sk = sessionStorage.getItem(STORAGE_SECRET_KEY);
    if (!ak?.trim() || !sk?.trim()) return null;
    return { accessKey: ak.trim(), secretKey: sk.trim() };
  } catch {
    return null;
  }
}

export function saveAccessCredentials(ak: string, sk: string): void {
  sessionStorage.setItem(STORAGE_ACCESS_KEY, ak.trim());
  sessionStorage.setItem(STORAGE_SECRET_KEY, sk.trim());
}

export function clearAccessCredentials(): void {
  try {
    sessionStorage.removeItem(STORAGE_ACCESS_KEY);
    sessionStorage.removeItem(STORAGE_SECRET_KEY);
  } catch {
    /* ignore */
  }
}
