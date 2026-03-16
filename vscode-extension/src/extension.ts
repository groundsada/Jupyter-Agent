/**
 * JupyterHub Remote — VS Code Extension
 *
 * Handles: vscode://groundsada.jhub-vscode/connect?hub=...&token=...&user=...
 *
 * Flow:
 *  1. User clicks "Open in VS Code" in JupyterLab
 *  2. Browser opens the vscode:// URI
 *  3. If extension not installed → VS Code marketplace install prompt (built-in)
 *  4. Extension activates, URI handler fires
 *  5. Extension downloads jhub-ssh binary if not bundled
 *  6. Extension writes SSH config entry and token file
 *  7. Extension opens VS Code Remote-SSH window connected to the pod
 */

import * as vscode from 'vscode';
import { parseConnectURI, sshHostname, InvalidURIError } from './uri';
import { getBinaryPath } from './binary';
import { writeSSHConfig, writeToken, defaultTokenPath } from './sshconfig';

export function activate(context: vscode.ExtensionContext): void {
  // URI handler: vscode://groundsada.jhub-vscode/connect?...
  const uriHandler = vscode.window.registerUriHandler({
    handleUri(uri: vscode.Uri) {
      handleConnectUri(uri, context).catch((err: unknown) => {
        const msg = err instanceof Error ? err.message : String(err);
        vscode.window.showErrorMessage(`JupyterHub Remote: ${msg}`);
      });
    },
  });

  // Command: manually reconnect (opens host picker pre-filtered to jhub hosts)
  const reconnectCmd = vscode.commands.registerCommand(
    'jhub-vscode.reconnect',
    () => vscode.commands.executeCommand('opensshremotes.openEmptyWindowInCurrentConnection'),
  );

  context.subscriptions.push(uriHandler, reconnectCmd);
}

export function deactivate(): void {
  // nothing to clean up
}

async function handleConnectUri(
  uri: vscode.Uri,
  context: vscode.ExtensionContext,
): Promise<void> {
  // ── 1. Parse URI params ───────────────────────────────────────────────────
  let params;
  try {
    params = parseConnectURI(uri.query);
  } catch (err) {
    if (err instanceof InvalidURIError) {
      throw err;
    }
    throw new Error(`Could not parse connection URI: ${err}`);
  }

  const { hub, token, user, folder } = params;
  const hostname = sshHostname(hub, user);

  await vscode.window.withProgress(
    {
      location: vscode.ProgressLocation.Notification,
      title: `JupyterHub Remote: Connecting to ${user}'s server…`,
      cancellable: false,
    },
    async () => {
      // ── 2. Ensure jhub-ssh binary is available ──────────────────────────
      let binaryPath: string;
      try {
        binaryPath = await getBinaryPath(context);
      } catch (err) {
        throw new Error(
          `Failed to get jhub-ssh binary: ${err}\n\n` +
          `You can install it manually from https://github.com/groundsada/jupyter-ssh/releases`,
        );
      }

      // ── 3. Store token to disk (read by jhub-ssh ProxyCommand) ──────────
      const tokenPath = defaultTokenPath(hostname);
      writeToken(tokenPath, token);

      // ── 4. Write SSH config entry ─────────────────────────────────────
      writeSSHConfig({
        hostname,
        binaryPath,
        hubURL: hub,
        tokenPath,
      });

      // ── 5. Open Remote-SSH window ─────────────────────────────────────
      // opensshremotes.openEmptyWindowInCurrentConnection is the command
      // exposed by the ms-vscode-remote.remote-ssh extension.
      // It opens a new VS Code window connected to the given SSH host.
      try {
        await vscode.commands.executeCommand(
          'opensshremotes.openEmptyWindowInCurrentConnection',
          { hostName: hostname, remoteCommand: folder ? `cd ${folder} && $SHELL` : undefined },
        );
      } catch {
        // Older Remote-SSH versions use a different command name
        await vscode.commands.executeCommand(
          'opensshremotes.openEmptyWindowInCurrentConnection',
          hostname,
        );
      }
    },
  );
}
