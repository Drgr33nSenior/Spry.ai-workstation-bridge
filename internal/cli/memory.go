package cli

import (
	"context"
	"encoding/json"
	"errors"
	"io"

	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/client"
	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/domain"
	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/memory"
)

func runMemory(ctx context.Context, c *client.Client, command string, args []string, out io.Writer) int {
	if command == "memory-inspect" {
		if len(args) != 1 || !validID(args[0]) {
			return failure(out, 2, "usage", "memory-inspect requires an operation ID")
		}
		var operation domain.Operation
		if err := c.Do(ctx, "POST", "/api/v1/memory/inspect", "", domain.MemoryOperationRequest{OperationID: args[0]}, &operation); err != nil {
			return report(out, err)
		}
		return output(out, operation)
	}
	f := flags(command)
	file := f.String("file", "", "typed memory JSON input")
	if f.Parse(args) != nil || f.NArg() != 0 || *file == "" {
		return failure(out, 2, "usage", command+" requires --file with typed JSON input")
	}
	var body any
	endpoint := "/api/v1/memory/preview"
	if command == "memory-preview" {
		var draft domain.Draft
		if err := readJSON(*file, &draft); err != nil {
			return failure(out, 2, "invalid", err.Error())
		}
		if !domain.MemoryAction(draft.Action) {
			return failure(out, 2, "invalid", "memory-preview requires memory.evidence.import or memory.plan.export")
		}
		if err := domain.ValidateMemoryRequest(draft); err != nil {
			return failure(out, 2, "invalid", err.Error())
		}
		body = draft
	} else {
		var request domain.MemoryRequest
		if err := readJSON(*file, &request); err != nil {
			return failure(out, 2, "invalid", err.Error())
		}
		if err := domain.ValidateMemoryRequest(domain.Draft{Action: "memory.plan.export", Memory: &request}); err != nil {
			return failure(out, 2, "invalid", err.Error())
		}
		body, endpoint = request, "/api/v1/memory/advice"
	}
	var result json.RawMessage
	if err := c.Do(ctx, "POST", endpoint, "", body, &result); err != nil {
		return report(out, err)
	}
	return output(out, result)
}

func fetchMemoryArtifact(ctx context.Context, c *client.Client, operationID string, expected domain.Artifact) (domain.Artifact, error) {
	var artifact domain.Artifact
	if err := c.Do(ctx, "POST", "/api/v1/memory/artifact", "", domain.MemoryArtifactRequest{OperationID: operationID, Name: expected.Name}, &artifact); err != nil {
		return artifact, err
	}
	if artifact.Name != expected.Name || artifact.SHA256 != expected.SHA256 || artifact.Size != expected.Size || artifact.SourceRevision != expected.SourceRevision || artifact.Qualification != expected.Qualification {
		return domain.Artifact{}, errors.New("private memory artifact differs from the recorded operation; refresh its evidence")
	}
	return artifact, nil
}

func runMemorySeal(ctx context.Context, args []string, out io.Writer) int {
	f := flags("memory-seal")
	directory := f.String("directory", "", "absolute local evidence directory")
	source := f.String("source-revision", "", "source content revision")
	hardware := f.String("hardware-sha256", "", "hardware evidence SHA-256")
	boot := f.String("boot-id", "", "hardware boot identity")
	if f.Parse(args) != nil || f.NArg() != 0 || *directory == "" || *source == "" || *hardware == "" || *boot == "" {
		return failure(out, 2, "usage", "memory-seal requires --directory ABSOLUTE_DIRECTORY --source-revision SHA256 --hardware-sha256 SHA256 --boot-id UUID")
	}
	sha, err := memory.Seal(ctx, *directory, *source, *hardware, *boot)
	if err != nil {
		if ctx.Err() != nil {
			return failure(out, 8, "deadline", "local memory sealing interrupted; inspect retained evidence before retrying")
		}
		output(out, map[string]any{"error": map[string]string{"code": "invalid", "message": err.Error()}, "manifest_sha256": sha, "status": "unqualified"})
		return 2
	}
	return output(out, map[string]string{"status": "sealed-unqualified", "directory": *directory, "manifest_sha256": sha})
}
