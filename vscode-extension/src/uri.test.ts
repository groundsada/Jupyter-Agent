import { parseConnectURI, sshHostname, InvalidURIError } from './uri';

describe('parseConnectURI', () => {
  const valid = 'hub=https%3A%2F%2Fjupyter.example.com&token=mytoken&user=alice';

  it('parses valid params', () => {
    const p = parseConnectURI(valid);
    expect(p.hub).toBe('https://jupyter.example.com');
    expect(p.token).toBe('mytoken');
    expect(p.user).toBe('alice');
    expect(p.folder).toBe('/home/jovyan');
  });

  it('accepts custom folder', () => {
    const p = parseConnectURI(valid + '&folder=%2Fwork');
    expect(p.folder).toBe('/work');
  });

  it('throws InvalidURIError when hub missing', () => {
    expect(() => parseConnectURI('token=t&user=alice')).toThrow(InvalidURIError);
  });

  it('throws InvalidURIError when token missing', () => {
    expect(() => parseConnectURI('hub=https://x.com&user=alice')).toThrow(InvalidURIError);
  });

  it('throws InvalidURIError when user missing', () => {
    expect(() => parseConnectURI('hub=https://x.com&token=t')).toThrow(InvalidURIError);
  });

  it('throws InvalidURIError on invalid hub URL', () => {
    expect(() => parseConnectURI('hub=not-a-url&token=t&user=alice')).toThrow(InvalidURIError);
  });

  it('throws InvalidURIError on path-traversal username', () => {
    expect(() => parseConnectURI('hub=https://x.com&token=t&user=../etc')).toThrow(InvalidURIError);
  });
});

describe('sshHostname', () => {
  it('prepends username as subdomain', () => {
    expect(sshHostname('https://jupyter.example.com', 'alice'))
      .toBe('alice.jupyter.example.com');
  });

  it('handles hub URL with path', () => {
    expect(sshHostname('https://jupyter.example.com/hub', 'bob'))
      .toBe('bob.jupyter.example.com');
  });

  it('handles hub URL with port', () => {
    expect(sshHostname('https://jupyter.example.com:8443', 'carol'))
      .toBe('carol.jupyter.example.com');
  });
});
