package worker

import (
	"errors"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

type fixtureCgroupFS struct {
	files       map[string]string
	directories map[string]bool
	writeErr    map[string]error
	writeResult map[string]string
	mkdirErr    map[string]error
}

func newFixtureCgroupFS(root string, pid int) *fixtureCgroupFS {
	return &fixtureCgroupFS{files: map[string]string{
		filepath.Join(root, "cgroup.procs"):               "",
		filepath.Join(root, "supervisor", "cgroup.procs"): "" + strconv.Itoa(pid) + "\n",
		filepath.Join(root, "cgroup.controllers"):         "cpu memory pids\n",
		filepath.Join(root, "cgroup.subtree_control"):     "",
	}, directories: map[string]bool{root: true, filepath.Join(root, "supervisor"): true}, writeErr: map[string]error{}, writeResult: map[string]string{}, mkdirErr: map[string]error{}}
}

func (f *fixtureCgroupFS) ReadFile(name string) ([]byte, error) {
	v, ok := f.files[name]
	if !ok {
		return nil, fs.ErrNotExist
	}
	return []byte(v), nil
}

func (f *fixtureCgroupFS) WriteFile(name string, data []byte, _ fs.FileMode) error {
	if err := f.writeErr[name]; err != nil {
		return err
	}
	if value, ok := f.writeResult[name]; ok {
		f.files[name] = value
		return nil
	}
	if strings.HasSuffix(name, "cgroup.subtree_control") {
		current := strings.Fields(f.files[name])
		for _, token := range strings.Fields(string(data)) {
			if strings.HasPrefix(token, "+") && !containsField(strings.Join(current, " "), strings.TrimPrefix(token, "+")) {
				current = append(current, strings.TrimPrefix(token, "+"))
			}
		}
		f.files[name] = strings.Join(current, " ")
		return nil
	}
	f.files[name] = string(data)
	return nil
}

func (f *fixtureCgroupFS) Mkdir(name string, _ fs.FileMode) error {
	if err := f.mkdirErr[name]; err != nil {
		return err
	}
	if f.directories[name] {
		return fs.ErrExist
	}
	f.directories[name] = true
	return nil
}

func TestDelegatedCgroupInitializesControllersAndLimits(t *testing.T) {
	root := "/delegated/bridge-worker.service"
	files := newFixtureCgroupFS(root, 42)
	if err := initializeDelegatedCgroup(files, root, 42); err != nil {
		t.Fatal(err)
	}
	if got, _ := files.ReadFile(filepath.Join(root, "cgroup.subtree_control")); strings.TrimSpace(string(got)) != "cpu memory pids" {
		t.Fatalf("controllers not enabled: %q", got)
	}
	group, err := createLimitedJobCgroup(files, root, "operation-1", 8192, 4)
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{"memory.max": "8589934592", "memory.swap.max": "0", "pids.max": "256", "cpu.max": "400000 100000"} {
		got, err := files.ReadFile(filepath.Join(group, name))
		if err != nil || strings.TrimSpace(string(got)) != want {
			t.Fatalf("%s = %q, %v; want %q", name, got, err, want)
		}
	}
	// A second job sees the enabled state and does not need to modify ancestors.
	if err := initializeDelegatedCgroup(files, root, 42); err != nil {
		t.Fatal(err)
	}
	if _, err := createLimitedJobCgroup(files, root, "operation-2", 4096, 1); err != nil {
		t.Fatal(err)
	}
}

func TestDelegatedCgroupFailsClosed(t *testing.T) {
	root := "/delegated/bridge-worker.service"
	for name, arrange := range map[string]func(*fixtureCgroupFS){
		"missing controller": func(f *fixtureCgroupFS) { f.files[filepath.Join(root, "cgroup.controllers")] = "cpu memory" },
		"internal process":   func(f *fixtureCgroupFS) { f.files[filepath.Join(root, "cgroup.procs")] = "99\n" },
		"wrong supervisor":   func(f *fixtureCgroupFS) { f.files[filepath.Join(root, "supervisor", "cgroup.procs")] = "99\n" },
		"denied enable": func(f *fixtureCgroupFS) {
			f.writeErr[filepath.Join(root, "cgroup.subtree_control")] = errors.New("permission denied")
		},
		"unreadable state": func(f *fixtureCgroupFS) { delete(f.files, filepath.Join(root, "cgroup.subtree_control")) },
	} {
		t.Run(name, func(t *testing.T) {
			files := newFixtureCgroupFS(root, 42)
			arrange(files)
			if err := initializeDelegatedCgroup(files, root, 42); err == nil {
				t.Fatal("unsafe delegated cgroup accepted")
			}
		})
	}
}

func TestDelegatedJobCgroupRejectsFailedLimitSetup(t *testing.T) {
	root := "/delegated/bridge-worker.service"
	group := filepath.Join(root, "bridge-operation-1")
	for name, arrange := range map[string]func(*fixtureCgroupFS){
		"create denied": func(f *fixtureCgroupFS) { f.mkdirErr[group] = errors.New("permission denied") },
		"limit denied": func(f *fixtureCgroupFS) {
			f.writeErr[filepath.Join(group, "memory.swap.max")] = errors.New("unsupported")
		},
		"limit not retained": func(f *fixtureCgroupFS) { f.writeResult[filepath.Join(group, "memory.max")] = "max" },
	} {
		t.Run(name, func(t *testing.T) {
			files := newFixtureCgroupFS(root, 42)
			arrange(files)
			if _, err := createLimitedJobCgroup(files, root, "operation-1", 8192, 4); err == nil {
				t.Fatal("unbounded job cgroup accepted")
			}
		})
	}
}
