package cli

import (
	"context"
	"encoding/json"
	"errors"
	"io"

	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/client"
	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/domain"
)

// runPerformance only previews a sealed bundle or creates an explicit
// selection plan. It never applies a workload change or runs an experiment.
func runPerformance(ctx context.Context, c *client.Client, command string, args []string, out io.Writer) int {
	if command == "performance-inspect" {
		if len(args) != 1 || !validID(args[0]) {
			return failure(out, 2, "usage", "performance-inspect requires an operation ID")
		}
		var operation domain.Operation
		request := struct {
			OperationID string `json:"operation_id"`
		}{OperationID: args[0]}
		if err := c.Do(ctx, "POST", "/api/v1/performance/inspect", "", request, &operation); err != nil {
			return report(out, err)
		}
		return output(out, operation)
	}

	f := flags(command)
	file := f.String("file", "", "typed performance draft JSON")
	if f.Parse(args) != nil || f.NArg() != 0 || *file == "" {
		return failure(out, 2, "usage", command+" requires --file with typed JSON input")
	}
	var draft domain.Draft
	if err := readJSON(*file, &draft); err != nil {
		return failure(out, 2, "invalid", err.Error())
	}
	if !domain.PerformanceAction(draft.Action) {
		return failure(out, 2, "invalid", command+" requires performance.export or performance.profile.select")
	}
	if command == "performance-select" && draft.Action != "performance.profile.select" {
		return failure(out, 2, "invalid", "performance-select requires performance.profile.select")
	}
	if err := domain.ValidatePerformanceRequest(draft); err != nil {
		return failure(out, 2, "invalid", err.Error())
	}
	if command == "performance-select" {
		var plan domain.Plan
		if err := c.Do(ctx, "POST", "/api/v1/plans", "", draft, &plan); err != nil {
			return report(out, err)
		}
		return output(out, plan)
	}
	var result json.RawMessage
	if err := c.Do(ctx, "POST", "/api/v1/performance/preview", "", draft, &result); err != nil {
		return report(out, err)
	}
	return output(out, result)
}

func fetchPerformanceArtifact(ctx context.Context, c *client.Client, operationID string, expected domain.Artifact) (domain.Artifact, error) {
	var artifact domain.Artifact
	request := struct {
		OperationID string `json:"operation_id"`
		Name        string `json:"name"`
	}{OperationID: operationID, Name: expected.Name}
	if err := c.Do(ctx, "POST", "/api/v1/performance/artifact", "", request, &artifact); err != nil {
		return artifact, err
	}
	if artifact.Name != expected.Name || artifact.SHA256 != expected.SHA256 || artifact.Size != expected.Size || artifact.SourceRevision != expected.SourceRevision || artifact.Qualification != expected.Qualification {
		return domain.Artifact{}, errors.New("private performance artifact differs from the recorded operation; refresh its evidence")
	}
	return artifact, nil
}
