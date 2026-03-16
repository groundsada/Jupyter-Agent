/**
 * Manages the bundled jhub-ssh binary.
 *
 * Each platform-specific .vsix on the marketplace contains the correct binary
 * for that platform (set via "vsce publish --target <platform>").
 * The binary is stored at:
 *   <extension-dir>/bin/jhub-ssh          (Linux/macOS)
 *   <extension-dir>/bin/jhub-ssh.exe      (Windows)
 *
 * During development and CI, the binary is downloaded from GitHub Releases
 * on first use and cached in the extension's global storage path.
 */

import * as fs from 'fs';
import * as https from 'https';
import * as os from 'os';
import * as path from 'path';
import * as vscode from 'vscode';

const GITHUB_REPO = 'groundsada/jupyter-ssh';
const BINARY_NAME = process.platform === 'win32' ? 'jhub-ssh.exe' : 'jhub-ssh';

/**
 * Returns the path to the jhub-ssh binary, downloading it if necessary.
 *
 * Search order:
 *  1. <extension-dir>/bin/jhub-ssh   — bundled in platform .vsix (production)
 *  2. <globalStoragePath>/jhub-ssh   — cached download (fallback / dev)
 */
export async function getBinaryPath(
  context: vscode.ExtensionContext,
): Promise<string> {
  // 1. Check for bundled binary (installed via platform-specific .vsix)
  const bundled = path.join(context.extensionPath, 'bin', BINARY_NAME);
  if (fs.existsSync(bundled)) {
    await ensureExecutable(bundled);
    return bundled;
  }

  // 2. Check global storage cache
  const cached = path.join(context.globalStoragePath, BINARY_NAME);
  if (fs.existsSync(cached)) {
    await ensureExecutable(cached);
    return cached;
  }

  // 3. Download from GitHub Releases
  return vscode.window.withProgress(
    {
      location: vscode.ProgressLocation.Notification,
      title: 'JupyterHub Remote: Downloading SSH helper…',
      cancellable: false,
    },
    () => downloadBinary(context),
  );
}

async function downloadBinary(context: vscode.ExtensionContext): Promise<string> {
  const version = getPackageVersion();
  const platform = goPlatform();
  const arch = goArch();
  const ext = process.platform === 'win32' ? '.exe' : '';
  const filename = `jhub-ssh-${platform}-${arch}${ext}`;
  const url = `https://github.com/${GITHUB_REPO}/releases/download/v${version}/${filename}`;

  fs.mkdirSync(context.globalStoragePath, { recursive: true });
  const dest = path.join(context.globalStoragePath, BINARY_NAME);

  await download(url, dest);
  await ensureExecutable(dest);
  return dest;
}

function getPackageVersion(): string {
  // eslint-disable-next-line @typescript-eslint/no-var-requires
  const pkg = require(path.join(__dirname, '..', 'package.json')) as { binaryVersion: string };
  return pkg.binaryVersion;
}

function goPlatform(): string {
  const map: Record<string, string> = {
    darwin: 'darwin',
    linux: 'linux',
    win32: 'windows',
  };
  const p = map[process.platform];
  if (!p) throw new Error(`Unsupported platform: ${process.platform}`);
  return p;
}

function goArch(): string {
  const map: Record<string, string> = {
    x64: 'amd64',
    arm64: 'arm64',
  };
  const a = map[os.arch()];
  if (!a) throw new Error(`Unsupported architecture: ${os.arch()}`);
  return a;
}

function download(url: string, dest: string): Promise<void> {
  return new Promise((resolve, reject) => {
    const file = fs.createWriteStream(dest);
    const get = (u: string) => {
      https.get(u, (res) => {
        // Follow redirects (GitHub releases redirect to S3)
        if (res.statusCode === 301 || res.statusCode === 302) {
          const location = res.headers.location;
          if (location) return get(location);
          return reject(new Error('Redirect with no Location header'));
        }
        if (res.statusCode !== 200) {
          return reject(new Error(`Download failed: HTTP ${res.statusCode} from ${u}`));
        }
        res.pipe(file);
        file.on('finish', () => file.close(() => resolve()));
        file.on('error', reject);
      }).on('error', reject);
    };
    get(url);
  });
}

async function ensureExecutable(filePath: string): Promise<void> {
  if (process.platform !== 'win32') {
    fs.chmodSync(filePath, 0o755);
  }
}
