package store

import (
	"fmt"
	"testing"
	"time"

	"github.com/Drgr33nSenior/Spry.ai-workstation-bridge/internal/domain"
)

// retainedFixture mirrors the bounded maximum retained history that made a
// point lookup pay to copy unrelated event payloads before scoped reads.
func retainedFixture() *Store {
	return retainedFixtureSize(500)
}

func retainedFixtureSize(count int) *Store {
	s := &Store{state: NewState("demo")}
	base := time.Unix(1, 0).UTC()
	for i := range count {
		events := make([]domain.Progress, 128)
		for j := range events {
			events[j] = domain.Progress{Phase: "fixture", Message: "retained event payload"}
		}
		id := fmt.Sprintf("operation-%03d", i)
		s.state.Operations[id] = domain.Operation{
			ID:        id,
			State:     "succeeded",
			CreatedAt: base.Add(time.Duration(i) * time.Second),
			UpdatedAt: base.Add(time.Duration(i) * time.Second),
			Events:    events,
		}
	}
	return s
}

func BenchmarkViewRetainedOperations(b *testing.B) {
	s := retainedFixture()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		_ = s.View()
	}
}

func BenchmarkOperationWithRetainedOperations(b *testing.B) {
	s := retainedFixture()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		_, _ = s.Operation("operation-499")
	}
}

func BenchmarkOperationRecordsWithRetainedOperations(b *testing.B) {
	s := retainedFixture()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		_ = s.OperationRecords()
	}
}
