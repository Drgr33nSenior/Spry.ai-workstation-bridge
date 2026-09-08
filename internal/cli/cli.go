// Package cli implements bridgectl without implicit privileged fallback.
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/admin"
	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/client"
	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/domain"
)

const usage = `Usage: bridgectl [--context FILE] [--endpoint ORIGIN] [--credential-file FILE] [--ca-file FILE] [--deadline 30s] [--json] COMMAND

Read: status | models | config | resources | profiles | builds | caches | harnesses | operations [ID]
Source: export-source --output NEW_FILE
Artifact: artifact OPERATION_ID --name ARTIFACT_NAME --output NEW_FILE
Plan: plan --file DRAFT.json
Apply: apply --plan ID --target EXACT_TARGET --idempotency-key KEY
Inspect: wait ID | cancel ID | recover ID
Clients: harness export NAME --output FILE
         harness configure NAME --directory NEW_DIRECTORY [--bundle FILE]
         harness launch NAME --directory DIRECTORY [--mode cli|acp]
Local administration (service stopped):
  admin bootstrap|recover --config FILE --output NEW_CREDENTIAL_FILE [--name NAME] [--ttl 24h]
  admin issue --config FILE --output NEW_CREDENTIAL_FILE --role owner|operator|viewer --name NAME [--ttl 24h]
  admin list --config FILE
  admin revoke --config FILE --name CREDENTIAL_ID

Global flags precede the command. Credentials are read from owner-only files, never command arguments.
Plans use the same typed JSON contract and server validation as the web UI. Apply requires the exact target.
Exit codes: 0 success; 2 usage/validation; 3 authentication/authorization; 4 conflict; 5 unavailable/transport;
            6 failed/cancelled operation; 7 recovery required; 8 deadline exceeded.
`

func Run(args []string, out, errout io.Writer) int {
	global := flag.NewFlagSet("bridgectl", flag.ContinueOnError)
	global.SetOutput(io.Discard)
	contextPath := global.String("context", "", "management context JSON")
	endpoint := global.String("endpoint", "", "management HTTP(S) origin")
	credentialFile := global.String("credential-file", "", "owner-only credential file")
	ca := global.String("ca-file", "", "additional trusted management CA")
	deadline := global.Duration("deadline", 30*time.Second, "request or wait deadline")
	machine := global.Bool("json", false, "machine-readable JSON (all commands)")
	_ = machine
	if err := global.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprint(out, usage)
			return 0
		}
		return failure(out, 2, "usage", err.Error())
	}
	rest := global.Args()
	if len(rest) == 0 || rest[0] == "help" || rest[0] == "--help" {
		fmt.Fprint(out, usage)
		return 0
	}
	if *deadline < time.Millisecond || *deadline > 24*time.Hour {
		return failure(out, 2, "invalid", "deadline must be between 1ms and 24h")
	}
	if rest[0] == "admin" {
		return runAdmin(rest[1:], out)
	}
	if rest[0] == "harness" && len(rest) > 1 && (rest[1] == "launch" || rest[1] == "configure") {
		if rest[1] == "launch" || containsFlag(rest, "--bundle") {
			return runLocalHarness(rest[1:], out, errout)
		}
	}
	cfg := client.Config{Endpoint: "http://127.0.0.1:8080"}
	if *contextPath != "" {
		var err error
		cfg, err = client.LoadConfig(*contextPath)
		if err != nil {
			return report(out, err)
		}
	}
	if *endpoint != "" {
		cfg.Endpoint = *endpoint
	}
	if *credentialFile != "" {
		cfg.CredentialFile = *credentialFile
	}
	if *ca != "" {
		cfg.CAFile = *ca
	}
	c, err := client.New(cfg)
	if err != nil {
		return report(out, err)
	}
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), *deadline)
	defer cancel()
	command := rest[0]
	rest = rest[1:]
	switch command {
	case "artifact":
		if len(rest) == 0 || !validID(rest[0]) {
			return failure(out, 2, "usage", "artifact requires an operation ID")
		}
		operationID := rest[0]
		f := flags("artifact")
		name := f.String("name", "", "exact artifact name")
		path := f.String("output", "", "new local artifact output")
		if f.Parse(rest[1:]) != nil || f.NArg() != 0 || *name == "" || *path == "" {
			return failure(out, 2, "usage", "artifact requires --name ARTIFACT_NAME --output NEW_FILE")
		}
		var operation domain.Operation
		if err := c.Do(ctx, "GET", "/api/v1/operations/"+operationID, "", nil, &operation); err != nil {
			return report(out, err)
		}
		for _, artifact := range operation.Artifacts {
			if artifact.Name == *name {
				if err := client.WriteArtifact(artifact, *path); err != nil {
					return report(out, err)
				}
				return output(out, map[string]string{"status": "exported", "output": *path, "sha256": artifact.SHA256})
			}
		}
		return failure(out, 2, "not_found", "operation does not contain that artifact")
	case "export-source":
		f := flags("export-source")
		path := f.String("output", "", "new managed source export file")
		if f.Parse(rest) != nil || f.NArg() != 0 || *path == "" {
			return failure(out, 2, "usage", "export-source requires --output NEW_FILE")
		}
		var cfg domain.Configuration
		if err := c.Do(ctx, "GET", "/api/v1/config/export", "", nil, &cfg); err != nil {
			return report(out, err)
		}
		if cfg.Revision != cfg.ContentRevision() {
			return failure(out, 2, "integrity", "source export revision does not match its content")
		}
		b, err := json.MarshalIndent(cfg, "", "  ")
		if err != nil {
			return report(out, err)
		}
		if err := client.WriteNewFile(*path, append(b, '\n')); err != nil {
			return report(out, err)
		}
		return output(out, map[string]string{"status": "exported", "output": *path, "revision": cfg.Revision})
	case "status", "models", "config", "resources", "profiles", "builds", "caches", "harnesses", "operations":
		path := "/api/v1/" + command
		if command == "operations" && len(rest) == 1 {
			if !validID(rest[0]) {
				return failure(out, 2, "invalid", "operation ID must be 32 lowercase hex characters")
			}
			path += "/" + rest[0]
		} else if len(rest) != 0 {
			return failure(out, 2, "usage", "unexpected command arguments")
		}
		var result json.RawMessage
		if err := c.Do(ctx, "GET", path, "", nil, &result); err != nil {
			return report(out, err)
		}
		return output(out, result)
	case "plan":
		flags := flags("plan")
		file := flags.String("file", "", "typed draft JSON")
		if flags.Parse(rest) != nil || flags.NArg() != 0 || *file == "" {
			return failure(out, 2, "usage", "plan requires --file DRAFT.json")
		}
		var draft domain.Draft
		if err := readJSON(*file, &draft); err != nil {
			return failure(out, 2, "invalid", err.Error())
		}
		var plan domain.Plan
		if err := c.Do(ctx, "POST", "/api/v1/plans", "", draft, &plan); err != nil {
			return report(out, err)
		}
		return output(out, plan)
	case "apply":
		flags := flags("apply")
		id := flags.String("plan", "", "validated plan ID")
		target := flags.String("target", "", "exact target")
		key := flags.String("idempotency-key", "", "stable operation key")
		if flags.Parse(rest) != nil || flags.NArg() != 0 || !validID(*id) || *target == "" || *key == "" {
			return failure(out, 2, "usage", "apply requires --plan ID --target EXACT_TARGET --idempotency-key KEY")
		}
		var operation domain.Operation
		if err := c.Do(ctx, "POST", "/api/v1/operations", *key, map[string]string{"plan_id": *id, "target": *target}, &operation); err != nil {
			return report(out, err)
		}
		return output(out, operation)
	case "wait", "cancel", "recover":
		if len(rest) != 1 || !validID(rest[0]) {
			return failure(out, 2, "usage", command+" requires an operation ID")
		}
		path := "/api/v1/operations/" + rest[0]
		if command == "wait" {
			return wait(ctx, c, path, out)
		}
		var result json.RawMessage
		if command == "cancel" {
			var current domain.Operation
			if err := c.Do(ctx, "GET", path, "", nil, &current); err != nil {
				return report(out, err)
			}
			if err := c.DoRevision(ctx, "POST", path+"/cancel", current.Revision, map[string]any{}, &result); err != nil {
				return report(out, err)
			}
			return output(out, result)
		}
		if err := c.Do(ctx, "POST", path+"/"+command, "", map[string]any{}, &result); err != nil {
			return report(out, err)
		}
		return output(out, result)
	case "harness":
		return runRemoteHarness(ctx, c, rest, out)
	default:
		return failure(out, 2, "usage", "unknown command; run bridgectl help")
	}
}

func flags(name string) *flag.FlagSet {
	f := flag.NewFlagSet(name, flag.ContinueOnError)
	f.SetOutput(io.Discard)
	return f
}
func validID(id string) bool { return regexp.MustCompile(`^[a-f0-9]{32}$`).MatchString(id) }
func containsFlag(args []string, want string) bool {
	for _, a := range args {
		if a == want || strings.HasPrefix(a, want+"=") {
			return true
		}
	}
	return false
}
func output(w io.Writer, v any) int {
	e := json.NewEncoder(w)
	e.SetIndent("", "  ")
	if e.Encode(v) != nil {
		return 5
	}
	return 0
}
func failure(w io.Writer, status int, code, message string) int {
	output(w, map[string]any{"error": map[string]string{"code": code, "message": message}})
	return status
}
func report(w io.Writer, err error) int {
	var api *client.APIError
	if errors.As(err, &api) {
		status := 5
		switch api.Status {
		case 400, 413, 422:
			status = 2
		case 401, 403:
			status = 3
		case 409, 412:
			status = 4
		}
		return failure(w, status, api.Code, api.Message)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return failure(w, 8, "deadline", "deadline exceeded; submitted operations continue until their durable result")
	}
	return failure(w, 5, "unavailable", err.Error())
}
func readJSON(path string, dst any) error {
	f, err := os.Open(path)
	if err != nil {
		return errors.New("cannot open JSON input")
	}
	defer f.Close()
	d := json.NewDecoder(io.LimitReader(f, 1<<20))
	d.DisallowUnknownFields()
	if err := d.Decode(dst); err != nil {
		return fmt.Errorf("invalid typed JSON input: %w", err)
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return errors.New("input must contain exactly one JSON object")
	}
	return nil
}
func wait(ctx context.Context, c *client.Client, path string, out io.Writer) int {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		var operation domain.Operation
		if err := c.Do(ctx, "GET", path, "", nil, &operation); err != nil {
			if ctx.Err() != nil {
				return report(out, ctx.Err())
			}
			return report(out, err)
		}
		if domain.Terminal(operation.State) {
			output(out, operation)
			switch operation.State {
			case "succeeded":
				return 0
			case "recovery-required":
				return 7
			default:
				return 6
			}
		}
		select {
		case <-ctx.Done():
			return report(out, ctx.Err())
		case <-ticker.C:
		}
	}
}
func runAdmin(args []string, out io.Writer) int {
	if len(args) == 0 {
		return failure(out, 2, "usage", "admin requires bootstrap, recover, issue, list or revoke")
	}
	action := args[0]
	f := flags("admin")
	config := f.String("config", "", "administrator-owned service configuration")
	role := f.String("role", "owner", "credential role")
	name := f.String("name", "owner", "credential name or revoke ID")
	path := f.String("output", "", "new credential file")
	ttl := f.Duration("ttl", 24*time.Hour, "credential lifetime")
	if f.Parse(args[1:]) != nil || f.NArg() != 0 || *config == "" {
		return failure(out, 2, "usage", "admin requires --config FILE")
	}
	switch action {
	case "bootstrap", "recover", "issue":
		if *path == "" {
			return failure(out, 2, "usage", "credential creation requires --output NEW_OWNER_ONLY_FILE")
		}
	case "list", "revoke":
	default:
		return failure(out, 2, "usage", "unsupported local administration action")
	}
	result, err := admin.Run(*config, action, *role, *name, *path, *ttl)
	if err != nil {
		return report(out, err)
	}
	return output(out, result)
}
func runRemoteHarness(ctx context.Context, c *client.Client, args []string, out io.Writer) int {
	if len(args) < 2 {
		return failure(out, 2, "usage", "harness requires export|configure NAME")
	}
	action, name := args[0], args[1]
	if name != "qwen" && name != "dsh" && name != "hermes" {
		return failure(out, 2, "invalid", "select qwen, dsh or hermes")
	}
	f := flags("harness")
	path := f.String("output", "", "new bundle JSON file")
	directory := f.String("directory", "", "new native configuration directory")
	if f.Parse(args[2:]) != nil || f.NArg() != 0 {
		return failure(out, 2, "usage", "invalid harness options")
	}
	if action != "export" && action != "configure" {
		return failure(out, 2, "usage", "remote harness supports export or configure")
	}
	var bundle domain.Bundle
	if err := c.Do(ctx, "GET", "/api/v1/harnesses/"+name+"/export", "", nil, &bundle); err != nil {
		return report(out, err)
	}
	if _, err := client.VerifyBundle(bundle); err != nil {
		return failure(out, 2, "integrity", err.Error())
	}
	if action == "export" {
		if *path == "" {
			return failure(out, 2, "usage", "export requires --output NEW_FILE")
		}
		b, err := json.MarshalIndent(bundle, "", "  ")
		if err != nil {
			return report(out, err)
		}
		if err := client.WriteNewFile(*path, append(b, '\n')); err != nil {
			return report(out, err)
		}
		return output(out, map[string]string{"status": "exported-not-qualified", "output": *path})
	}
	if *directory == "" {
		return failure(out, 2, "usage", "configure requires --directory NEW_DIRECTORY")
	}
	if err := client.ConfigureBundle(bundle, *directory); err != nil {
		return report(out, err)
	}
	return output(out, map[string]string{"status": "configured-not-qualified", "directory": *directory, "harness": name})
}
func runLocalHarness(args []string, out, errout io.Writer) int {
	if len(args) < 2 {
		return failure(out, 2, "usage", "local harness requires configure|launch NAME")
	}
	action, name := args[0], args[1]
	f := flags("harness")
	directory := f.String("directory", "", "native configuration directory")
	bundlePath := f.String("bundle", "", "exported bundle JSON")
	mode := f.String("mode", "cli", "cli or acp")
	if f.Parse(args[2:]) != nil || f.NArg() != 0 || *directory == "" {
		return failure(out, 2, "usage", "local harness requires --directory DIRECTORY")
	}
	if action == "configure" {
		if *bundlePath == "" {
			return failure(out, 2, "usage", "offline configure requires --bundle FILE")
		}
		bundle, err := client.ReadBundleFile(*bundlePath)
		if err != nil {
			return report(out, err)
		}
		if bundle.Harness != name {
			return failure(out, 2, "invalid", "selected harness differs from the exported bundle")
		}
		if err := client.ConfigureBundle(bundle, *directory); err != nil {
			return report(out, err)
		}
		return output(out, map[string]string{"status": "configured-not-qualified", "directory": *directory, "harness": name})
	}
	if action != "launch" {
		return failure(out, 2, "usage", "unsupported local client command")
	}
	fmt.Fprintln(errout, "Starting an explicitly selected local client. Tools execute on this client; no automatic fallback. Hermes output caps remain provider-owned.")
	if err := client.Launch(*directory, name, *mode); err != nil {
		return report(out, err)
	}
	return 0
}
