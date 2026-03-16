// Package sshconfig writes and manages the JupyterHub SSH block in ~/.ssh/config.
//
// It mirrors the behavior of `coder config-ssh`, inserting a marked block that
// can be updated or removed on subsequent runs.
package sshconfig

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	startMarker = "# BEGIN JUPYTERHUB"
	endMarker   = "# END JUPYTERHUB"
)

// Block is the content written between the markers.
type Block struct {
	// HubHost is the SSH hostname users will connect to (e.g. "jupyter.example.com").
	// The wildcard pattern "*.jupyter.example.com" is also added for subdomain mode.
	HubHost string

	// BinaryPath is the absolute path to the jhub-ssh binary.
	BinaryPath string

	// TokenPath is where the token is stored on disk (read at ProxyCommand time).
	TokenPath string
}

// Generate returns the SSH config block as a string.
func (b *Block) Generate() string {
	var sb strings.Builder
	fmt.Fprintln(&sb, startMarker)
	fmt.Fprintf(&sb, "Host %s\n", b.HubHost)
	fmt.Fprintln(&sb, "  User jovyan")
	fmt.Fprintln(&sb, "  StrictHostKeyChecking no")
	fmt.Fprintln(&sb, "  UserKnownHostsFile /dev/null")
	fmt.Fprintf(&sb, "  ProxyCommand %s proxy-connect --hub https://%s --token-file %s %%r\n",
		b.BinaryPath, b.HubHost, b.TokenPath)
	fmt.Fprintln(&sb, "")
	fmt.Fprintf(&sb, "Host *.%s\n", b.HubHost)
	fmt.Fprintln(&sb, "  User jovyan")
	fmt.Fprintln(&sb, "  StrictHostKeyChecking no")
	fmt.Fprintln(&sb, "  UserKnownHostsFile /dev/null")
	fmt.Fprintf(&sb, "  ProxyCommand %s proxy-connect --hub https://%s --token-file %s %%r\n",
		b.BinaryPath, b.HubHost, b.TokenPath)
	fmt.Fprintln(&sb, endMarker)
	return sb.String()
}

// Write reads the existing ~/.ssh/config (if any), replaces or appends the
// JupyterHub block, and writes it back atomically.
func Write(configPath string, block *Block) error {
	// Ensure ~/.ssh exists
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		return fmt.Errorf("create ssh dir: %w", err)
	}

	var existing []byte
	if data, err := os.ReadFile(configPath); err == nil {
		existing = data
	}

	updated, err := upsertBlock(existing, block.Generate())
	if err != nil {
		return err
	}

	// Write atomically via a temp file
	tmp := configPath + ".jhub-tmp"
	if err := os.WriteFile(tmp, updated, 0o600); err != nil {
		return fmt.Errorf("write temp config: %w", err)
	}
	return os.Rename(tmp, configPath)
}

// Remove deletes the JupyterHub block from the SSH config.
func Remove(configPath string) error {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil // file doesn't exist — nothing to remove
	}

	result, err := removeBlock(data)
	if err != nil {
		return err
	}

	tmp := configPath + ".jhub-tmp"
	if err := os.WriteFile(tmp, result, 0o600); err != nil {
		return fmt.Errorf("write temp config: %w", err)
	}
	return os.Rename(tmp, configPath)
}

// upsertBlock replaces the existing JupyterHub block in existing config bytes,
// or appends it if not present.
func upsertBlock(existing []byte, newBlock string) ([]byte, error) {
	content := string(existing)
	start := strings.Index(content, startMarker)
	end := strings.Index(content, endMarker)

	switch {
	case start == -1 && end == -1:
		// No existing block — append
		if len(content) > 0 && !strings.HasSuffix(content, "\n") {
			content += "\n"
		}
		content += "\n" + newBlock + "\n"

	case start != -1 && end != -1 && start < end:
		// Replace existing block
		before := content[:start]
		after := content[end+len(endMarker):]
		// Trim leading newline from after
		after = strings.TrimPrefix(after, "\n")
		content = before + newBlock + "\n" + after

	default:
		return nil, fmt.Errorf("malformed JupyterHub block in SSH config (start=%d end=%d)", start, end)
	}

	return []byte(content), nil
}

// removeBlock strips the JupyterHub marked block from the config.
func removeBlock(data []byte) ([]byte, error) {
	content := string(data)
	start := strings.Index(content, startMarker)
	end := strings.Index(content, endMarker)

	if start == -1 {
		return data, nil // nothing to remove
	}
	if end == -1 || end < start {
		return nil, fmt.Errorf("malformed JupyterHub block in SSH config")
	}

	before := content[:start]
	after := content[end+len(endMarker):]
	after = strings.TrimPrefix(after, "\n")
	return []byte(before + after), nil
}

// DefaultConfigPath returns the default ~/.ssh/config path.
func DefaultConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join("/tmp", ".ssh", "config")
	}
	return filepath.Join(home, ".ssh", "config")
}

// DefaultTokenPath returns the path where jhub-ssh stores the API token.
func DefaultTokenPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "/tmp/.jhub-ssh-token"
	}
	return filepath.Join(home, ".config", "jhub-ssh", "token")
}
