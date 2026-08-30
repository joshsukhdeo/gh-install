package selector

import (
	"io/fs"
	"path/filepath"
)

type BinaryType int

const (
	BinaryDebInstaller BinaryType = iota
	BinaryRpmInstaller
	BinaryPkgInstaller
	BinaryExecutable
)

func BinaryTypeFromPath(fromPath string) BinaryType {
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
