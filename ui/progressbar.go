package ui

// ProgressBar defines the interface for progress bar implementations.
type ProgressBar interface {
	Start()
	Stop()
	Pause()
	Resume()
	WaitForAnimation()
	Update(step int, a string, b string, c string, target string, e string)
	UpdateSymlink(symlink string)
	AddAsset(name string, fullName string, symlink string, installCmd string)
	SetCurrentAsset(index int)
	SetAssetInstallCmd(installCmd string)
	View() string
	Error(args ...any)
}

// NullProgressBar is a no-op progress bar for when progress is disabled.
type NullProgressBar struct{}

func (p *NullProgressBar) Start()                                              {}
func (p *NullProgressBar) Stop()                                               {}
func (p *NullProgressBar) Pause()                                              {}
func (p *NullProgressBar) Resume()                                             {}
func (p *NullProgressBar) WaitForAnimation()                                   {}
func (p *NullProgressBar) Update(step int, a, b, c, target, e string)          {}
func (p *NullProgressBar) UpdateSymlink(symlink string)                        {}
func (p *NullProgressBar) AddAsset(name, fullName, symlink, installCmd string) {}
func (p *NullProgressBar) SetCurrentAsset(index int)                           {}
func (p *NullProgressBar) SetAssetInstallCmd(installCmd string)                {}
func (p *NullProgressBar) View() string                                        { return "" }
func (p *NullProgressBar) Error(args ...any)                                   {}
