package release

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/rs/zerolog/log"
)

var vtBaseURL = "https://www.virustotal.com/api/v3"

func verifyHashWithVirusTotal(hash string, apiKey string) error {
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
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("virustotal api request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		log.Warn().Str("hash", hash).Msg("VirusTotal has no record of this hash. Bypassing check for zero-day release.")
		return nil
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
