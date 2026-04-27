package text

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"github.com/andrewneudegg/lab/pkg/tool"
)

func Register(reg *tool.Registry) error {
	return reg.Register(CorrectTool{})
}

func schema(v string) json.RawMessage { return json.RawMessage(v) }

type CorrectTool struct{}

func (CorrectTool) Name() string { return "text.correct" }
func (CorrectTool) Description() string {
	return "Correct short English spelling and light grammar, and return search-query variants for typo-prone user text."
}
func (CorrectTool) Schema() json.RawMessage {
	return schema(`{"type":"object","required":["text"],"properties":{"text":{"type":"string"},"mode":{"type":"string","enum":["spelling","grammar","search_query","all"],"description":"search_query keeps the result query-like; all applies spelling and light grammar"},"locale":{"type":"string","enum":["en-US","en-GB"],"description":"preferred English spelling for ambiguous variants"},"max_variants":{"type":"integer","minimum":1,"maximum":8}}}`)
}
func (CorrectTool) Risk() tool.RiskLevel { return tool.RiskReadOnly }
func (CorrectTool) Run(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	var req struct {
		Text        string `json:"text"`
		Mode        string `json:"mode"`
		Locale      string `json:"locale"`
		MaxVariants int    `json:"max_variants"`
	}
	if err := json.Unmarshal(input, &req); err != nil {
		return nil, err
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	req.Text = compactWhitespace(req.Text)
	if req.Text == "" {
		return nil, fmt.Errorf("text is required")
	}
	if len(req.Text) > 2000 {
		return nil, fmt.Errorf("text must be 2000 characters or fewer")
	}
	mode := strings.ToLower(strings.TrimSpace(req.Mode))
	if mode == "" {
		mode = "all"
	}
	if mode != "spelling" && mode != "grammar" && mode != "search_query" && mode != "all" {
		return nil, fmt.Errorf("mode must be spelling, grammar, search_query, or all")
	}
	locale := strings.TrimSpace(req.Locale)
	if locale == "" {
		locale = "en-US"
	}
	if locale != "en-US" && locale != "en-GB" {
		return nil, fmt.Errorf("locale must be en-US or en-GB")
	}
	maxVariants := req.MaxVariants
	if maxVariants <= 0 {
		maxVariants = 4
	}
	if maxVariants > 8 {
		maxVariants = 8
	}

	result := correctText(req.Text, mode, locale, maxVariants)
	return json.Marshal(result)
}

type Result struct {
	Text          string       `json:"text"`
	Corrected     string       `json:"corrected_text"`
	Mode          string       `json:"mode"`
	Locale        string       `json:"locale"`
	Changed       bool         `json:"changed"`
	Corrections   []Correction `json:"corrections"`
	Alternatives  []string     `json:"alternatives,omitempty"`
	SearchQueries []string     `json:"search_queries,omitempty"`
	Notes         []string     `json:"notes,omitempty"`
}

type Correction struct {
	Original    string `json:"original"`
	Replacement string `json:"replacement"`
	Kind        string `json:"kind"`
	Reason      string `json:"reason"`
}

type token struct {
	text string
	word bool
}

type correctionRule struct {
	replacement string
	alternates  []string
	reason      string
}

func correctText(input, mode, locale string, maxVariants int) Result {
	tokens := tokenize(input)
	corrections := make([]Correction, 0, 4)
	alternateRules := make(map[int][]string)
	if mode == "spelling" || mode == "search_query" || mode == "all" {
		applySpelling(tokens, locale, &corrections, alternateRules)
	}
	if mode == "grammar" || mode == "all" {
		applyGrammar(tokens, &corrections)
	}
	corrected := compactWhitespace(renderTokens(tokens))
	if mode == "search_query" {
		corrected = strings.Trim(corrected, ".!?")
	}
	alternatives := buildAlternatives(tokens, alternateRules, maxVariants)
	searchQueries := buildSearchQueries(input, corrected, alternatives, maxVariants)
	return Result{
		Text:          input,
		Corrected:     corrected,
		Mode:          mode,
		Locale:        locale,
		Changed:       corrected != input,
		Corrections:   corrections,
		Alternatives:  alternatives,
		SearchQueries: searchQueries,
		Notes: []string{
			"Lightweight deterministic correction; preserve exact code symbols, names, and quoted strings when precision matters.",
		},
	}
}

func applySpelling(tokens []token, locale string, corrections *[]Correction, alternateRules map[int][]string) {
	for i := range tokens {
		if !tokens[i].word {
			continue
		}
		lower := strings.ToLower(tokens[i].text)
		rule, ok := commonSpellingCorrections[lower]
		if !ok {
			continue
		}
		replacement := rule.replacement
		if locale == "en-GB" {
			if gb, ok := britishPreferred[replacement]; ok {
				replacement = gb
			}
		}
		if replacement == "" || strings.EqualFold(tokens[i].text, replacement) {
			continue
		}
		original := tokens[i].text
		tokens[i].text = matchCase(original, replacement)
		*corrections = append(*corrections, Correction{
			Original:    original,
			Replacement: tokens[i].text,
			Kind:        "spelling",
			Reason:      rule.reason,
		})
		if len(rule.alternates) > 0 {
			alternateRules[i] = rule.alternates
		}
	}
}

func applyGrammar(tokens []token, corrections *[]Correction) {
	for i := range tokens {
		if !tokens[i].word {
			continue
		}
		if tokens[i].text == "i" {
			tokens[i].text = "I"
			*corrections = append(*corrections, Correction{Original: "i", Replacement: "I", Kind: "grammar", Reason: "capitalise the first-person pronoun"})
			continue
		}
		lower := strings.ToLower(tokens[i].text)
		if lower != "a" && lower != "an" {
			continue
		}
		next := nextWord(tokens, i+1)
		if next == "" {
			continue
		}
		want := "a"
		if startsWithVowelSound(next) {
			want = "an"
		}
		if lower == want {
			continue
		}
		original := tokens[i].text
		tokens[i].text = matchCase(original, want)
		*corrections = append(*corrections, Correction{
			Original:    original,
			Replacement: tokens[i].text,
			Kind:        "grammar",
			Reason:      "choose a or an from the following word",
		})
	}
}

func buildAlternatives(tokens []token, alternateRules map[int][]string, max int) []string {
	seen := map[string]bool{}
	base := strings.ToLower(compactWhitespace(renderTokens(tokens)))
	var out []string
	for index, alternates := range alternateRules {
		for _, alternate := range alternates {
			if alternate == "" {
				continue
			}
			clone := append([]token(nil), tokens...)
			clone[index].text = matchCase(tokens[index].text, alternate)
			value := compactWhitespace(renderTokens(clone))
			key := strings.ToLower(value)
			if value == "" || key == base || seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, value)
			if len(out) >= max {
				return out
			}
		}
	}
	return out
}

func buildSearchQueries(input, corrected string, alternatives []string, max int) []string {
	seen := map[string]bool{}
	out := make([]string, 0, max)
	add := func(value string) {
		value = compactWhitespace(strings.Trim(value, ".!?"))
		key := strings.ToLower(value)
		if value == "" || seen[key] || len(out) >= max {
			return
		}
		seen[key] = true
		out = append(out, value)
	}
	add(corrected)
	for _, alternative := range alternatives {
		add(alternative)
	}
	add(input)
	return out
}

func tokenize(input string) []token {
	matches := tokenRE.FindAllString(input, -1)
	out := make([]token, 0, len(matches))
	for _, match := range matches {
		out = append(out, token{text: match, word: wordRE.MatchString(match)})
	}
	return out
}

func renderTokens(tokens []token) string {
	var b strings.Builder
	for i, tok := range tokens {
		if tok.text == "" {
			continue
		}
		if i > 0 && needsSpace(tokens[i-1], tok) {
			b.WriteByte(' ')
		}
		b.WriteString(tok.text)
	}
	return b.String()
}

func needsSpace(prev, cur token) bool {
	if cur.text == "." || cur.text == "," || cur.text == ":" || cur.text == ";" || cur.text == "!" || cur.text == "?" || cur.text == ")" || cur.text == "]" {
		return false
	}
	if prev.text == "(" || prev.text == "[" {
		return false
	}
	return true
}

func nextWord(tokens []token, start int) string {
	for i := start; i < len(tokens); i++ {
		if tokens[i].word {
			return strings.ToLower(tokens[i].text)
		}
	}
	return ""
}

func startsWithVowelSound(word string) bool {
	word = strings.ToLower(strings.TrimSpace(word))
	if word == "" {
		return false
	}
	if strings.HasPrefix(word, "uni") || strings.HasPrefix(word, "use") || strings.HasPrefix(word, "user") || strings.HasPrefix(word, "one") {
		return false
	}
	if strings.HasPrefix(word, "hour") || strings.HasPrefix(word, "honest") || strings.HasPrefix(word, "heir") {
		return true
	}
	r := rune(word[0])
	return r == 'a' || r == 'e' || r == 'i' || r == 'o' || r == 'u'
}

func matchCase(original, replacement string) string {
	if original == strings.ToUpper(original) {
		return strings.ToUpper(replacement)
	}
	runes := []rune(original)
	if len(runes) > 0 && unicode.IsUpper(runes[0]) {
		return strings.ToUpper(replacement[:1]) + replacement[1:]
	}
	return replacement
}

func compactWhitespace(s string) string {
	return strings.TrimSpace(strings.Join(strings.Fields(s), " "))
}

var (
	tokenRE = regexp.MustCompile(`[A-Za-z]+(?:'[A-Za-z]+)?|\d+|[^\sA-Za-z\d]+`)
	wordRE  = regexp.MustCompile(`^[A-Za-z]+(?:'[A-Za-z]+)?$`)
)

var commonSpellingCorrections = map[string]correctionRule{
	"acommodate":    {replacement: "accommodate", reason: "common misspelling"},
	"adress":        {replacement: "address", reason: "common misspelling"},
	"arguement":     {replacement: "argument", reason: "common misspelling"},
	"beleive":       {replacement: "believe", reason: "common misspelling"},
	"calender":      {replacement: "calendar", reason: "common misspelling"},
	"concensus":     {replacement: "consensus", reason: "common misspelling"},
	"definately":    {replacement: "definitely", reason: "common misspelling"},
	"dependancy":    {replacement: "dependency", reason: "common misspelling"},
	"dependancies":  {replacement: "dependencies", reason: "common misspelling"},
	"documenation":  {replacement: "documentation", reason: "common misspelling"},
	"documentaion":  {replacement: "documentation", reason: "common misspelling"},
	"enviroment":    {replacement: "environment", reason: "common misspelling"},
	"enviornment":   {replacement: "environment", reason: "common misspelling"},
	"exmaple":       {replacement: "example", reason: "common misspelling"},
	"grammer":       {replacement: "grammar", reason: "common misspelling"},
	"implmentation": {replacement: "implementation", reason: "common misspelling"},
	"langauge":      {replacement: "language", reason: "common misspelling"},
	"occured":       {replacement: "occurred", reason: "common misspelling"},
	"offical":       {replacement: "official", reason: "common misspelling"},
	"pijama":        {replacement: "pajama", alternates: []string{"pyjama"}, reason: "common spelling variant"},
	"pijamas":       {replacement: "pajamas", alternates: []string{"pyjamas"}, reason: "common spelling variant"},
	"recieve":       {replacement: "receive", reason: "common misspelling"},
	"recieved":      {replacement: "received", reason: "common misspelling"},
	"recomend":      {replacement: "recommend", reason: "common misspelling"},
	"seperate":      {replacement: "separate", reason: "common misspelling"},
	"teh":           {replacement: "the", reason: "common typo"},
	"wierd":         {replacement: "weird", reason: "common misspelling"},
}

var britishPreferred = map[string]string{
	"behavior": "behaviour",
	"color":    "colour",
	"favorite": "favourite",
	"honor":    "honour",
	"pajama":   "pyjama",
	"pajamas":  "pyjamas",
}
