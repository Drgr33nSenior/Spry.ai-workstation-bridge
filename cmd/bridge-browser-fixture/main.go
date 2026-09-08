//go:build bridge_browser_fixture

// bridge-browser-fixture is test-only infrastructure for the real-browser
// regression. It constructs a live API policy over the isolated Demo adapter;
// it is excluded from ordinary builds and cannot select that adapter in bridged.
package main

import (
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/adapters"
	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/api"
	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/auth"
	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/config"
	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/domain"
	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/engine"
	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/safefile"
	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/source"
	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/store"
)

func main() {
	if err := run(); err != nil {
		_, _ = os.Stderr.WriteString("bridge-browser-fixture: " + err.Error() + "\n")
		os.Exit(1)
	}
}

func run() error {
	stateDir := flag.String("state", "", "isolated test state directory")
	listen := flag.String("listen", "", "loopback listener")
	origin := flag.String("origin", "", "HTTPS test origin")
	certPath := flag.String("cert", "", "ephemeral test certificate")
	keyPath := flag.String("key", "", "ephemeral test key")
	credentialPath := flag.String("credential", "", "owner-only test credential output")
	flag.Parse()
	if flag.NArg() != 0 || *stateDir == "" || *listen == "" || *origin == "" || *certPath == "" || *keyPath == "" || *credentialPath == "" {
		return errors.New("state, listen, origin, cert, key and credential are required")
	}
	u, err := url.Parse(*origin)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return errors.New("origin must be an HTTPS test origin")
	}
	if host, _, err := net.SplitHostPort(*listen); err != nil || !net.ParseIP(host).IsLoopback() {
		return errors.New("fixture must bind an explicit loopback IP and port")
	}
	if err := os.MkdirAll(*stateDir, 0700); err != nil {
		return err
	}
	if err := os.Chmod(*stateDir, 0700); err != nil {
		return err
	}
	db, err := store.Open(*stateDir, "demo")
	if err != nil {
		return err
	}
	defer db.Close()
	src := source.New(filepath.Join(*stateDir, "managed-source.json"))
	if _, err = src.Read(); err != nil {
		initial := domain.Configuration{Serving: domain.Serving{Model: "Qwen3.5-9B", Context: 4096, Concurrency: 2, MemoryFraction: .8}, Resources: domain.Resources{CPU: 20, MemoryMiB: 32768, SharedMemoryMiB: 16384, GPUCount: 1}, Caches: domain.CacheBudgets{ModelsGiB: 128, CompilerGiB: 100, ShaderGiB: 16, BuildJobs: 8, BuildMemoryMiB: 32768, ScratchGiB: 64}}
		if err = src.Initialize(initial); err != nil {
			return err
		}
	}
	if _, err = os.Lstat(*credentialPath); os.IsNotExist(err) {
		credential, token, err := auth.NewCredential("browser-fixture-owner", "owner", time.Hour)
		if err != nil {
			return err
		}
		if err = db.Update(func(s *store.State) error { s.Credentials[credential.ID] = credential; return nil }); err != nil {
			return err
		}
		if err = safefile.CreateSecret(*credentialPath, []byte(token+"\n"), os.Geteuid()); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	adapter, err := adapters.NewDemo(filepath.Join(*stateDir, "demo-executor"), "demo-workstation")
	if err != nil {
		return err
	}
	eng := engine.New(db, src, adapter, "demo-workstation", 4, 10*time.Second)
	if err = eng.Start(); err != nil {
		return err
	}
	defer eng.Close(context.Background())
	c := config.Config{Mode: "live", BrowserSessions: true, Target: "demo-workstation", AllowedHosts: []string{u.Host}, ExternalURL: *origin}
	pair, err := tls.LoadX509KeyPair(*certPath, *keyPath)
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", *listen)
	if err != nil {
		return err
	}
	defer listener.Close()
	server := &http.Server{Handler: api.New(c, eng, nil).Handler(), TLSConfig: &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{pair}}, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 35 * time.Second, WriteTimeout: 40 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16384}
	// SIGUSR1 is deliberately fixture-only. It expires synthetic server-side
	// sessions so the browser regression can exercise expiry without a test API
	// route, a production flag, or a shortened production session TTL.
	expire := make(chan os.Signal, 1)
	signal.Notify(expire, syscall.SIGUSR1)
	defer signal.Stop(expire)
	go func() {
		for range expire {
			_ = db.Update(func(s *store.State) error {
				for id, session := range s.Sessions {
					session.ExpiresAt = time.Now().Add(-time.Second)
					s.Sessions[id] = session
				}
				return nil
			})
		}
	}()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	errCh := make(chan error, 1)
	go func() { errCh <- server.ServeTLS(listener, "", "") }()
	select {
	case err = <-errCh:
	case <-ctx.Done():
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = server.Shutdown(shutdown)
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
