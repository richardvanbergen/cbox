package workflow

import "testing"

func TestResolveOpenCommand(t *testing.T) {
	tests := []struct {
		name       string
		openFlag   bool
		openCmd    string
		configOpen string
		want       string
	}{
		{
			name:       "flag not set, no config",
			openFlag:   false,
			openCmd:    "",
			configOpen: "",
			want:       "",
		},
		{
			name:       "flag not set, config present",
			openFlag:   false,
			openCmd:    "",
			configOpen: "code $Dir",
			want:       "",
		},
		{
			name:       "flag set with command",
			openFlag:   true,
			openCmd:    "vim $Dir",
			configOpen: "code $Dir",
			want:       "vim $Dir",
		},
		{
			name:       "flag set without value, config present",
			openFlag:   true,
			openCmd:    "",
			configOpen: "code $Dir",
			want:       "code $Dir",
		},
		{
			name:       "flag set with whitespace-only value, config present",
			openFlag:   true,
			openCmd:    " ",
			configOpen: "code $Dir",
			want:       "code $Dir",
		},
		{
			name:       "flag set without value, no config",
			openFlag:   true,
			openCmd:    "",
			configOpen: "",
			want:       "",
		},
		{
			name:       "flag set with command, no config",
			openFlag:   true,
			openCmd:    "vim $Dir",
			configOpen: "",
			want:       "vim $Dir",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveOpenCommand(tt.openFlag, tt.openCmd, tt.configOpen)
			if got != tt.want {
				t.Errorf("resolveOpenCommand(%v, %q, %q) = %q, want %q",
					tt.openFlag, tt.openCmd, tt.configOpen, got, tt.want)
			}
		})
	}
}
