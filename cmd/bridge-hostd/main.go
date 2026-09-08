package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/domain"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/hostexec"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "bridge-hostd:", err)
		os.Exit(1)
	}
}
func run() error {
	policy := flag.String("policy", "/etc/bridge/host-policy.json", "root-owned installed executor policy")
	manifest := flag.String("manifest", "", "offline: print hashes for a prepared runtime bundle; does not install")
	systemManifest := flag.Bool("system-manifest", false, "offline on target: print fixed system executable hashes without running them")
	hashConfiguration := flag.String("hash-configuration", "", "offline: print qualification key for an exported management configuration")
	hashTemplate := flag.String("hash-template", "", "offline: print Pod template hash for an exported Deployment")
	hashConfigMap := flag.String("hash-configmap", "", "offline: print content hash for a reviewed exported ConfigMap")
	previewServing := flag.String("preview-serving", "", "offline: render fixed serving candidate from management configuration and draft policy")
	deployment := flag.String("deployment", "", "offline: reviewed exported Deployment input for --preview-serving")
	flag.Parse()
	if flag.NArg() != 0 {
		return fmt.Errorf("unexpected arguments")
	}
	if *manifest != "" {
		m, err := hostexec.ArtifactManifest(*manifest)
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(m)
	}
	if *systemManifest {
		m, err := hostexec.SystemManifest()
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(m)
	}
	if *hashConfiguration != "" {
		f, err := os.Open(*hashConfiguration)
		if err != nil {
			return err
		}
		defer f.Close()
		var c domain.Configuration
		d := json.NewDecoder(io.LimitReader(f, 1<<20))
		d.DisallowUnknownFields()
		if err = d.Decode(&c); err != nil {
			return err
		}
		fmt.Fprintln(os.Stdout, hostexec.ConfigurationHash(&c.Serving, &c.Resources))
		return nil
	}
	if *hashTemplate != "" {
		f, err := os.Open(*hashTemplate)
		if err != nil {
			return err
		}
		defer f.Close()
		var d struct {
			Spec struct {
				Template json.RawMessage `json:"template"`
			} `json:"spec"`
		}
		if err = json.NewDecoder(io.LimitReader(f, 4<<20)).Decode(&d); err != nil {
			return err
		}
		if len(d.Spec.Template) == 0 {
			return fmt.Errorf("Deployment template is absent")
		}
		var template map[string]any
		if err = json.Unmarshal(d.Spec.Template, &template); err != nil {
			return err
		}
		fmt.Fprintln(os.Stdout, domain.Hash(template))
		return nil
	}
	if *hashConfigMap != "" {
		f, err := os.Open(*hashConfigMap)
		if err != nil {
			return err
		}
		defer f.Close()
		var m map[string]any
		if err = json.NewDecoder(io.LimitReader(f, 4<<20)).Decode(&m); err != nil {
			return err
		}
		if m["kind"] != "ConfigMap" {
			return fmt.Errorf("expected ConfigMap export")
		}
		fmt.Fprintln(os.Stdout, domain.Hash(map[string]any{"data": m["data"], "binaryData": m["binaryData"]}))
		return nil
	}
	if *previewServing != "" {
		var p hostexec.Policy
		var c domain.Configuration
		var current map[string]any
		for path, out := range map[string]any{*policy: &p, *previewServing: &c, *deployment: &current} {
			if path == "" {
				return fmt.Errorf("preview requires policy, management configuration and exported deployment")
			}
			f, err := os.Open(path)
			if err != nil {
				return err
			}
			d := json.NewDecoder(io.LimitReader(f, 4<<20))
			d.DisallowUnknownFields()
			err = d.Decode(out)
			f.Close()
			if err != nil {
				return err
			}
		}
		candidate, err := hostexec.PreviewServing(p, current, c)
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(candidate)
	}
	p, err := hostexec.LoadPolicy(*policy)
	if err != nil {
		return err
	}
	e, err := hostexec.NewExecutor(p)
	if err != nil {
		return err
	}
	listener, err := hostexec.Listen(p)
	if err != nil {
		return err
	}
	defer listener.Close()
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	return hostexec.Serve(ctx, listener, e)
}
