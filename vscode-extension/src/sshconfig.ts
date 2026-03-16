/**
 * Manages the JupyterHub SSH config block in ~/.ssh/config.
 *
 * Mirrors the Go sshconfig package but runs inside the VS Code extension
 * without shelling out — pure Node.js file I/O.
 */

import * as fs from 'fs';
import * as os from 'os';
import * as path from 'path';

const START_MARKER = '# BEGIN JUPYTERHUB';
const END_MARKER   = '# END JUPYTERHUB';

export interface HostEntry {
  /** SSH hostname, e.g. "alice.jupyter.example.com" */
  hostname: string;
  /** Absolute path to the jhub-ssh binary */
  binaryPath: string;
  /** Hub URL, e.g. "https://jupyter.example.com" */
  hubURL: string;
  /** Path to the token file */
  tokenPath: string;
}

/**
 * Write (or update) the JupyterHub block in ~/.ssh/config.
 * Returns the path of the config file that was written.
 *
 * @param configPath  Override for the SSH config file path (used in tests).
 */
export function writeSSHConfig(entry: HostEntry, configPath = defaultSSHConfigPath()): string {
  const dir = path.dirname(configPath);

  if (!fs.existsSync(dir)) fs.mkdirSync(dir, { recursive: true, mode: 0o700 });

  const existing = fs.existsSync(configPath)
    ? fs.readFileSync(configPath, 'utf8')
    : '';

  const block = buildBlock(entry);
  const updated = upsertBlock(existing, block);

  // Write atomically
  const tmp = configPath + '.jhub-tmp';
  fs.writeFileSync(tmp, updated, { mode: 0o600 });
  fs.renameSync(tmp, configPath);

  return configPath;
}

/**
 * Remove the JupyterHub block from ~/.ssh/config.
 *
 * @param configPath  Override for the SSH config file path (used in tests).
 */
export function removeSSHConfig(configPath = defaultSSHConfigPath()): void {
  if (!fs.existsSync(configPath)) return;
  const content = fs.readFileSync(configPath, 'utf8');
  const updated = removeBlock(content);
  fs.writeFileSync(configPath, updated, { mode: 0o600 });
}

function buildBlock(entry: HostEntry): string {
  const proxyCmd = `${entry.binaryPath} proxy-connect --hub ${entry.hubURL} --token-file ${entry.tokenPath} %r`;
  const lines = [
    START_MARKER,
    // Exact hostname for `ssh alice.jupyter.example.com`
    `Host ${entry.hostname}`,
    '  User jovyan',
    '  StrictHostKeyChecking no',
    '  UserKnownHostsFile /dev/null',
    `  ProxyCommand ${proxyCmd}`,
    END_MARKER,
  ];
  return lines.join('\n') + '\n';
}

function upsertBlock(existing: string, newBlock: string): string {
  const start = existing.indexOf(START_MARKER);
  const end   = existing.indexOf(END_MARKER);

  if (start === -1 && end === -1) {
    const sep = existing.length > 0 && !existing.endsWith('\n') ? '\n' : '';
    return `${existing}${sep}\n${newBlock}`;
  }

  if (start !== -1 && end !== -1 && start < end) {
    const before = existing.slice(0, start);
    const after  = existing.slice(end + END_MARKER.length).replace(/^\n/, '');
    return `${before}${newBlock}${after}`;
  }

  throw new Error('Malformed JupyterHub SSH config block (mismatched markers)');
}

function removeBlock(content: string): string {
  const start = content.indexOf(START_MARKER);
  if (start === -1) return content;
  const end = content.indexOf(END_MARKER);
  if (end === -1) return content;
  return content.slice(0, start) + content.slice(end + END_MARKER.length).replace(/^\n/, '');
}

export function defaultSSHConfigPath(): string {
  return path.join(os.homedir(), '.ssh', 'config');
}

export function defaultTokenPath(hostname: string): string {
  return path.join(os.homedir(), '.config', 'jhub-ssh', `token-${hostname}`);
}

/** Write a token to disk with restricted permissions. */
export function writeToken(tokenPath: string, token: string): void {
  const dir = path.dirname(tokenPath);
  if (!fs.existsSync(dir)) fs.mkdirSync(dir, { recursive: true, mode: 0o700 });
  fs.writeFileSync(tokenPath, token, { mode: 0o600 });
}
