package detector

import (
	"runtime"
	"strings"
	"testing"
)

func TestDetectCurrent(t *testing.T) {
	info := Detect()
	if info.OS != runtime.GOOS {
		t.Errorf("expected OS %s, got %s", runtime.GOOS, info.OS)
	}
	if info.Arch != runtime.GOARCH {
		t.Errorf("expected Arch %s, got %s", runtime.GOARCH, info.Arch)
	}
	if runtime.GOOS == "darwin" && info.PrimaryManager != ManagerBrew && info.PrimaryManager != ManagerUnknown {
		t.Errorf("expected brew or unknown on darwin, got %s", info.PrimaryManager)
	}
}

func TestParseLinuxDistro(t *testing.T) {
	cases := []struct {
		name     string
		content  string
		expected Distro
	}{
		{
			name:     "ubuntu",
			content:  "NAME=\"Ubuntu\"\nID=ubuntu\nID_LIKE=debian\n",
			expected: DistroUbuntu,
		},
		{
			name:     "debian",
			content:  "PRETTY_NAME=\"Debian GNU/Linux 12 (bookworm)\"\nNAME=\"Debian GNU/Linux\"\nID=debian\n",
			expected: DistroDebian,
		},
		{
			name:     "arch",
			content:  "NAME=\"Arch Linux\"\nID=arch\n",
			expected: DistroArch,
		},
		{
			name:     "fedora",
			content:  "NAME=\"Fedora Linux\"\nID=fedora\n",
			expected: DistroFedora,
		},
		{
			name:     "unknown",
			content:  "FOO=bar\n",
			expected: DistroUnknown,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			distro := parseOSRelease(strings.NewReader(tc.content))
			if distro != tc.expected {
				t.Errorf("expected %s, got %s", tc.expected, distro)
			}
		})
	}
}
