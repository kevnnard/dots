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
	ManagerUnknown PackageManager = "unknown"
)

type Distro string

const (
	DistroUbuntu  Distro = "ubuntu"
	DistroDebian  Distro = "debian"
	DistroArch    Distro = "arch"
	DistroFedora  Distro = "fedora"
	DistroDarwin  Distro = "darwin"
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

	if info.OS == "darwin" {
		info.Distro = DistroDarwin
		if hasCommand("brew") {
			info.PrimaryManager = ManagerBrew
		} else {
			info.PrimaryManager = ManagerUnknown
		}
	} else if info.OS == "linux" {
		info.Distro = detectLinuxDistro()
		info.PrimaryManager = pickPrimaryLinuxManager(info.Distro)
	} else {
		info.PrimaryManager = ManagerUnknown
	}

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

func parseOSRelease(r io.Reader) Distro {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "ID=") {
			val := strings.Trim(strings.TrimPrefix(line, "ID="), "\"")
			switch strings.ToLower(val) {
			case "ubuntu":
				return DistroUbuntu
			case "debian":
				return DistroDebian
			case "arch":
				return DistroArch
			case "fedora":
				return DistroFedora
			default:
				return DistroUnknown
			}
		}
	}
	return DistroUnknown
}

func pickPrimaryLinuxManager(distro Distro) PackageManager {
	if hasCommand("yay") {
		return ManagerYay
	}
	if hasCommand("pacman") {
		return ManagerPacman
	}
	if hasCommand("apt-get") {
		return ManagerApt
	}
	if hasCommand("dnf") {
		return ManagerDnf
	}
	if hasCommand("brew") {
		return ManagerBrew
	}
	return ManagerUnknown
}
