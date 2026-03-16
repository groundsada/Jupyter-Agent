/**
 * Unit tests for the VS Code extension logic.
 * Tests the pure functions — not the DOM/widget rendering.
 */

// ── Helpers extracted for testability ────────────────────────────────────────

/**
 * Validate that a vscode:// URI has the expected structure.
 */
function isValidVSCodeURI(uri: string): boolean {
  return uri.startsWith('vscode://') || uri.startsWith('vscode-insiders://');
}

/**
 * Build the vscode:// URI from connection info (mirrors hub apihandlers.py logic).
 */
function buildVSCodeURI(sshHost: string, sshUser: string): string {
  return `vscode://ms-vscode-remote.remote-ssh/open?hostName=${sshHost}&user=${sshUser}`;
}

/**
 * Build the setup command string (mirrors hub apihandlers.py logic).
 */
function buildSetupCmd(hubURL: string, token: string): string {
  return `jhub-ssh config-ssh --hub ${hubURL} --token ${token}`;
}

// ── Tests ─────────────────────────────────────────────────────────────────────

describe('buildVSCodeURI', () => {
  it('starts with vscode://', () => {
    const uri = buildVSCodeURI('jupyter.example.com', 'jovyan');
    expect(isValidVSCodeURI(uri)).toBe(true);
  });

  it('includes the ssh host', () => {
    const uri = buildVSCodeURI('jupyter.example.com', 'jovyan');
    expect(uri).toContain('jupyter.example.com');
  });

  it('includes the ssh user', () => {
    const uri = buildVSCodeURI('jupyter.example.com', 'jovyan');
    expect(uri).toContain('user=jovyan');
  });

  it('uses remote-ssh extension scheme', () => {
    const uri = buildVSCodeURI('jupyter.example.com', 'jovyan');
    expect(uri).toContain('ms-vscode-remote.remote-ssh');
  });
});

describe('buildSetupCmd', () => {
  it('includes jhub-ssh binary', () => {
    const cmd = buildSetupCmd('https://jupyter.example.com', 'mytoken');
    expect(cmd).toContain('jhub-ssh config-ssh');
  });

  it('includes hub URL', () => {
    const cmd = buildSetupCmd('https://jupyter.example.com', 'mytoken');
    expect(cmd).toContain('https://jupyter.example.com');
  });

  it('includes token', () => {
    const cmd = buildSetupCmd('https://jupyter.example.com', 'mytoken');
    expect(cmd).toContain('mytoken');
  });
});

describe('isValidVSCodeURI', () => {
  it('accepts vscode:// URIs', () => {
    expect(isValidVSCodeURI('vscode://some.extension/path')).toBe(true);
  });

  it('accepts vscode-insiders:// URIs', () => {
    expect(isValidVSCodeURI('vscode-insiders://some.extension/path')).toBe(true);
  });

  it('rejects http:// URIs', () => {
    expect(isValidVSCodeURI('http://example.com')).toBe(false);
  });

  it('rejects empty string', () => {
    expect(isValidVSCodeURI('')).toBe(false);
  });
});
