import * as fs from 'fs';
import * as os from 'os';
import * as path from 'path';
import { writeSSHConfig, removeSSHConfig } from './sshconfig';

let tmpDir: string;
let configPath: string;

beforeEach(() => {
  tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), 'jhub-vscode-test-'));
  configPath = path.join(tmpDir, '.ssh', 'config');
});

afterEach(() => {
  fs.rmSync(tmpDir, { recursive: true, force: true });
});

const entry = {
  hostname: 'alice.jupyter.example.com',
  binaryPath: '/usr/local/bin/jhub-ssh',
  hubURL: 'https://jupyter.example.com',
  tokenPath: '/tmp/token-alice',
};

it('creates .ssh directory if missing', () => {
  writeSSHConfig(entry, configPath);
  expect(fs.existsSync(configPath)).toBe(true);
});

it('writes valid SSH config block', () => {
  writeSSHConfig(entry, configPath);
  const content = fs.readFileSync(configPath, 'utf8');
  expect(content).toContain('# BEGIN JUPYTERHUB');
  expect(content).toContain('# END JUPYTERHUB');
  expect(content).toContain('Host alice.jupyter.example.com');
  expect(content).toContain('ProxyCommand /usr/local/bin/jhub-ssh proxy-connect');
  expect(content).toContain('StrictHostKeyChecking no');
});

it('replaces existing block on second write', () => {
  writeSSHConfig(entry, configPath);
  writeSSHConfig({ ...entry, hostname: 'bob.jupyter.example.com', tokenPath: '/tmp/token-bob' }, configPath);
  const content = fs.readFileSync(configPath, 'utf8');
  expect(content).toContain('bob.jupyter.example.com');
  expect(content).not.toContain('alice.jupyter.example.com');
  const starts = (content.match(/# BEGIN JUPYTERHUB/g) ?? []).length;
  expect(starts).toBe(1);
});

it('preserves existing SSH config around the block', () => {
  fs.mkdirSync(path.dirname(configPath), { recursive: true });
  fs.writeFileSync(configPath, 'Host myserver\n  User alice\n');
  writeSSHConfig(entry, configPath);
  const content = fs.readFileSync(configPath, 'utf8');
  expect(content).toContain('Host myserver');
  expect(content).toContain('# BEGIN JUPYTERHUB');
});

it('removeSSHConfig strips the block', () => {
  writeSSHConfig(entry, configPath);
  removeSSHConfig(configPath);
  const content = fs.readFileSync(configPath, 'utf8');
  expect(content).not.toContain('BEGIN JUPYTERHUB');
  expect(content).not.toContain('END JUPYTERHUB');
});

it('removeSSHConfig is no-op when no block exists', () => {
  fs.mkdirSync(path.dirname(configPath), { recursive: true });
  fs.writeFileSync(configPath, 'Host myserver\n  User alice\n');
  expect(() => removeSSHConfig(configPath)).not.toThrow();
  expect(fs.readFileSync(configPath, 'utf8')).toContain('Host myserver');
});

it('removeSSHConfig is no-op when file does not exist', () => {
  expect(() => removeSSHConfig(configPath)).not.toThrow();
});
