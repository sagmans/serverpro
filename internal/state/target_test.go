package state

import (
	"strings"
	"testing"
)

func TestValidateTargetRequiresExplicitProjectAndServer(t *testing.T) {
	target := Target{Namespace: "prod", Server: "default"}
	for _, tc := range []struct {
		name string
		st   State
		want string
	}{
		{name: "missing namespace", st: State{Server: "default"}, want: "state namespace"},
		{name: "missing server", st: State{Project: "prod"}, want: "state server"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateTarget(target, tc.st)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("ValidateTarget error = %v, want %q", err, tc.want)
			}
		})
	}
}
