package main

import (
	"flag"
	"fmt"
	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/worker"
	"net"
	"net/http"
	"os"
	"strconv"
	"time"
)

func main() {
	policy := flag.String("policy", "/etc/bridge/worker-policy.json", "root-owned installed worker policy")
	flag.Parse()
	p, e := worker.LoadPolicy(*policy)
	if e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
	s, e := worker.New(p)
	if e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
	var l net.Listener
	activated := os.Getenv("LISTEN_PID") == strconv.Itoa(os.Getpid()) && os.Getenv("LISTEN_FDS") == "1"
	if activated {
		f := os.NewFile(3, "systemd-worker-socket")
		l, e = net.FileListener(f)
		f.Close()
		if e == nil && l.Addr().String() != p.Socket {
			l.Close()
			e = fmt.Errorf("activated socket differs from root policy")
		}
	} else {
		l, e = net.Listen("unix", p.Socket)
	}
	if e != nil {
		fmt.Fprintln(os.Stderr, "worker socket unavailable")
		os.Exit(1)
	}
	defer l.Close()
	if !activated {
		e = os.Chmod(p.Socket, 0660)
	}
	if e != nil {
		fmt.Fprintln(os.Stderr, "worker socket permissions failed")
		os.Exit(1)
	}
	server := &http.Server{Handler: s.Handler(), ConnContext: worker.ConnContext, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 8192}
	if e = server.Serve(l); e != nil && e != http.ErrServerClosed {
		fmt.Fprintln(os.Stderr, "worker listener failed")
		os.Exit(1)
	}
}
