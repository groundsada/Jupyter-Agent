/**
 * URI parsing for vscode://groundsada.jhub-vscode/connect?...
 *
 * The JupyterLab button opens this URI. VS Code activates the extension
 * (installing it from the marketplace first if needed) and we parse the params.
 */

export interface ConnectParams {
  /** Full JupyterHub base URL, e.g. https://jupyter.example.com */
  hub: string;
  /** Short-lived JupyterHub API token (1 hour) */
  token: string;
  /** JupyterHub username */
  user: string;
  /** Folder to open in VS Code (default: /home/jovyan) */
  folder: string;
}

export class InvalidURIError extends Error {
  constructor(message: string) {
    super(message);
    this.name = 'InvalidURIError';
  }
}

/**
 * Parse a JupyterHub connect URI into ConnectParams.
 *
 * Expected format:
 *   vscode://groundsada.jhub-vscode/connect?hub=https://...&token=...&user=alice
 */
export function parseConnectURI(query: string): ConnectParams {
  const params = new URLSearchParams(query);

  const hub   = params.get('hub');
  const token = params.get('token');
  const user  = params.get('user');
  const folder = params.get('folder') ?? '/home/jovyan';

  if (!hub)   throw new InvalidURIError('Missing required param: hub');
  if (!token) throw new InvalidURIError('Missing required param: token');
  if (!user)  throw new InvalidURIError('Missing required param: user');

  // Basic validation
  try {
    new URL(hub);
  } catch {
    throw new InvalidURIError(`Invalid hub URL: ${hub}`);
  }

  if (user.includes('/') || user.includes('..')) {
    throw new InvalidURIError(`Invalid username: ${user}`);
  }

  return { hub, token, user, folder };
}

/**
 * Derive the SSH hostname for a user from the hub URL.
 *
 * https://jupyter.example.com + alice → alice.jupyter.example.com
 *
 * This matches the wildcard ingress format used by the port-forwarding proxy
 * and the SSH gateway.
 */
export function sshHostname(hub: string, user: string): string {
  const url = new URL(hub);
  return `${user}.${url.hostname}`;
}
