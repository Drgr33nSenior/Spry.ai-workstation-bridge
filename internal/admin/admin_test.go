package admin

import "testing"

func TestExplicitOwnerUID(t *testing.T) {
	for _, tc := range []struct {
		uid, owner int
		want       bool
	}{{0, 1000, true}, {1000, 1000, true}, {1001, 1000, false}, {501, 1000, false}} {
		if Authorized(tc.uid, tc.owner) != tc.want {
			t.Fatalf("authorization uid=%d owner=%d", tc.uid, tc.owner)
		}
	}
}
