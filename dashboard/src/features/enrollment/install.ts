/**
 * Builds agent install commands exactly in the formats given by docs/api.md:
 *   Linux   (root):  sudo ./agentmesh-agent install --server <gateway_url> --token <token> [--ca-file ./ca.pem]
 *   Windows (admin): .\agentmesh-agent.exe install --server <gateway_url> --token <token> [--ca-file .\ca.pem]
 */
export function linuxInstallCommand(gatewayUrl: string, token: string, withCa: boolean): string {
  return (
    `sudo ./agentmesh-agent install --server ${gatewayUrl} --token ${token}` +
    (withCa ? ' --ca-file ./ca.pem' : '')
  );
}

export function windowsInstallCommand(gatewayUrl: string, token: string, withCa: boolean): string {
  return (
    `.\\agentmesh-agent.exe install --server ${gatewayUrl} --token ${token}` +
    (withCa ? ' --ca-file .\\ca.pem' : '')
  );
}

export const EXPIRY_OPTIONS = [
  { value: String(3600), label: '1 hour' },
  { value: String(86400), label: '24 hours' },
  { value: String(7 * 86400), label: '7 days' },
  { value: String(30 * 86400), label: '30 days' },
] as const;
