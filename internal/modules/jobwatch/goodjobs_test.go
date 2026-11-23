package jobwatch

import (
	"testing"
)

// parseGoodjobs is pure; the network is only exercised in the manual smoke
// test (design 7). Sample fields come from the live check on 2026-11-23.
func TestParseGoodjobs(t *testing.T) {
	body := []byte(`[
  {
    "title": "AI Agent Engineer",
    "company": "bTaskee",
    "location": "Ho Chi Minh City, Vietnam",
    "link": "https://linkedin.com/jobs/view/ai-agent-engineer-1",
    "source": "LinkedIn",
    "salary": "",
    "posted": "15 minutes ago",
    "posted_date": "2026-11-23",
    "posted_ts": 1795392000.0,
    "skills": ["Python", "Machine Learning", "LLM", "RAG"],
    "description": "<p>very long html, must be ignored</p>",
    "_cache_key": "jobs:ai engineer:ho chi minh city"
  },
  {
    "title": "Senior Backend Engineer (Golang) — M_Service (MoMo)",
    "company": "N/A",
    "location": "Hồ Chí Minh",
    "link": "https://itviec.com/it-jobs/senior-backend-engineer#rc=job_list__item",
    "source": "ITViec",
    "salary": "Up to 2,500 USD",
    "skills": ["Go"],
    "description": "ignored"
  },
  {"title": "", "company": "X", "link": "https://x/1", "source": "TopCV"}
]`)
	jobs, err := parseGoodjobs(body)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(jobs) != 2 {
		t.Fatalf("want 2 usable jobs, got %d", len(jobs))
	}

	first := jobs[0]
	if first.Company != "bTaskee" || !first.AIData {
		t.Fatalf("first job misclassified: %+v", first)
	}
	// Python in the skills also hits the Backend set — the classifier marks
	// both, and the digest shows such jobs under AI · Data (digest.go).
	if !first.Backend {
		t.Fatalf("python skill should also mark backend: %+v", first)
	}
	if !first.Wanted() {
		t.Fatal("AI job must be wanted")
	}
	// description is never decoded → cannot leak into state or archive.
	if first.Skills[0] != "Python" {
		t.Fatalf("skills not parsed: %+v", first)
	}

	second := jobs[1]
	if second.Company != "" {
		t.Fatalf(`"N/A" company must normalize to empty: %q`, second.Company)
	}
	if !second.Backend {
		t.Fatalf("golang job must be backend: %+v", second)
	}
	// The title carries M_Service (MoMo) → finance flag, even though the
	// company field was "N/A".
	if !second.Finance {
		t.Fatalf("momo in title must set finance: %+v", second)
	}
	if second.Link == "" || second.Link != "https://itviec.com/it-jobs/senior-backend-engineer#rc=job_list__item" {
		// link is kept verbatim for display; the fragment is only stripped
		// in the dedup key (match.go linkKey).
		t.Fatalf("link must be kept verbatim: %q", second.Link)
	}
}

func TestParseGoodjobsRejectsGarbage(t *testing.T) {
	if _, err := parseGoodjobs([]byte(`{"not":"an array"}`)); err == nil {
		t.Fatal("object instead of array must error")
	}
}
