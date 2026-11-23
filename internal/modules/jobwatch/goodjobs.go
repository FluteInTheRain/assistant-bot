package jobwatch

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Goodjobs is the v1 JobSource: one GET against goodjobs.io.vn's public
// /recent-jobs endpoint covers 13+ VN boards, already merged and deduped
// upstream by link (design 3, 4).
type Goodjobs struct {
	// BaseURL is "" → https://api.goodjobs.io.vn.
	BaseURL string
	// Client is nil → a default http.Client.
	Client *http.Client
}

const defaultGoodjobsURL = "https://api.goodjobs.io.vn/recent-jobs"

// userAgent identifies the bot honestly: one personal digest a day is the
// entire load, and the operator of the service should be able to see who
// is asking (design 5.2).
const userAgent = "assistant-bot/1.1 (personal Discord digest bot; +https://github.com/FluteInTheRain/assistant-bot)"

// goodjobsJob mirrors the response fields the bot uses. Unknown fields are
// ignored; description is never decoded (design 5.2).
type goodjobsJob struct {
	Title    string   `json:"title"`
	Company  string   `json:"company"`
	Location string   `json:"location"`
	Link     string   `json:"link"`
	Source   string   `json:"source"`
	Salary   string   `json:"salary"`
	Skills   []string `json:"skills"`
}

// fetchTimeout bounds one poll (design 5.2).
const fetchTimeout = 30 * time.Second

func (g *Goodjobs) Fetch(ctx context.Context) ([]Job, error) {
	url := g.BaseURL
	if url == "" {
		url = defaultGoodjobsURL
	}
	client := g.Client
	if client == nil {
		client = &http.Client{}
	}
	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("goodjobs request: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("goodjobs fetch: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("goodjobs status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20)) // a few MB in practice
	if err != nil {
		return nil, fmt.Errorf("goodjobs read: %w", err)
	}
	jobs, err := parseGoodjobs(body)
	if err != nil {
		return nil, fmt.Errorf("goodjobs decode: %w", err)
	}
	return jobs, nil
}

// parseGoodjobs decodes one /recent-jobs response. Pure, so the decode is
// unit-tested without the network (design 7).
func parseGoodjobs(body []byte) ([]Job, error) {
	var raw []goodjobsJob
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}
	jobs := make([]Job, 0, len(raw))
	for _, r := range raw {
		if r.Link == "" || r.Title == "" {
			continue // not a usable posting
		}
		j := Job{
			Title:    strings.TrimSpace(r.Title),
			Company:  strings.TrimSpace(r.Company),
			Location: strings.TrimSpace(r.Location),
			Link:     strings.TrimSpace(r.Link),
			Source:   strings.TrimSpace(r.Source),
			Salary:   strings.TrimSpace(r.Salary),
			Skills:   r.Skills,
		}
		// "N/A" means the board did not know the company (design 3).
		if strings.EqualFold(j.Company, "n/a") {
			j.Company = ""
		}
		Classify(&j)
		jobs = append(jobs, j)
	}
	return jobs, nil
}
