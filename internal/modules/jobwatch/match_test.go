package jobwatch

import (
	"testing"
)

func TestClassifyBackend(t *testing.T) {
	cases := []struct {
		name string
		job  Job
		want Job
	}{
		{
			name: "golang backend job",
			job:  Job{Title: "Senior Backend Engineer (Golang)", Skills: []string{"Python"}},
			want: Job{Backend: true},
		},
		{
			name: "java developer matches, javascript does not",
			job:  Job{Title: "Java Developer"},
			want: Job{Backend: true},
		},
		{
			name: "javascript developer is not backend (token match)",
			job:  Job{Title: "JavaScript Developer"},
			want: Job{},
		},
		{
			name: "frontend is not wanted",
			job:  Job{Title: "Frontend Developer (React)", Skills: []string{"JavaScript"}},
			want: Job{},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := c.job
			Classify(&got)
			if got.Backend != c.want.Backend || got.AIData != c.want.AIData || got.Finance != c.want.Finance {
				t.Fatalf("got %+v, want %+v", got, c.want)
			}
		})
	}
}

func TestClassifyAIData(t *testing.T) {
	cases := []struct {
		name string
		job  Job
		want bool
	}{
		{"AI in title is a token", Job{Title: "AI Agent Engineer"}, true},
		{"ai inside chairman does not match", Job{Title: "Chairman of the Board"}, false},
		{"machine learning phrase", Job{Title: "Machine Learning & Industrial Data Specialist"}, true},
		{"data engineer phrase", Job{Title: "Data Engineer (Fintech)"}, true},
		{"data entry is not wanted", Job{Title: "Data Entry Clerk"}, false},
		{"skill tag counts", Job{Title: "Researcher", Skills: []string{"PyTorch", "NLP"}}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := c.job
			Classify(&got)
			if got.AIData != c.want {
				t.Fatalf("AIData = %v, want %v (job %+v)", got.AIData, c.want, got)
			}
		})
	}
}

func TestClassifyFinance(t *testing.T) {
	cases := []struct {
		name string
		job  Job
		want bool
	}{
		{"generic term in company", Job{Title: "Backend Developer", Company: "MB Bank"}, true},
		{"vietnamese diacritic phrase", Job{Title: "Kế toán", Company: "Công ty tài chính"}, true}, // finance via tài chính
		{"brand token in company", Job{Title: "Data Analyst", Company: "SSI Securities Corporation"}, true},
		{"brand as substring of another word does not match", Job{Title: "Data Analyst", Company: "Mission Group"}, false},
		{"generic term in title", Job{Title: "Fintech Backend Engineer", Company: "Startup X"}, true},
		{"non-finance company", Job{Title: "Backend Developer", Company: "FPT Software"}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := c.job
			Classify(&got)
			if got.Finance != c.want {
				t.Fatalf("Finance = %v, want %v (job %+v)", got.Finance, c.want, got)
			}
		})
	}
}

func TestWanted(t *testing.T) {
	j := Job{Title: "Backend Developer", Company: "FPT"}
	Classify(&j)
	if !j.Wanted() {
		t.Fatal("backend job must be wanted")
	}
	n := Job{Title: "Marketing Manager"}
	Classify(&n)
	if n.Wanted() {
		t.Fatal("marketing job must not be wanted")
	}
}

func TestLinkKeyStripsFragment(t *testing.T) {
	cases := map[string]string{
		"https://jobsgo.vn/x.html#rc=job_list__item": "https://jobsgo.vn/x.html",
		"https://itviec.com/it-jobs/a":               "https://itviec.com/it-jobs/a",
	}
	for in, want := range cases {
		if got := linkKey(in); got != want {
			t.Errorf("linkKey(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRoleKey(t *testing.T) {
	a := roleKey("  FPT   Software ", "Senior-Backend Developer")
	b := roleKey("fpt software", "senior backend developer")
	if a != b {
		t.Fatalf("roleKey must normalize: %q != %q", a, b)
	}
	if a != "fpt software|senior backend developer" {
		t.Fatalf("unexpected key %q", a)
	}
}
