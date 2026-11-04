package detector

import (
	"bufio"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

type PackageManager string

const (
	ManagerBrew    PackageManager = "brew"
	ManagerApt     PackageManager = "apt"
	ManagerPacman  PackageManager = "pacman"
	ManagerDnf     PackageManager = "dnf"
	ManagerYay     PackageManager = "yay"
	ManagerPnpm    PackageManager = "pnpm"
	ManagerNpm     PackageManager = "npm"
	ManagerCargo   PackageManager = "cargo"
	ManagerZypper  PackageManager = "zypper"
	ManagerWinget  PackageManager = "winget"
	ManagerScoop   PackageManager = "scoop"
	ManagerChoco   PackageManager = "choco"
	ManagerUnknown PackageManager = "unknown"
)

type Distro string

const (
	DistroUbuntu  Distro = "ubuntu"
	DistroDebian  Distro = "debian"
	DistroArch    Distro = "arch"
	DistroFedora  Distro = "fedora"
	DistroSuse    Distro = "suse"
	DistroDarwin  Distro = "darwin"
	DistroWindows Distro = "windows"
	DistroUnknown Distro = "unknown"
)

type SystemInfo struct {
	OS             string           `json:"os"`
	Arch           string           `json:"arch"`
	Distro         Distro           `json:"distro"`
	PrimaryManager PackageManager   `json:"primary_manager"`
	Managers       []PackageManager `json:"available_managers"`
}

// Detect gathers OS, architecture, Linux distribution (if applicable), and available package managers.
func Detect() *SystemInfo {
	info := &SystemInfo{
		OS:       runtime.GOOS,
		Arch:     runtime.GOARCH,
		Distro:   DistroUnknown,
		Managers: detectAvailableManagers(),
	}

	switch info.OS {
	case "darwin":
		info.Distro = DistroDarwin
	case "windows":
		info.Distro = DistroWindows
	case "linux":
		info.Distro = detectLinuxDistro()
	}
	info.PrimaryManager = pickPrimaryManager(info.OS, info.Distro, hasCommand)

	return info
}

// HasManager returns true if the specified package manager is available on the system.
func (s *SystemInfo) HasManager(pm PackageManager) bool {
	if s == nil {
		return false
	}
	for _, m := range s.Managers {
		if m == pm {
			return true
		}
	}
	return false
}

// IsInstalled returns true if the command binary exists in PATH.
func IsInstalled(cmdName string) bool {
	return hasCommand(cmdName)
}

func hasCommand(cmdName string) bool {
	_, err := exec.LookPath(cmdName)
	return err == nil
}

func detectAvailableManagers() []PackageManager {
	candidates := []struct {
		cmd string
		mgr PackageManager
	}{
		{"brew", ManagerBrew},
		{"apt-get", ManagerApt},
		{"pacman", ManagerPacman},
		{"yay", ManagerYay},
		{"dnf", ManagerDnf},
		{"zypper", ManagerZypper},
		{"winget", ManagerWinget},
		{"scoop", ManagerScoop},
		{"choco", ManagerChoco},
		{"pnpm", ManagerPnpm},
		{"npm", ManagerNpm},
		{"cargo", ManagerCargo},
	}

	var found []PackageManager
	for _, c := range candidates {
		if hasCommand(c.cmd) {
			found = append(found, c.mgr)
		}
	}
	return found
}

func detectLinuxDistro() Distro {
	f, err := os.Open("/etc/os-release")
	if err != nil {
		f, err = os.Open("/usr/lib/os-release")
		if err != nil {
			return DistroUnknown
		}
	}
	defer f.Close()

	return parseOSRelease(f)
}

// distroFromID maps a single os-release identifier to a known distro family.
func distroFromID(id string) Distro {
	switch strings.ToLower(id) {
	case "ubuntu":
		return DistroUbuntu
	case "debian":
		return DistroDebian
	case "arch":
		return DistroArch
	case "fedora":
		return DistroFedora
	case "suse", "opensuse", "opensuse-leap", "opensuse-tumbleweed", "sles":
		return DistroSuse
	}
	return DistroUnknown
}

// parseOSRelease resolves ID first, then each ID_LIKE token, so derivatives
// (Omarchy, CachyOS, Mint, Nobara...) map to their parent family.
func parseOSRelease(r io.Reader) Distro {
	var id, idLike string
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		switch {
		case strings.HasPrefix(line, "ID="):
			id = strings.Trim(strings.TrimPrefix(line, "ID="), "\"'")
		case strings.HasPrefix(line, "ID_LIKE="):
			idLike = strings.Trim(strings.TrimPrefix(line, "ID_LIKE="), "\"'")
		}
	}
	if d := distroFromID(id); d != DistroUnknown {
		return d
	}
	for _, token := range strings.Fields(idLike) {
		if d := distroFromID(token); d != DistroUnknown {
			return d
		}
	}
	return DistroUnknown
}

// pickPrimaryManager chooses the preferred native package manager. The has
// function reports whether a command exists, which keeps the choice testable.
func pickPrimaryManager(osName string, distro Distro, has func(string) bool) PackageManager {
	var order []PackageManager
	switch osName {
	case "darwin":
		order = []PackageManager{ManagerBrew}
	case "windows":
		order = []PackageManager{ManagerWinget, ManagerScoop, ManagerChoco}
	default:
		switch distro {
		case DistroArch:
			order = []PackageManager{ManagerYay, ManagerPacman}
		case DistroUbuntu, DistroDebian:
			order = []PackageManager{ManagerApt}
		case DistroFedora:
			order = []PackageManager{ManagerDnf}
		case DistroSuse:
			order = []PackageManager{ManagerZypper}
		}
		order = append(order, ManagerYay, ManagerPacman, ManagerApt, ManagerDnf, ManagerZypper, ManagerBrew)
	}
	for _, m := range order {
		if has(managerCommand(m)) {
			return m
		}
	}
	return ManagerUnknown
}

// managerCommand returns the executable name that backs a package manager.
func managerCommand(m PackageManager) string {
	if m == ManagerApt {
		return "apt-get"
	}
	return string(m)
}
