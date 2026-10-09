package ui

import "fmt"

// ConveyorUI displays a package riding a conveyor belt animation.
type ConveyorUI struct {
	Repo         string // repository name, exported for struct literal init
	DisableIcons bool
	step         int
	target       string
	frame        int
}

// Start initializes the conveyor animation.
func (c *ConveyorUI) Start() {
	// nop - animation ticks on every View() call
}

// Stop stops the animation (no-op for conveyor).
func (c *ConveyorUI) Stop() {}

// Pause pauses the animation.
func (c *ConveyorUI) Pause() {}

// Resume resumes the animation.
func (c *ConveyorUI) Resume() {}

// Update updates the conveyor state (step/target).
func (c *ConveyorUI) Update(step int, a string, b string, cStr string, target string, e string) {
	c.step = step
	if target != "" {
		c.target = target
	}
}

// UpdateSymlink updates the symlink path (conveyor has no symlinks).
func (c *ConveyorUI) UpdateSymlink(symlink string) {}

// AddAsset adds an asset to the conveyor (conveyor shows one package at a time).
func (c *ConveyorUI) AddAsset(name string, fullName string, symlink string, installCmd string) {}

// SetCurrentAsset sets the current asset index (conveyor shows one package).
func (c *ConveyorUI) SetCurrentAsset(index int) {}

// SetAssetInstallCmd sets the install command (conveyor has no explicit cmd).
func (c *ConveyorUI) SetAssetInstallCmd(installCmd string) {}

// View returns the conveyor belt rendering.
func (c *ConveyorUI) View() string {
	// belt is 40 chars wide; package position cycles with frame
	packagePos := c.frame % 40
	box := "📦"
	if c.DisableIcons {
		box = "[PKG]"
	}

	// Simple scrolling belt; package moves across
	belt := "─────────────────────────────────────────────────"

	// Animate by shifting package position each frame
	shifted := belt[:packagePos] + box + belt[packagePos+len(box):]

	return fmt.Sprintf("\n%s\nInstalling %s -> %s (Step %d)...\n", shifted, c.Repo, c.target, c.step)
}

// WaitForAnimation waits for the animation to finish (conveyor finishes immediately).
func (c *ConveyorUI) WaitForAnimation() {}

// Error handles installation error for ConveyorUI.
func (c *ConveyorUI) Error(args ...any) {}

// NewConveyorUI creates a new ConveyorUI.
func NewConveyorUI(repo string) *ConveyorUI {
	return &ConveyorUI{Repo: repo}
}
