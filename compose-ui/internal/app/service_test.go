package app

import (
	"errors"
	"os/exec"
	"testing"
)

func TestShouldFallbackToDockerComposePlugin(t *testing.T) {
	tests := []struct {
		name string
		err  error
		out  string
		want bool
	}{
		{name: "nil error", err: nil, out: "", want: false},
		{name: "docker-compose not found", err: exec.ErrNotFound, out: "", want: true},
		{name: "docker-compose shell missing", err: errors.New("exit status 127"), out: "docker-compose: command not found", want: true},
		{name: "docker-compose executable missing", err: errors.New("exit status 1"), out: "executable file not found in $PATH", want: true},
		{name: "other error", err: errors.New("exit status 1"), out: "permission denied", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldFallbackToDockerComposePlugin(tt.err, []byte(tt.out))
			if got != tt.want {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}
