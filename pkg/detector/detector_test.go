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
			name:     "omarchy via ID_LIKE",
			content:  "NAME=\"Omarchy\"\nID=omarchy\nID_LIKE=arch\n",
			expected: DistroArch,
		},
		{
			name:     "cachyos via ID_LIKE",
			content:  "ID=cachyos\nID_LIKE=\"arch\"\n",
			expected: DistroArch,
		},
		{
			name:     "mint maps to ubuntu family",
			content:  "ID=linuxmint\nID_LIKE=\"ubuntu debian\"\n",
			expected: DistroUbuntu,
		},
		{
			name:     "nobara via ID_LIKE",
			content:  "ID=nobara\nID_LIKE=\"rhel centos fedora\"\n",
			expected: DistroFedora,
		},
		{
			name:     "opensuse",
			content:  "ID=\"opensuse-tumbleweed\"\nID_LIKE=\"opensuse suse\"\n",
			expected: DistroSuse,
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

func TestPickPrimaryManager(t *testing.T) {
	has := func(names ...string) func(string) bool {
		return func(c string) bool {
			for _, n := range names {
				if n == c {
					return true
				}
			}
			return false
		}
	}
	cases := []struct {
		name     string
		os       string
		distro   Distro
		has      func(string) bool
		expected PackageManager
	}{
		{"omarchy prefers yay", "linux", DistroArch, has("yay", "pacman"), ManagerYay},
		{"arch without yay uses pacman", "linux", DistroArch, has("pacman"), ManagerPacman},
		{"ubuntu uses apt even if brew exists", "linux", DistroUbuntu, has("apt-get", "brew"), ManagerApt},
		{"fedora uses dnf", "linux", DistroFedora, has("dnf"), ManagerDnf},
		{"suse uses zypper", "linux", DistroSuse, has("zypper"), ManagerZypper},
		{"unknown linux falls back to brew", "linux", DistroUnknown, has("brew"), ManagerBrew},
		{"windows prefers winget", "windows", DistroWindows, has("winget", "scoop"), ManagerWinget},
		{"windows scoop when no winget", "windows", DistroWindows, has("scoop", "choco"), ManagerScoop},
		{"darwin brew", "darwin", DistroDarwin, has("brew"), ManagerBrew},
		{"darwin without brew", "darwin", DistroDarwin, has(), ManagerUnknown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := pickPrimaryManager(tc.os, tc.distro, tc.has)
			if got != tc.expected {
				t.Errorf("expected %s, got %s", tc.expected, got)
			}
		})
	}
}
