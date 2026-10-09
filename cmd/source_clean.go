package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/charmbracelet/log"
	"github.com/joshsukhdeo/gh-pt/safety"
)

// CleanSourceBuilds removes build artifacts for a repository or all repositories.
func CleanSourceBuilds(repository string, all bool) error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("could not determine home directory: %w", err)
	}

	buildsDir := filepath.Join(homeDir, "builds")

	if all {
		// Clean all build artifacts
		log.Info("cleaning all source build artifacts", "directory", buildsDir)
		if err := safety.RemoveAll(buildsDir); err != nil {
			return fmt.Errorf("failed to remove builds directory: %w", err)
		}
		log.Info("successfully cleaned all source build artifacts")
		return nil
	}

	if repository == "" {
		return fmt.Errorf("repository argument is required (or use --all to clean all builds)")
	}

	// Clean specific repository
	parts := splitRepository(repository)
	if len(parts) != 2 {
		return fmt.Errorf("invalid repository format: %s (expected owner/repo)", repository)
	}

	repoName := parts[1]
	repoDir := filepath.Join(buildsDir, repoName)

	if _, err := os.Stat(repoDir); os.IsNotExist(err) {
		log.Info("no build artifacts found for repository", "repository", repository, "directory", repoDir)
		return nil
	}

	log.Info("cleaning source build artifacts", "repository", repository, "directory", repoDir)
	if err := safety.RemoveAll(repoDir); err != nil {
		return fmt.Errorf("failed to remove build directory: %w", err)
	}

	log.Info("successfully cleaned source build artifacts", "repository", repository)
	return nil
}

// splitRepository splits a repository string into owner and repo parts.
func splitRepository(repo string) []string {
	parts := make([]string, 0, 2)
	current := ""
	for _, ch := range repo {
		if ch == '/' {
			if current != "" {
				parts = append(parts, current)
				current = ""
			}
		} else {
			current += string(ch)
		}
	}
	if current != "" {
		parts = append(parts, current)
	}
	return parts
}
