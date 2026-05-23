/** 与服务端 SignAccessPayload 一致：hex(SHA1(accessKey.trim()+'\n'+unixSec.trim()+'\n'+secretKey.trim()))；单列密钥时 accessKey===secretKey。 */

export async function signAccessPayload(
  accessKey: string,
  unixSeconds: string,
  secretKey: string
): Promise<string> {
  const canonical = `${accessKey.trim()}\n${unixSeconds.trim()}\n${secretKey.trim()}`;
  const data = new TextEncoder().encode(canonical);
  const digest = await crypto.subtle.digest('SHA-1', data);
  return Array.from(new Uint8Array(digest))
    .map((b) => b.toString(16).padStart(2, '0'))
    .join('');
}
