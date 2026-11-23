package jobwatch

import (
	"strings"
	"unicode"
)

// Keyword sets are Go constants in v1 — no config knobs until one is needed
// (same rule as ociwatch thresholds, design 5.3). Single words match as
// whole tokens, so "ai" never hits "chairman" and "java" never hits
// "javascript"; phrases (containing a space) match as substrings of the
// normalized text.

// backendKeywords: one hit on title or skill tags → Backend section
// (draft sets from the design; the builder may refine wording).
var backendKeywords = []string{
	"backend", "golang", "go", "python", "node", "java", "net", "php",
	"laravel", "spring", "rust", "kotlin", "back end",
}

// aiDataKeywords: one hit → AI · Data section.
var aiDataKeywords = []string{
	"ai", "machine learning", "deep learning", "llm", "nlp",
	"computer vision", "data engineer", "data science", "data analyst",
	"analytics", "big data", "mlops", "tensorflow", "pytorch",
}

// financeKeywords: generic sector terms, matched over company + title +
// skills. Diacritic-free variants are listed too: boards are inconsistent
// about Vietnamese accents (design 5.3).
var financeKeywords = []string{
	"bank", "banking", "ngân hàng", "ngan hang", "chứng khoán",
	"chung khoan", "securities", "fintech", "finance", "tài chính",
	"tai chinh", "fund", "quỹ", "quy", "đầu tư", "dau tu", "investment",
	"capital", "forex", "crypto",
}

// financeBrands: known VN finance/fintech companies, matched as whole tokens
// against company and title (ITviec reports company as "N/A" with the name
// embedded in the title) — never against skills (design 5.3). "mb bank" is
// a phrase: it matches via the normalized-text path in anyKeyword.
var financeBrands = []string{
	"momo", "vnpay", "zalopay", "moca", "techcombank", "vpbank", "tpbank",
	"vietcombank", "bidv", "vietinbank", "sacombank", "hdbank", "mb bank",
	"ssi", "vndirect", "vietcap", "tcbs", "f88",
}

// Classify sets the Backend / AIData / Finance flags. Matching uses the
// title and the skill tags — never the description, which would flag every
// JD that merely mentions a bank client (design 5.3).
func Classify(j *Job) {
	text := j.Title
	if len(j.Skills) > 0 {
		text += " " + strings.Join(j.Skills, " ")
	}
	norm, tokens := analyze(text)
	j.Backend = anyKeyword(norm, tokens, backendKeywords)
	j.AIData = anyKeyword(norm, tokens, aiDataKeywords)

	finNorm, finTokens := analyze(j.Company + " " + text)
	// Brands match company + title — ITviec often reports company as "N/A"
	// and embeds it in the title ("... — M_Service (MoMo)") — but never
	// skills, which carry generic domain tags like "Banking" (design 5.3).
	brandNorm := normalizeText(j.Company + " " + j.Title)
	j.Finance = anyKeyword(finNorm, finTokens, financeKeywords) ||
		anyKeyword(brandNorm, tokenSet(brandNorm), financeBrands)
}

// analyze returns the normalized text plus its whole-word token set.
// Normalization: lowercase, '-' → space, whitespace collapsed.
func analyze(s string) (string, map[string]bool) {
	norm := normalizeText(s)
	return norm, tokenSet(norm)
}

func normalizeText(s string) string {
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, "-", " ")
	return strings.Join(strings.Fields(s), " ")
}

// tokenSet splits on whitespace and trims punctuation off each edge, so
// "(golang)," yields "golang" (design 5.3).
func tokenSet(norm string) map[string]bool {
	set := make(map[string]bool)
	for _, f := range strings.Fields(norm) {
		if t := trimToken(f); t != "" {
			set[t] = true
		}
	}
	return set
}

func trimToken(s string) string {
	runes := []rune(s)
	for len(runes) > 0 && !wordRune(runes[0]) {
		runes = runes[1:]
	}
	for len(runes) > 0 && !wordRune(runes[len(runes)-1]) {
		runes = runes[:len(runes)-1]
	}
	return string(runes)
}

func wordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}

// anyKeyword reports whether any keyword hits. A keyword containing a space
// is matched as a phrase (substring of the normalized text); anything else
// must equal a whole token (design 5.3).
func anyKeyword(norm string, tokens map[string]bool, keywords []string) bool {
	for _, kw := range keywords {
		if strings.Contains(kw, " ") {
			if norm != "" && strings.Contains(norm, kw) {
				return true
			}
		} else if tokens[kw] {
			return true
		}
	}
	return false
}

// linkKey is the primary dedup key: the link with any "#fragment" stripped —
// goodjobs links sometimes carry a "#rc=…" suffix that varies per listing
// page (design 5.2).
func linkKey(link string) string {
	if i := strings.Index(link, "#"); i >= 0 {
		link = link[:i]
	}
	return strings.TrimSpace(link)
}

// roleKey is the secondary dedup key: same company + same normalized title
// collapses cross-board reposts of one role (design 5.2).
func roleKey(company, title string) string {
	return normalizeText(company) + "|" + normalizeText(title)
}
