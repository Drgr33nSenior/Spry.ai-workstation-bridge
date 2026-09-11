package hostexec

import (
	"strings"
	"testing"
)

func TestServingStatusSeparatesReadinessWarmthAndRestartIdentity(t *testing.T) {
	c := map[string]any{"name": "sglang", "containerID": "container-1", "imageID": "sha256:fixture", "restartCount": 0, "ready": false, "state": map[string]any{"running": map[string]any{"startedAt": "fixture-time"}}}
	p := map[string]any{"metadata": map[string]any{"uid": "pod-1", "namespace": "ai", "ownerReferences": []any{map[string]any{"uid": "replica-1"}}}, "spec": map[string]any{"nodeName": "fixture"}, "status": map[string]any{"phase": "Running", "containerStatuses": []any{c}}}
	pods := map[string]any{"items": []any{p}}
	rs := map[string]bool{"replica-1": true}
	s := observedServingStatus(pods, rs, "fixture", "ai", "ai")
	if s.State != "model-loading" || s.KubernetesReady == nil || *s.KubernetesReady {
		t.Fatalf("loading %+v", s)
	}
	c["state"] = map[string]any{"waiting": map[string]any{"reason": "CrashLoopBackOff", "message": "unbounded private kubelet message"}}
	c["lastState"] = map[string]any{"terminated": map[string]any{"reason": "OOMKilled", "message": "historical private termination message"}}
	s = observedServingStatus(pods, rs, "fixture", "ai", "ai")
	if s.State != "unavailable" || !strings.Contains(s.Reason, "restarting repeatedly after a memory termination") || strings.Contains(s.Reason, "private") {
		t.Fatalf("crash loop treated as loading or exposed a raw message: %+v", s)
	}
	c["state"] = map[string]any{"terminated": map[string]any{"reason": "OOMKilled", "message": "unbounded private termination message"}}
	s = observedServingStatus(pods, rs, "fixture", "ai", "ai")
	if s.State != "unavailable" || !strings.Contains(s.Reason, "memory use") || strings.Contains(s.Reason, "private") {
		t.Fatalf("OOM treated as loading or exposed a raw message: %+v", s)
	}
	c["state"] = map[string]any{"waiting": map[string]any{"reason": "UnrecognizedWaitingReason"}}
	s = observedServingStatus(pods, rs, "fixture", "ai", "ai")
	if s.State != "unknown" || !strings.Contains(s.Reason, "no recognized") {
		t.Fatalf("unrecognized waiting state was treated as loading: %+v", s)
	}
	c["ready"] = true
	c["state"] = map[string]any{"running": map[string]any{"startedAt": "fixture-time"}}
	c["lastState"] = map[string]any{"terminated": map[string]any{"reason": "OOMKilled", "message": "historical private message"}}
	s = observedServingStatus(pods, rs, "fixture", "ai", "ai")
	if s.State != "healthy" || s.RepresentativeWarmup != "unknown" {
		t.Fatal("readiness invented warmup")
	}
	old := s.Identity
	c["containerID"] = ""
	if s = observedServingStatus(pods, rs, "fixture", "ai", "ai"); s.State != "unknown" || s.KubernetesReady != nil {
		t.Fatalf("missing current process identity was accepted: %+v", s)
	}
	c["containerID"] = "container-2"
	c["restartCount"] = 1
	s = observedServingStatus(pods, rs, "fixture", "ai", "ai")
	if s.Identity == old || s.State == "ready" {
		t.Fatal("restart reused warmth")
	}
	old = s.Identity
	object(p["metadata"])["uid"] = "pod-2"
	s = observedServingStatus(pods, rs, "fixture", "ai", "ai")
	if s.Identity == old || s.State != "healthy" || s.RepresentativeWarmup != "unknown" {
		t.Fatalf("replacement Pod reused lifecycle identity or warmth: %+v", s)
	}
	if s = observedServingStatus(pods, rs, "fixture", "ai", "gaming"); s.State != "not-applicable" {
		t.Fatal("gaming advertised ready")
	}
	if s = observedServingStatus(pods, rs, "foreign", "ai", "ai"); s.State != "unknown" {
		t.Fatal("foreign node")
	}
	pods["items"] = []any{p, p}
	if s = observedServingStatus(pods, rs, "fixture", "ai", "ai"); s.State != "unknown" {
		t.Fatal("ambiguous rollout")
	}
}
