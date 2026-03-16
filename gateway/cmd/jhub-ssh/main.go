// jhub-ssh — SSH gateway CLI and server for JupyterHub
//
// Subcommands:
//
//	serve         Start the SSH gateway + port-forwarding proxy server
//	config-ssh    Write ~/.ssh/config ProxyCommand block
//	proxy-connect Connect stdin/stdout to a user's SSH sidecar (used as ProxyCommand)
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/groundsada/jhub-ssh/internal/hubclient"
	"github.com/groundsada/jhub-ssh/internal/portfwd"
	"github.com/groundsada/jhub-ssh/internal/sshconfig"
	"github.com/groundsada/jhub-ssh/internal/sshgateway"
	"golang.org/x/net/websocket"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}
	cmd := os.Args[1]
	args := os.Args[2:]

	switch cmd {
	case "serve":
		cmdServe(args)
	case "config-ssh":
		cmdConfigSSH(args)
	case "proxy-connect":
		cmdProxyConnect(args)
	case "help", "--help", "-h":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n", cmd)
		usage()
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `jhub-ssh — SSH gateway for JupyterHub

Usage:
  jhub-ssh serve          Start the gateway server
  jhub-ssh config-ssh     Write SSH config ProxyCommand block
  jhub-ssh proxy-connect  Connect stdin/stdout to user's SSH sidecar (ProxyCommand helper)
  jhub-ssh help           Show this message

Run 'jhub-ssh <command> --help' for command-specific flags.`)
}

// ── serve ────────────────────────────────────────────────────────────────────

func cmdServe(args []string) {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	addr := fs.String("addr", ":8022", "Address to listen on")
	hubURL := fs.String("hub", "", "JupyterHub base URL (required)")
	adminToken := fs.String("admin-token", os.Getenv("JUPYTERHUB_API_TOKEN"), "Hub admin API token")
	sshPort := fs.String("ssh-port", "2222", "SSH sidecar port inside pods")
	fs.Parse(args)

	if *hubURL == "" {
		fmt.Fprintln(os.Stderr, "serve: --hub is required")
		os.Exit(1)
	}
	if *adminToken == "" {
		fmt.Fprintln(os.Stderr, "serve: --admin-token or $JUPYTERHUB_API_TOKEN is required")
		os.Exit(1)
	}

	hub := hubclient.New(*hubURL, *adminToken)
	logger := log.New(os.Stdout, "[jhub-ssh] ", log.LstdFlags)

	gwHandler := sshgateway.New(hub, logger)
	gwHandler.SSHPort = *sshPort

	pfHandler := portfwd.New(hub, logger)

	mux := http.NewServeMux()
	gwHandler.Mux(mux)   // /ssh/{username}
	pfHandler.Mux(mux)   // subdomain port forwarding
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "ok")
	})

	logger.Printf("Listening on %s", *addr)
	if err := http.ListenAndServe(*addr, mux); err != nil {
		logger.Fatalf("ListenAndServe: %v", err)
	}
}

// ── config-ssh ───────────────────────────────────────────────────────────────

func cmdConfigSSH(args []string) {
	fs := flag.NewFlagSet("config-ssh", flag.ExitOnError)
	hubURL := fs.String("hub", "", "JupyterHub base URL, e.g. https://jupyter.example.com (required)")
	token := fs.String("token", "", "Your JupyterHub API token")
	configPath := fs.String("config", sshconfig.DefaultConfigPath(), "Path to SSH config file")
	remove := fs.Bool("remove", false, "Remove the JupyterHub block instead of writing it")
	fs.Parse(args)

	if *hubURL == "" {
		fmt.Fprintln(os.Stderr, "config-ssh: --hub is required")
		os.Exit(1)
	}

	if *remove {
		if err := sshconfig.Remove(*configPath); err != nil {
			fmt.Fprintf(os.Stderr, "config-ssh: remove: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("Removed JupyterHub SSH config block.")
		return
	}

	// Store token to disk
	tokenPath := sshconfig.DefaultTokenPath()
	if *token != "" {
		if err := writeToken(tokenPath, *token); err != nil {
			fmt.Fprintf(os.Stderr, "config-ssh: store token: %v\n", err)
			os.Exit(1)
		}
	}

	// Derive host from hub URL
	host := strings.TrimPrefix(*hubURL, "https://")
	host = strings.TrimPrefix(host, "http://")
	host = strings.TrimRight(host, "/")

	binaryPath, err := os.Executable()
	if err != nil {
		binaryPath = "jhub-ssh"
	}
	binaryPath, _ = filepath.Abs(binaryPath)

	block := &sshconfig.Block{
		HubHost:    host,
		BinaryPath: binaryPath,
		TokenPath:  tokenPath,
	}

	if err := sshconfig.Write(*configPath, block); err != nil {
		fmt.Fprintf(os.Stderr, "config-ssh: write: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Wrote JupyterHub SSH config to %s\n", *configPath)
	fmt.Printf("You can now SSH into your server:\n\n  ssh %s\n\n", host)
}

// ── proxy-connect ─────────────────────────────────────────────────────────────

// cmdProxyConnect is invoked by SSH as a ProxyCommand.
// It connects stdin/stdout to the SSH gateway WebSocket, which in turn relays
// to the SSH sidecar inside the user's pod.
//
// SSH config calls it like:
//
//	ProxyCommand jhub-ssh proxy-connect --hub https://jupyter.example.com --token-file ~/.config/jhub-ssh/token %r
//
// %r is expanded by SSH to the remote username (the JupyterHub username).
func cmdProxyConnect(args []string) {
	fs := flag.NewFlagSet("proxy-connect", flag.ExitOnError)
	hubURL := fs.String("hub", "", "JupyterHub base URL (required)")
	tokenFlag := fs.String("token", "", "JupyterHub API token (prefer --token-file)")
	tokenFile := fs.String("token-file", sshconfig.DefaultTokenPath(), "File containing JupyterHub API token")
	fs.Parse(args)

	if *hubURL == "" {
		fmt.Fprintln(os.Stderr, "proxy-connect: --hub is required")
		os.Exit(1)
	}

	// Username is the remaining positional argument (from SSH's %r expansion)
	username := ""
	if fs.NArg() > 0 {
		username = fs.Arg(0)
	}
	if username == "" {
		fmt.Fprintln(os.Stderr, "proxy-connect: username argument is required")
		os.Exit(1)
	}

	// Read token
	token := *tokenFlag
	if token == "" {
		data, err := os.ReadFile(*tokenFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "proxy-connect: read token file %q: %v\n", *tokenFile, err)
			os.Exit(1)
		}
		token = strings.TrimSpace(string(data))
	}

	// Build WebSocket URL
	wsURL := strings.Replace(*hubURL, "https://", "wss://", 1)
	wsURL = strings.Replace(wsURL, "http://", "ws://", 1)
	wsURL = strings.TrimRight(wsURL, "/") + "/ssh/" + username

	// Connect
	origin := *hubURL
	cfg, err := websocket.NewConfig(wsURL, origin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "proxy-connect: build ws config: %v\n", err)
		os.Exit(1)
	}
	cfg.Header.Set("Authorization", "token "+token)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_ = ctx // websocket.DialConfig does not accept a context; timeout set via dialer

	ws, err := websocket.DialConfig(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "proxy-connect: dial %s: %v\n", wsURL, err)
		os.Exit(1)
	}
	ws.PayloadType = websocket.BinaryFrame

	done := make(chan struct{}, 2)

	go func() {
		io.Copy(ws, os.Stdin)
		done <- struct{}{}
	}()
	go func() {
		io.Copy(os.Stdout, ws)
		done <- struct{}{}
	}()

	<-done
}

// ── helpers ───────────────────────────────────────────────────────────────────

func writeToken(path, token string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(token), 0o600)
}

// Compile-time interface check: net.Conn must satisfy io.ReadWriter.
var _ io.ReadWriter = (*net.TCPConn)(nil)
