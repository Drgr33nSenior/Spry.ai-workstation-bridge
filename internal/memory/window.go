package memory

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strconv"
	"strings"
)

func counters(s string) (map[string]int64, error) {
	out := map[string]int64{}
	for _, line := range strings.Split(strings.TrimSpace(s), "\n") {
		v := strings.Fields(line)
		if len(v) != 2 {
			return nil, errors.New("memory counters missing")
		}
		if _, ok := out[v[0]]; ok {
			return nil, errors.New("duplicate memory counter")
		}
		n, e := strconv.ParseInt(v[1], 10, 64)
		if e != nil || n < 0 {
			return nil, errors.New("invalid memory counter")
		}
		out[v[0]] = n
	}
	return out, nil
}
func parsePSI(s string) (map[string]int64, error) {
	out := map[string]int64{}
	for _, line := range strings.Split(strings.TrimSpace(s), "\n") {
		v := strings.Fields(line)
		if len(v) < 2 {
			return nil, errors.New("memory PSI missing")
		}
		if _, ok := out[v[0]]; ok {
			return nil, errors.New("duplicate PSI scope")
		}
		found := false
		for _, f := range v[1:] {
			if strings.HasPrefix(f, "total=") {
				n, e := strconv.ParseInt(strings.TrimPrefix(f, "total="), 10, 64)
				if e != nil || n < 0 || found {
					return nil, errors.New("invalid PSI total")
				}
				out[v[0]] = n
				found = true
			}
		}
		if !found {
			return nil, errors.New("PSI total missing")
		}
	}
	if _, ok := out["some"]; !ok {
		return nil, errors.New("PSI some missing")
	}
	if _, ok := out["full"]; !ok || len(out) != 2 {
		return nil, errors.New("PSI full missing")
	}
	return out, nil
}
func observeWindow(ctx context.Context, path string, limit, shm, total int64) (Window, error) {
	w := Window{Stat: map[string]int64{}}
	f, e := os.Open(path)
	if e != nil {
		return w, e
	}
	defer f.Close()
	scanner := bufio.NewScanner(io.LimitReader(contextReader{ctx, f}, MaxFile+1))
	scanner.Buffer(make([]byte, 4096), 2<<20)
	var psi map[string]int64
	var lastMono, lastUnix, lastPeak int64
	fail := func() (Window, error) { return w, errors.New("invalid or incomplete memory telemetry window") }
	for scanner.Scan() {
		var sample struct {
			Mono   int64  `json:"monotonic_ns"`
			Unix   int64  `json:"unix_ns"`
			Host   string `json:"host_memory"`
			Cgroup struct {
				ID     string            `json:"id"`
				Path   string            `json:"path"`
				Values map[string]string `json:"values"`
			} `json:"pod_cgroup"`
		}
		if json.Unmarshal(scanner.Bytes(), &sample) != nil || sample.Mono <= 0 || sample.Unix <= 0 || sample.Cgroup.ID == "" || sample.Cgroup.Path == "" {
			return fail()
		}
		v := sample.Cgroup.Values
		read := func(key string) (int64, error) {
			n, e := strconv.ParseInt(v[key], 10, 64)
			if e != nil || n < 0 {
				return 0, errors.New("missing nonnegative memory counter")
			}
			return n, nil
		}
		current, e := read("memory.current")
		if e != nil {
			return fail()
		}
		peak, e := read("memory.peak")
		if e != nil {
			return fail()
		}
		maximum, e := read("memory.max")
		if e != nil || maximum != limit<<20 || current <= 0 || current > peak || peak > maximum {
			return fail()
		}
		swap, e := read("memory.swap.current")
		if e != nil || swap != 0 {
			return fail()
		}
		stat, e := counters(v["memory.stat"])
		if e != nil {
			return fail()
		}
		for _, key := range []string{"anon", "file", "shmem"} {
			if _, ok := stat[key]; !ok {
				return fail()
			}
		}
		if stat["shmem"] > stat["file"] || stat["shmem"] > current {
			return fail()
		}
		events, e := counters(v["memory.events"])
		if e != nil {
			return fail()
		}
		for _, key := range []string{"high", "max", "oom", "oom_kill"} {
			if n, ok := events[key]; !ok || n != 0 {
				return fail()
			}
		}
		pressure, e := parsePSI(v["memory.pressure"])
		if e != nil {
			return fail()
		}
		host := map[string]int64{}
		for _, line := range strings.Split(sample.Host, "\n") {
			fields := strings.Fields(line)
			if len(fields) == 0 {
				continue
			}
			switch fields[0] {
			case "MemTotal:", "MemAvailable:", "SwapTotal:":
				if len(fields) != 3 || fields[2] != "kB" {
					return fail()
				}
				n, e := strconv.ParseInt(fields[1], 10, 64)
				if e != nil || n < 0 {
					return fail()
				}
				if _, exists := host[fields[0]]; exists {
					return fail()
				}
				host[fields[0]] = n
			}
		}
		if len(host) != 3 || host["MemTotal:"]/1024 != total || host["SwapTotal:"] != 0 {
			return fail()
		}
		if w.Samples == 0 {
			w.CgroupID = sample.Cgroup.ID
			w.Path = sample.Cgroup.Path
			w.FirstMono = sample.Mono
			w.FirstUnix = sample.Unix
			w.Available = host["MemAvailable:"] * 1024
			psi = pressure
		} else if w.CgroupID != sample.Cgroup.ID || w.Path != sample.Cgroup.Path || sample.Mono <= lastMono || sample.Mono-lastMono > 30e9 || sample.Unix <= lastUnix || peak < lastPeak || pressure["some"] != psi["some"] || pressure["full"] != psi["full"] {
			return fail()
		}
		w.Samples++
		if w.Samples > 100000 {
			return fail()
		}
		w.Sampled = max(w.Sampled, current)
		w.Peak = max(w.Peak, peak)
		for _, k := range []string{"anon", "file", "shmem"} {
			w.Stat[k] = max(w.Stat[k], stat[k])
		}
		w.Available = min(w.Available, host["MemAvailable:"]*1024)
		lastMono, lastUnix, lastPeak = sample.Mono, sample.Unix, peak
	}
	if e = scanner.Err(); e != nil {
		return w, e
	}
	if w.Samples < 2 {
		return fail()
	}
	w.LastMono = lastMono
	w.LastUnix = lastUnix
	w.Duration = float64(lastMono-w.FirstMono) / 1e9
	w.Envelope = w.Peak + (shm << 20)
	return w, nil
}
