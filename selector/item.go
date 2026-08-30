package selector

import (
	"io/fs"
	"path/filepath"
	"strings"
)

type BinaryType int

const (
	BinaryDebInstaller BinaryType = iota
	BinaryRpmInstaller
	BinaryPkgInstaller
	BinaryPacmanInstaller
	BinaryExecutable
)

func BinaryTypeFromPath(fromPath string) BinaryType {
	if strings.HasSuffix(fromPath, ".pkg.tar.zst") || strings.HasSuffix(fromPath, ".pkg.tar.xz") {
		return BinaryPacmanInstaller
	}
	extension := filepath.Ext(fromPath)
	switch extension {
	case ".rpm":
		return BinaryRpmInstaller
	case ".deb":
		return BinaryDebInstaller
	case ".pkg", ".txz":
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
