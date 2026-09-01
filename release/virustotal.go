package release

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/joshsukhdeo/gh-install/config"
	"github.com/pterm/pterm"
)

var vtBaseURL = "https://www.virustotal.com/api/v3"
var vtPollDelay = 15 * time.Second


func doVTRequestWithRetry(client *http.Client, req *http.Request) (*http.Response, error) {
	for {
		resp, err := client.Do(req)
		if err != nil {
			return resp, err
		}
		if resp.StatusCode == http.StatusTooManyRequests {
			resp.Body.Close()
			log.Warn().Msg("VirusTotal rate limit (429) reached. Throttling for 15s...")
			time.Sleep(15 * time.Second)
			continue
		}
		return resp, nil
	}
}

func verifyHashWithVirusTotal(hash string, filePath string, apiKey string, interactive bool, skipSandbox bool) error {
RetryVT:
	if apiKey == "" {
		return nil
	}

	url := fmt.Sprintf("%s/files/%s", vtBaseURL, hash)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("failed to create virustotal request: %w", err)
	}

	req.Header.Set("x-apikey", apiKey)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := doVTRequestWithRetry(client, req)
	if err != nil {
		return fmt.Errorf("virustotal api request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		if !interactive {
			return fmt.Errorf("invalid virustotal api key")
		}
		pterm.Warning.Println("Invalid VirusTotal API Key detected.")
		if os.Getenv("GH_INSTALL_VT_API_KEY") != "" {
			pterm.Warning.Println("Note: Your GH_INSTALL_VT_API_KEY environment variable is currently overriding the config. Updating the config below will NOT take effect until you unset the environment variable.")
		}
		
		var confirm string
		fmt.Printf("\nDo you want to update your API key in config? (y/N): ")
		fmt.Scanln(&confirm)
		if strings.ToLower(strings.TrimSpace(confirm)) == "y" {
			fmt.Printf("Enter new VirusTotal API Key: ")
			var newKey string
			fmt.Scanln(&newKey)
			if newKey != "" {
				apiKey = strings.TrimSpace(newKey)
				// Update config
				cfg, _ := config.LoadConfig()
				if cfg == nil {
					cfg = &config.Config{}
				}
				cfg.VTApiKey = apiKey
				config.SaveConfig(cfg)
				goto RetryVT
			}
		} else {
			fmt.Printf("Continue installation without VirusTotal? (y/N): ")
			fmt.Scanln(&confirm)
			if strings.ToLower(strings.TrimSpace(confirm)) == "y" {
				return nil
			}
			return fmt.Errorf("installation aborted due to invalid vt api key")
		}
	}

	if resp.StatusCode == http.StatusNotFound {
		if skipSandbox {
			log.Warn().Str("hash", hash).Msg("VirusTotal has no record of this hash. Bypassing sandbox (--skip-vt-sandbox).")
			return nil
		}
		if interactive {
			// ponytail: directly relying on fmt to avoid passing the GithubRelease struct just for prompts
			var confirm string
			fmt.Printf("\nVirusTotal has no record of %s. Upload to sandbox and wait for analysis? [y/N]: ", filepath.Base(filePath))
			fmt.Scanln(&confirm)
			if strings.ToLower(strings.TrimSpace(confirm)) != "y" {
				log.Warn().Str("hash", hash).Msg("User declined VT sandbox upload. Bypassing check.")
				return nil
			}
		}
		log.Info().Str("file", filepath.Base(filePath)).Msg("Uploading file to VirusTotal sandbox...")
		return uploadToVirusTotal(filePath, apiKey)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("virustotal api returned status %d", resp.StatusCode)
	}

	var result struct {
		Data struct {
			Attributes struct {
				LastAnalysisStats struct {
					Malicious int `json:"malicious"`
				} `json:"last_analysis_stats"`
			} `json:"attributes"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("failed to decode virustotal response: %w", err)
	}

	maliciousCount := result.Data.Attributes.LastAnalysisStats.Malicious
	if maliciousCount > 0 {
		return fmt.Errorf("virustotal blocked installation: %d engines detected hash %s as malicious", maliciousCount, hash)
	}

	log.Info().Str("hash", hash).Msg("VirusTotal confirms file is clean.")
	return nil
}

func calculateSHA256(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return "", err
	}

	return hex.EncodeToString(hasher.Sum(nil)), nil
}

func uploadToVirusTotal(filePath string, apiKey string) error {
	file, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return err
	}

	uploadURL := fmt.Sprintf("%s/files", vtBaseURL)

	if info.Size() > 32*1024*1024 {
		req, _ := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/files/upload_url", vtBaseURL), nil)
		req.Header.Set("x-apikey", apiKey)
		client := &http.Client{Timeout: 10 * time.Second}
		resp, err := doVTRequestWithRetry(client, req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()

		var result struct {
			Data string `json:"data"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&result)
		if result.Data == "" {
			return fmt.Errorf("failed to get upload_url for large file")
		}
		uploadURL = result.Data
	}

	pr, pw := io.Pipe()
	writer := multipart.NewWriter(pw)

	go func() {
		defer pw.Close()
		part, err := writer.CreateFormFile("file", filepath.Base(filePath))
		if err == nil {
			io.Copy(part, file)
		}
		writer.Close()
	}()

	req, _ := http.NewRequest(http.MethodPost, uploadURL, pr)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("x-apikey", apiKey)

	client := &http.Client{Timeout: 10 * time.Minute}
	resp, err := doVTRequestWithRetry(client, req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("virustotal upload failed with status %d", resp.StatusCode)
	}

	var result struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&result)

	log.Info().Str("analysis_id", result.Data.ID).Msg("Upload complete. Waiting for sandbox analysis...")
	return pollVirusTotalAnalysis(result.Data.ID, apiKey)
}

func pollVirusTotalAnalysis(analysisID string, apiKey string) error {
	client := &http.Client{Timeout: 10 * time.Second}
	for {
		time.Sleep(vtPollDelay) // ponytail: naive polling without backoff

		req, _ := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/analyses/%s", vtBaseURL, analysisID), nil)
		req.Header.Set("x-apikey", apiKey)

		resp, err := doVTRequestWithRetry(client, req)
		if err != nil {
			return err
		}

		var result struct {
			Data struct {
				Attributes struct {
					Status string `json:"status"`
					Stats  struct {
						Malicious int `json:"malicious"`
					} `json:"stats"`
				} `json:"attributes"`
			} `json:"data"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&result)
		resp.Body.Close()

		if result.Data.Attributes.Status == "completed" {
			malicious := result.Data.Attributes.Stats.Malicious
			if malicious > 0 {
				return fmt.Errorf("virustotal sandbox blocked installation: %d engines detected file as malicious", malicious)
			}
			log.Info().Msg("VirusTotal sandbox analysis confirms file is clean.")
			return nil
		}
		log.Info().Msg("Sandbox analysis still in progress...")
	}
}
