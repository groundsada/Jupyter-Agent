package sshconfig

import (
	"strings"
	"testing"
)

var testBlock = &Block{
	HubHost:    "jupyter.example.com",
	BinaryPath: "/usr/local/bin/jhub-ssh",
	TokenPath:  "/home/user/.config/jhub-ssh/token",
}

func TestGenerateContainsMarkers(t *testing.T) {
	out := testBlock.Generate()
	if !strings.Contains(out, startMarker) {
		t.Errorf("Generate() missing start marker")
	}
	if !strings.Contains(out, endMarker) {
		t.Errorf("Generate() missing end marker")
	}
}

func TestGenerateContainsProxyCommand(t *testing.T) {
	out := testBlock.Generate()
	if !strings.Contains(out, "ProxyCommand") {
		t.Errorf("Generate() missing ProxyCommand")
	}
	if !strings.Contains(out, "jhub-ssh proxy-connect") {
		t.Errorf("Generate() missing proxy-connect subcommand")
	}
}

func TestGenerateContainsWildcardHost(t *testing.T) {
	out := testBlock.Generate()
	if !strings.Contains(out, "Host *.jupyter.example.com") {
		t.Errorf("Generate() missing wildcard Host entry")
	}
}

func TestUpsertBlockAppendWhenEmpty(t *testing.T) {
	result, err := upsertBlock([]byte{}, testBlock.Generate())
	if err != nil {
		t.Fatal(err)
	}
	s := string(result)
	if !strings.Contains(s, startMarker) {
		t.Errorf("upsertBlock on empty: missing start marker")
	}
}

func TestUpsertBlockAppendToExistingContent(t *testing.T) {
	existing := "Host myserver\n  User alice\n"
	result, err := upsertBlock([]byte(existing), testBlock.Generate())
	if err != nil {
		t.Fatal(err)
	}
	s := string(result)
	if !strings.Contains(s, "Host myserver") {
		t.Errorf("existing content lost after upsert")
	}
	if !strings.Contains(s, startMarker) {
		t.Errorf("upsertBlock: missing JupyterHub block after append")
	}
}

func TestUpsertBlockReplacesExistingBlock(t *testing.T) {
	oldBlock := "# BEGIN JUPYTERHUB\nHost old.example.com\n# END JUPYTERHUB\n"
	result, err := upsertBlock([]byte(oldBlock), testBlock.Generate())
	if err != nil {
		t.Fatal(err)
	}
	s := string(result)
	if strings.Contains(s, "old.example.com") {
		t.Errorf("old block content still present after replace")
	}
	if !strings.Contains(s, "jupyter.example.com") {
		t.Errorf("new block content missing after replace")
	}
}

func TestUpsertBlockPreservesContentAfterBlock(t *testing.T) {
	existing := "# BEGIN JUPYTERHUB\nHost old\n# END JUPYTERHUB\nHost other\n  User bob\n"
	result, err := upsertBlock([]byte(existing), testBlock.Generate())
	if err != nil {
		t.Fatal(err)
	}
	s := string(result)
	if !strings.Contains(s, "Host other") {
		t.Errorf("content after block lost during replace")
	}
}

func TestRemoveBlockStripsBlock(t *testing.T) {
	content := "Host pre\n\n# BEGIN JUPYTERHUB\nHost jh\n# END JUPYTERHUB\n\nHost post\n"
	result, err := removeBlock([]byte(content))
	if err != nil {
		t.Fatal(err)
	}
	s := string(result)
	if strings.Contains(s, startMarker) || strings.Contains(s, endMarker) {
		t.Errorf("markers still present after remove")
	}
	if !strings.Contains(s, "Host pre") || !strings.Contains(s, "Host post") {
		t.Errorf("surrounding content lost after remove")
	}
}

func TestRemoveBlockNoOpWhenAbsent(t *testing.T) {
	content := "Host myserver\n  User alice\n"
	result, err := removeBlock([]byte(content))
	if err != nil {
		t.Fatal(err)
	}
	if string(result) != content {
		t.Errorf("removeBlock modified content when no block present")
	}
}

func TestUpsertBlockErrorOnMalformed(t *testing.T) {
	// End marker without start marker
	malformed := "# END JUPYTERHUB\n"
	_, err := upsertBlock([]byte(malformed), testBlock.Generate())
	if err == nil {
		t.Errorf("expected error for malformed block (end without start)")
	}
}
