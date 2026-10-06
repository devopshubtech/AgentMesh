/**
 * Builds agent install commands exactly in the formats given by docs/api.md:
 *   macOS / Linux (root): sudo ./agentmesh-agent install --server <gateway_url> --token <token> [--ca-file ./ca.pem] [--enable-exit-node]
 *   Windows (admin):      .\agentmesh-agent.exe install --server <gateway_url> --token <token> [--ca-file .\ca.pem] [--enable-exit-node]
 */
export interface InstallOptions {
  /** Add --ca-file for a self-signed gateway CA. */
  withCa: boolean;
  /** Add --enable-exit-node so phones can use this device's internet. */
  exitNode: boolean;
}

export function unixInstallCommand(gatewayUrl: string, token: string, o: InstallOptions): string {
  return (
    `sudo ./agentmesh-agent install --server ${gatewayUrl} --token ${token}` +
    (o.withCa ? ' --ca-file ./ca.pem' : '') +
    (o.exitNode ? ' --enable-exit-node' : '')
  );
}

export function windowsInstallCommand(gatewayUrl: string, token: string, o: InstallOptions): string {
  return (
    `.\\agentmesh-agent.exe install --server ${gatewayUrl} --token ${token}` +
    (o.withCa ? ' --ca-file .\\ca.pem' : '') +
    (o.exitNode ? ' --enable-exit-node' : '')
  );
}

/** expires_in_s value for a token that never expires (the server stores 9999-12-31). */
export const NEVER_EXPIRES = -1;

/** True for a token created with NEVER_EXPIRES. */
export const neverExpires = (expiresAt: string): boolean => new Date(expiresAt).getUTCFullYear() >= 9999;

export const EXPIRY_OPTIONS = [
  { value: String(3600), label: '1 hour' },
  { value: String(86400), label: '24 hours' },
  { value: String(7 * 86400), label: '7 days' },
  { value: String(30 * 86400), label: '30 days' },
  { value: String(NEVER_EXPIRES), label: 'Never (no expiry)' },
] as const;
