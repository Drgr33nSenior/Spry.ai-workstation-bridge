package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/adapters"
	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/admin"
	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/api"
	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/config"
	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/domain"
	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/engine"
	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/safefile"
	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/source"
	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/store"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "bridged:", err)
		os.Exit(1)
	}
}
func defaults() domain.Configuration {
	return domain.Configuration{Serving: domain.Serving{Model: "Qwen3.5-9B", Context: 4096, Concurrency: 2, MemoryFraction: 0.8}, Resources: domain.Resources{CPU: 20, MemoryMiB: 32768, SharedMemoryMiB: 16384, GPUCount: 1}, Caches: domain.CacheBudgets{ModelsGiB: 128, CompilerGiB: 100, ShaderGiB: 16, BuildJobs: 8, BuildMemoryMiB: 32768, ScratchGiB: 64}}
}
func run() error {
	policy := flag.String("config", "", "absolute service policy path")
	initDir := flag.String("init-demo", "", "create an isolated demo configuration; does not start a listener")
	port := flag.Int("demo-port", 8743, "loopback demo port for --init-demo")
	importPath := flag.String("import-source", "", "initial offline import of a reviewed management configuration; service must be stopped")
	flag.Parse()
	if flag.NArg() != 0 {
		return errors.New("unexpected arguments")
	}
	if *initDir != "" {
		if *policy != "" || *importPath != "" {
			return errors.New("init-demo cannot combine with live policy/import")
		}
		return initDemo(*initDir, *port)
	}
	if *policy == "" {
		return errors.New("use --config FILE or --init-demo DIRECTORY")
	}
	c, err := config.Load(*policy)
	if err != nil {
		return err
	}
	if *importPath != "" {
		if !admin.Authorized(os.Geteuid(), c.OwnerUID) {
			return errors.New("source import requires root or explicit owner UID")
		}
		db, err := store.Open(c.StateDir, c.Mode)
		if err != nil {
			return err
		}
		defer db.Close()
		b, err := safefile.Read(*importPath, 1<<20)
		if err != nil {
			return err
		}
		var current domain.Configuration
		if err = config.Decode(b, &current); err != nil {
			return err
		}
		if err = source.New(c.SourcePath).Initialize(current); err != nil {
			return err
		}
		fmt.Println("Initial source imported. Review a plan before live apply.")
		return nil
	}
	if c.Mode == "live" && os.Geteuid() == 0 {
		return errors.New("management server must run unprivileged")
	}
	db, err := store.Open(c.StateDir, c.Mode)
	if err != nil {
		return err
	}
	defer db.Close()
	src := source.New(c.SourcePath)
	current, err := src.Read()
	if err != nil {
		return err
	}
	var adapter domain.Adapter
	if c.Mode == "demo" {
		adapter, err = adapters.NewDemo(filepath.Join(c.StateDir, "demo-executor"), c.Target)
	} else {
		adapter, err = adapters.NewLive(adapters.LiveOptions{StateDir: c.StateDir, Target: c.Target, Environment: c.Environment, ReferenceRoot: c.ReferenceRoot, ModelRoot: c.ModelRoot, SourcePath: c.SourcePath, HostSocket: c.HostSocket, WorkerSocket: c.WorkerSocket, ClientBaseURL: c.ClientBaseURL, CompilerCacheRoot: c.CompilerCacheRoot, ShaderCacheRoot: c.ShaderCacheRoot, ModelBudgetBytes: current.Caches.ModelsGiB << 30})
	}
	if err != nil {
		return err
	}
	eng := engine.New(db, src, adapter, c.Target, c.QueueDepth, time.Duration(c.OperationTimeoutSeconds)*time.Second)
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	app := api.New(c, eng, logger)
	server := &http.Server{Addr: c.Listen, Handler: app.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 35 * time.Second, WriteTimeout: 40 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16384, TLSConfig: &tls.Config{MinVersion: tls.VersionTLS13}}
	if c.TLSCertFile != "" {
		pair, e := tls.LoadX509KeyPair(c.TLSCertFile, c.TLSKeyFile)
		if e != nil {
			return errors.New("TLS certificate/key cannot be loaded")
		}
		leaf, e := x509.ParseCertificate(pair.Certificate[0])
		if e != nil {
			return errors.New("TLS certificate invalid")
		}
		origin, _ := url.Parse(c.ExternalURL)
		if e = leaf.VerifyHostname(origin.Hostname()); e != nil {
			return errors.New("TLS certificate does not identify external_url host")
		}
		if time.Now().Before(leaf.NotBefore) || time.Now().After(leaf.NotAfter) {
			return errors.New("TLS certificate not currently valid")
		}
		server.TLSConfig.Certificates = []tls.Certificate{pair}
	}
	listener, err := net.Listen("tcp", c.Listen)
	if err != nil {
		return errors.New("management listener unavailable; no queued work dispatched")
	}
	defer listener.Close()
	if err = eng.Start(); err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	serveErr := make(chan error, 1)
	go func() {
		logger.Info("management listener starting", "mode", c.Mode, "target", c.Target, "listen", c.Listen)
		if c.TLSCertFile != "" {
			serveErr <- server.ServeTLS(listener, "", "")
		} else {
			serveErr <- server.Serve(listener)
		}
	}()
	select {
	case err = <-serveErr:
	case <-ctx.Done():
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_ = server.Shutdown(shutdown)
	engineErr := eng.Close(shutdown)
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return engineErr
}
func initDemo(dir string, port int) error {
	if port < 1024 || port > 65535 {
		return errors.New("demo port must be 1024..65535")
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	if err = safefile.CheckPath(abs); err != nil {
		return err
	}
	if _, err = os.Lstat(abs); err == nil {
		return errors.New("demo directory exists; choose a new directory")
	}
	if err = os.Mkdir(abs, 0700); err != nil {
		return err
	}
	stateDir := filepath.Join(abs, "state")
	if err = os.Mkdir(stateDir, 0700); err != nil {
		return err
	}
	listen := "127.0.0.1:" + strconv.Itoa(port)
	c := config.Config{Mode: "demo", StateDir: stateDir, SourcePath: filepath.Join(stateDir, "managed-source.json"), Listen: listen, AllowedHosts: []string{listen, "localhost:" + strconv.Itoa(port)}, ExternalURL: "http://" + listen, OwnerUID: os.Geteuid(), Target: "demo-workstation", Environment: "dev", QueueDepth: 4, OperationTimeoutSeconds: 300, ClientBaseURL: "http://127.0.0.1:18000/v1"}
	b, _ := json.MarshalIndent(c, "", "  ")
	if err = safefile.CreateSecret(filepath.Join(abs, "bridge.json"), append(b, '\n'), -1); err != nil {
		return err
	}
	if err = source.New(c.SourcePath).Initialize(defaults()); err != nil {
		return err
	}
	clientContext := map[string]any{"endpoint": c.ExternalURL, "credential_file": filepath.Join(abs, "owner.token"), "timeout_seconds": 30}
	b, _ = json.MarshalIndent(clientContext, "", "  ")
	if err = safefile.CreateSecret(filepath.Join(abs, "context.json"), append(b, '\n'), -1); err != nil {
		return err
	}
	fmt.Printf("DEMO ONLY initialized at %s\nBootstrap an owner using bridgectl admin bootstrap, then start with bridged --config.\n", abs)
	return nil
}
