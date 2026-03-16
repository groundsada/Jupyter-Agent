/**
 * JupyterLab extension: Open in VS Code
 *
 * Adds an "Open in VS Code" button to the JupyterLab top bar.
 *
 * Clicking it:
 *  1. Calls /hub/api/users/{name}/vscode-connect to get a signed, short-lived URI
 *  2. Opens vscode://groundsada.jhub-vscode/connect?hub=...&token=...&user=...
 *  3. VS Code handles the rest:
 *     - Prompts to install "JupyterHub Remote" extension if not present
 *     - Downloads jhub-ssh binary if needed
 *     - Writes SSH config + opens remote window
 *
 * No setup, no CLI, no config required from the user.
 */

import {
  JupyterFrontEnd,
  JupyterFrontEndPlugin,
} from '@jupyterlab/application';

import { ITopBar } from '@jupyterlab/apputils';
import { PageConfig } from '@jupyterlab/coreutils';
import { ServerConnection } from '@jupyterlab/services';
import { Widget } from '@lumino/widgets';

interface VSCodeConnectResponse {
  username: string;
  ssh_host: string;
  vscode_uri: string;
}

async function fetchConnectURI(username: string): Promise<VSCodeConnectResponse> {
  const settings = ServerConnection.makeSettings();
  const hubPrefix = PageConfig.getOption('hubPrefix') || '/hub';
  const url = `${hubPrefix}/api/users/${encodeURIComponent(username)}/vscode-connect`;

  const response = await ServerConnection.makeRequest(url, { method: 'GET' }, settings);
  if (!response.ok) {
    const text = await response.text();
    throw new Error(`Hub API ${response.status}: ${text}`);
  }
  return response.json() as Promise<VSCodeConnectResponse>;
}

class VSCodeButton extends Widget {
  private readonly _username: string;

  constructor(username: string) {
    super();
    this._username = username;
    this.addClass('jp-VSCodeButton');

    const btn = document.createElement('button');
    btn.title = 'Open in VS Code';
    btn.style.cssText = `
      display: flex; align-items: center; gap: 6px;
      padding: 4px 10px; cursor: pointer; border-radius: 4px;
      background: transparent; border: 1px solid var(--jp-border-color1);
      color: var(--jp-ui-font-color1); font-size: 13px;
      font-family: var(--jp-ui-font-family);
    `;
    btn.innerHTML = `
      <svg width="16" height="16" viewBox="0 0 100 100" xmlns="http://www.w3.org/2000/svg" fill="currentColor">
        <path d="M74.8 4.9L40.6 34.7 17.3 18.5 5 25.3v49.4l12.3 6.8 23.3-16.2 34.2 29.8 18.2-8.7V13.6L74.8 4.9zM17.3 62.6V37.4l16.4 12.6-16.4 12.6zm39.4-.5L38.2 50l18.5-12.1V62.1z"/>
      </svg>
      Open in VS Code
    `;
    btn.addEventListener('click', () => void this._onClick());
    this.node.appendChild(btn);
  }

  private async _onClick(): Promise<void> {
    const btn = this.node.querySelector('button')!;
    btn.textContent = 'Connecting\u2026';
    btn.setAttribute('disabled', 'true');

    try {
      const info = await fetchConnectURI(this._username);
      // Open the vscode:// URI.
      // VS Code intercepts this and installs "JupyterHub Remote" from the
      // marketplace if not already present, then handles the connection.
      window.location.href = info.vscode_uri;
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err);
      console.error('[jhub-vscode]', msg);
      btn.textContent = '\u26a0 Failed';
      btn.title = msg;
      setTimeout(() => {
        btn.innerHTML = `
          <svg width="16" height="16" viewBox="0 0 100 100" xmlns="http://www.w3.org/2000/svg" fill="currentColor">
            <path d="M74.8 4.9L40.6 34.7 17.3 18.5 5 25.3v49.4l12.3 6.8 23.3-16.2 34.2 29.8 18.2-8.7V13.6L74.8 4.9zM17.3 62.6V37.4l16.4 12.6-16.4 12.6zm39.4-.5L38.2 50l18.5-12.1V62.1z"/>
          </svg>
          Open in VS Code
        `;
        btn.removeAttribute('disabled');
        btn.title = 'Open in VS Code';
      }, 3000);
    } finally {
      btn.removeAttribute('disabled');
    }
  }
}

const plugin: JupyterFrontEndPlugin<void> = {
  id: '@groundsada/jupyterhub-vscode:plugin',
  description: 'One-click "Open in VS Code" button for JupyterHub',
  autoStart: true,
  optional: [ITopBar],
  activate: (app: JupyterFrontEnd, topBar: ITopBar | null) => {
    const hubUser = PageConfig.getOption('hubUser');
    if (!hubUser) {
      console.log('[jhub-vscode] Not running under JupyterHub \u2014 skipping');
      return;
    }

    const button = new VSCodeButton(hubUser);
    if (topBar) {
      topBar.addItem('vscode-button', button);
    } else {
      app.shell.add(button, 'top', { rank: 1000 });
    }
  },
};

export default plugin;
