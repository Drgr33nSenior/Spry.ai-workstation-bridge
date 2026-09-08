package worker

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
)

// cgroupFiles is deliberately small so controller admission can be tested with
// a fixture. The production implementation is osCgroupFiles in linux.go.
// It only addresses the delegated worker subtree supplied by root-owned policy.
type cgroupFiles interface {
	ReadFile(string) ([]byte, error)
	WriteFile(string, []byte, fs.FileMode) error
	Mkdir(string, fs.FileMode) error
}

var requiredCgroupControllers = []string{"cpu", "memory", "pids"}

func initializeDelegatedCgroup(files cgroupFiles, root string, workerPID int) error {
	root = filepath.Clean(root)
	if !filepath.IsAbs(root) || root == "/" {
		return errors.New("worker delegated cgroup root is invalid")
	}
	procs, err := files.ReadFile(filepath.Join(root, "cgroup.procs"))
	if err != nil {
		return fmt.Errorf("worker delegated cgroup process placement is unreadable: %w", err)
	}
	if len(strings.Fields(string(procs))) != 0 {
		return errors.New("worker delegated cgroup has internal processes; DelegateSubgroup=supervisor is required")
	}
	supervisor, err := files.ReadFile(filepath.Join(root, "supervisor", "cgroup.procs"))
	if err != nil {
		return fmt.Errorf("worker supervisor cgroup is unreadable: %w", err)
	}
	if !containsField(string(supervisor), strconv.Itoa(workerPID)) {
		return errors.New("worker process is not in its delegated supervisor cgroup")
	}
	available, err := files.ReadFile(filepath.Join(root, "cgroup.controllers"))
	if err != nil {
		return fmt.Errorf("worker delegated cgroup v2 controllers are unavailable: %w", err)
	}
	if missing := missingControllers(string(available)); len(missing) != 0 {
		return fmt.Errorf("worker delegated cgroup lacks required controllers: %s", strings.Join(missing, ", "))
	}
	enabled, err := files.ReadFile(filepath.Join(root, "cgroup.subtree_control"))
	if err != nil {
		return fmt.Errorf("worker delegated cgroup controller state is unreadable: %w", err)
	}
	if missing := missingControllers(string(enabled)); len(missing) != 0 {
		request := make([]string, 0, len(missing))
		for _, controller := range missing {
			request = append(request, "+"+controller)
		}
		if err := files.WriteFile(filepath.Join(root, "cgroup.subtree_control"), []byte(strings.Join(request, " ")), 0600); err != nil {
			return fmt.Errorf("worker cannot enable delegated cgroup controllers: %w", err)
		}
		enabled, err = files.ReadFile(filepath.Join(root, "cgroup.subtree_control"))
		if err != nil {
			return fmt.Errorf("worker delegated cgroup controller state is unreadable after enablement: %w", err)
		}
		if missing := missingControllers(string(enabled)); len(missing) != 0 {
			return fmt.Errorf("worker delegated cgroup controllers did not remain enabled: %s", strings.Join(missing, ", "))
		}
	}
	return nil
}

func createLimitedJobCgroup(files cgroupFiles, root, id string, memoryMiB int64, jobs int) (string, error) {
	if !validID(id) {
		return "", errors.New("invalid worker cgroup operation ID")
	}
	if memoryMiB < 1 || jobs < 1 {
		return "", errors.New("invalid worker cgroup limits")
	}
	group := filepath.Join(filepath.Clean(root), "bridge-"+id)
	if err := files.Mkdir(group, 0700); err != nil {
		return "", fmt.Errorf("worker cannot create delegated job cgroup: %w", err)
	}
	limits := []struct {
		name  string
		value string
	}{
		{"memory.max", strconv.FormatInt(memoryMiB<<20, 10)},
		{"memory.swap.max", "0"},
		{"pids.max", "256"},
		{"cpu.max", fmt.Sprintf("%d 100000", jobs*100000)},
	}
	for _, limit := range limits {
		path := filepath.Join(group, limit.name)
		if err := files.WriteFile(path, []byte(limit.value), 0600); err != nil {
			return "", fmt.Errorf("worker cgroup cannot set %s: %w", limit.name, err)
		}
		actual, err := files.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("worker cgroup cannot read back %s: %w", limit.name, err)
		}
		if strings.Join(strings.Fields(string(actual)), " ") != limit.value {
			return "", fmt.Errorf("worker cgroup %s did not retain requested limit", limit.name)
		}
	}
	return group, nil
}

func missingControllers(have string) []string {
	fields := map[string]bool{}
	for _, field := range strings.Fields(have) {
		fields[field] = true
	}
	missing := make([]string, 0, len(requiredCgroupControllers))
	for _, controller := range requiredCgroupControllers {
		if !fields[controller] {
			missing = append(missing, controller)
		}
	}
	return missing
}

func containsField(value, want string) bool {
	for _, field := range strings.Fields(value) {
		if field == want {
			return true
		}
	}
	return false
}
