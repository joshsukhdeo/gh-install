package selector

import (
	"io/fs"
	"path/filepath"
	"runtime"
	"strings"
)

type BinaryType int

const (
	BinaryDebInstaller BinaryType = iota
	BinaryRpmInstaller
	BinaryPkgInstaller
	BinaryPacmanInstaller
	BinaryExecutable
	BinaryMacInstaller
	BinaryWindowsInstaller
)

func BinaryTypeFromPath(fromPath string) BinaryType {
	if strings.HasSuffix(fromPath, ".pkg.tar.zst") || strings.HasSuffix(fromPath, ".pkg.tar.xz") {
		return BinaryPacmanInstaller
	}
	lowerPath := strings.ToLower(fromPath)
	if strings.HasSuffix(lowerPath, ".msi") || (strings.Contains(lowerPath, "setup") && strings.HasSuffix(lowerPath, ".exe")) {
		return BinaryWindowsInstaller
	}
	extension := filepath.Ext(lowerPath)
	switch extension {
	case ".rpm":
		return BinaryRpmInstaller
	case ".deb":
		return BinaryDebInstaller
	case ".txz":
		return BinaryPkgInstaller
	case ".dmg":
		return BinaryMacInstaller
	case ".pkg":
		if runtime.GOOS == "darwin" {
			return BinaryMacInstaller
		}
		return BinaryPkgInstaller
	default:
		return BinaryExecutable
	}
}

type SelectorItem struct {
	Name         string
	Selected     bool
	Id           int
	Compressed   bool
	BinaryType   BinaryType
	DownloadPath string
	FsPath       string
	Fs           fs.FS
}
