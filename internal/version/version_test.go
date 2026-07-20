package version

import (
	"runtime/debug"
	"testing"
)

func TestDevVersion(t *testing.T) {
	tests := []struct {
		name string
		info *debug.BuildInfo
		want string
	}{
		{
			name: "clean checkout",
			info: &debug.BuildInfo{Settings: []debug.BuildSetting{
				{Key: "vcs.revision", Value: "d1571e1cfd24abcdef0123456789"},
			}},
			want: "dev-d1571e1cfd24",
		},
		{
			name: "dirty checkout",
			info: &debug.BuildInfo{Settings: []debug.BuildSetting{
				{Key: "vcs.revision", Value: "d1571e1cfd24abcdef0123456789"},
				{Key: "vcs.modified", Value: "true"},
			}},
			want: "dev-d1571e1cfd24-dirty",
		},
		{
			name: "short revision left untruncated",
			info: &debug.BuildInfo{Settings: []debug.BuildSetting{
				{Key: "vcs.revision", Value: "abc123"},
			}},
			want: "dev-abc123",
		},
		{
			name: "no vcs info",
			info: &debug.BuildInfo{},
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := devVersion(tt.info); got != tt.want {
				t.Errorf("devVersion() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCurrent_LdflagsWins(t *testing.T) {
	old := Version
	Version = "v2.2.0"
	t.Cleanup(func() { Version = old })

	if got := Current(); got != "v2.2.0" {
		t.Errorf("Current() = %q, want %q", got, "v2.2.0")
	}
}
