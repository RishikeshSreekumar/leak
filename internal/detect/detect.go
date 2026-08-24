package detect

import (
	"math"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/RishikeshSreekumar/leak/internal/model"
)

// Candidate is a recurring charge Leak believes is a subscription. Nothing is
// written from a Candidate until the user approves it.
type Candidate struct {
	Name        string    `json:"name"`
	Merchant    string    `json:"merchant"`   // normalized grouping key
	Descriptor  string    `json:"descriptor"` // a raw statement line, for context
	Amount      float64   `json:"amount"`     // median charge
	Currency    string    `json:"currency"`
	Cycle       string    `json:"billing_cycle"`
	Category    string    `json:"category,omitempty"`
	Occurrences int       `json:"occurrences"`
	First       time.Time `json:"first_charged"`
	Last        time.Time `json:"last_charged"`
	NextRenewal time.Time `json:"next_renewal"`
	Confidence  float64   `json:"confidence"` // 0..1
}

// Options tunes detection.
type Options struct {
	// MinOccurrences is how many charges a merchant needs before it counts as
	// recurring. Two is enough to see a cadence; three is much safer.
	MinOccurrences int
	// AmountTolerance is the allowed spread around the median charge (0.15 =
	// ±15%), covering taxes, FX drift, and price changes.
	AmountTolerance float64
	// Now anchors the next-renewal projection.
	Now time.Time
}

// withDefaults fills unset options.
func (o Options) withDefaults() Options {
	if o.MinOccurrences <= 0 {
		o.MinOccurrences = 3
	}
	if o.AmountTolerance <= 0 {
		o.AmountTolerance = 0.15
	}
	if o.Now.IsZero() {
		o.Now = time.Now()
	}
	return o
}

// cycleShape describes the interval window that maps to a billing cycle.
type cycleShape struct {
	cycle   string
	lo, hi  float64 // inclusive day range for the median interval
	nominal float64 // canonical length, used to project the next renewal
}

var cycleShapes = []cycleShape{
	{model.CycleWeekly, 5, 9, 7},
	{model.CycleMonthly, 25, 38, 30.44},
	{model.CycleQuarterly, 80, 100, 91.31},
	{model.CycleYearly, 330, 400, 365.25},
}

// Recurring groups statement lines by merchant and returns the groups that
// charge on a steady cadence for a steady amount, most confident first.
func Recurring(txns []Txn, opts Options) []Candidate {
	opts = opts.withDefaults()

	groups := map[string][]Txn{}
	for _, t := range txns {
		key := MerchantKey(t.Description)
		if key == "" {
			continue
		}
		groups[key] = append(groups[key], t)
	}

	var out []Candidate
	for key, ts := range groups {
		if c, ok := candidate(key, ts, opts); ok {
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Confidence != out[j].Confidence {
			return out[i].Confidence > out[j].Confidence
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// candidate evaluates one merchant group.
func candidate(key string, ts []Txn, opts Options) (Candidate, bool) {
	if len(ts) < opts.MinOccurrences {
		return Candidate{}, false
	}
	sort.Slice(ts, func(i, j int) bool { return ts[i].Date.Before(ts[j].Date) })

	// Collapse same-day duplicates: one bill split across two statement lines
	// should not look like a 0-day cadence.
	ts = dedupeByDay(ts)
	if len(ts) < opts.MinOccurrences {
		return Candidate{}, false
	}

	amounts := make([]float64, len(ts))
	for i, t := range ts {
		amounts[i] = t.Amount
	}
	median := medianOf(amounts)
	if median <= 0 {
		return Candidate{}, false
	}
	for _, a := range amounts {
		if math.Abs(a-median)/median > opts.AmountTolerance {
			return Candidate{}, false // variable spend, not a subscription
		}
	}

	gaps := make([]float64, 0, len(ts)-1)
	for i := 1; i < len(ts); i++ {
		gaps = append(gaps, ts[i].Date.Sub(ts[i-1].Date).Hours()/24)
	}
	medGap := medianOf(gaps)
	shape, ok := shapeFor(medGap)
	if !ok {
		return Candidate{}, false
	}
	// Every gap must sit in the same window, so an occasional purchase that
	// happens to average out is rejected.
	for _, g := range gaps {
		if g < shape.lo || g > shape.hi {
			return Candidate{}, false
		}
	}

	last := ts[len(ts)-1]
	next := last.Date.AddDate(0, 0, int(math.Round(shape.nominal)))
	for next.Before(opts.Now) {
		next = next.AddDate(0, 0, int(math.Round(shape.nominal)))
	}

	name, category := prettyName(key)
	return Candidate{
		Name:        name,
		Merchant:    key,
		Descriptor:  ts[len(ts)-1].Description,
		Amount:      round2(median),
		Currency:    ts[len(ts)-1].Currency,
		Cycle:       shape.cycle,
		Category:    category,
		Occurrences: len(ts),
		First:       ts[0].Date,
		Last:        last.Date,
		NextRenewal: next,
		Confidence:  confidence(len(ts), gaps, amounts, median, shape, last.Date, opts.Now),
	}, true
}

// confidence blends how many charges were seen, how regular the gaps are, how
// stable the amount is, and how recent the last charge is. It is a ranking aid
// for the review step, not a probability.
func confidence(n int, gaps, amounts []float64, median float64, shape cycleShape, last, now time.Time) float64 {
	count := math.Min(float64(n)/6, 1) // six charges is as convincing as it gets

	regularity := 1.0
	if len(gaps) > 0 {
		var dev float64
		for _, g := range gaps {
			dev += math.Abs(g-shape.nominal) / shape.nominal
		}
		regularity = math.Max(0, 1-(dev/float64(len(gaps)))*2)
	}

	var spread float64
	for _, a := range amounts {
		spread += math.Abs(a-median) / median
	}
	stability := math.Max(0, 1-(spread/float64(len(amounts)))*4)

	// A charge that stopped long ago is probably already cancelled.
	staleness := now.Sub(last).Hours() / 24 / (shape.nominal * 3)
	recency := math.Max(0, 1-staleness)

	score := 0.35*count + 0.3*regularity + 0.2*stability + 0.15*recency
	return math.Round(math.Min(math.Max(score, 0), 1)*100) / 100
}

func shapeFor(days float64) (cycleShape, bool) {
	for _, s := range cycleShapes {
		if days >= s.lo && days <= s.hi {
			return s, true
		}
	}
	return cycleShape{}, false
}

func dedupeByDay(ts []Txn) []Txn {
	out := ts[:0]
	var lastDay time.Time
	for _, t := range ts {
		if t.Date.Equal(lastDay) {
			continue
		}
		lastDay = t.Date
		out = append(out, t)
	}
	return out
}

// --- merchant normalization ------------------------------------------------

var (
	// Payment-rail prefixes and processor markers that carry no merchant identity.
	noiseTokens = map[string]bool{
		"upi": true, "pos": true, "ach": true, "neft": true, "imps": true, "rtgs": true,
		"autopay": true, "mandate": true, "emandate": true, "si": true, "ecs": true,
		"debit": true, "card": true, "payment": true, "purchase": true, "txn": true,
		"transaction": true, "ref": true, "recurring": true, "bill": true, "billpay": true,
		"paypal": true, "sq": true, "tst": true, "www": true, "com": true, "http": true,
		"https": true, "in": true, "ltd": true, "pvt": true, "inc": true, "llc": true,
		"limited": true, "india": true, "services": true, "technologies": true,
	}
	digitRun    = regexp.MustCompile(`\d`)
	nonAlphaNum = regexp.MustCompile(`[^a-z0-9]+`)
)

// MerchantKey normalizes a statement descriptor down to a stable grouping key.
// A known service collapses to its canonical name, so "UPI/NETFLIX COM/98765/AXIS"
// and "POS 1234 NETFLIX.COM MUMBAI" both group as "netflix" despite the bank
// gluing different rail noise onto each line. Unknown merchants fall back to the
// first two meaningful tokens of the descriptor.
func MerchantKey(desc string) string {
	s := nonAlphaNum.ReplaceAllString(strings.ToLower(desc), " ")
	for _, m := range knownMerchants {
		if strings.Contains(s, m.match) {
			return m.match
		}
	}
	var kept []string
	for _, tok := range strings.Fields(s) {
		if noiseTokens[tok] || len(tok) < 2 {
			continue
		}
		// Reference numbers, dates, card fragments: anything mostly digits.
		if len(digitRun.FindAllString(tok, -1)) > len(tok)/2 {
			continue
		}
		kept = append(kept, tok)
		if len(kept) == 2 { // the identity lives at the front of the descriptor
			break
		}
	}
	return strings.Join(kept, " ")
}

// knownMerchants maps a normalized key fragment to a display name and category,
// so common services come out labelled instead of SHOUTING BANK TEXT.
var knownMerchants = []struct{ match, name, category string }{
	{"netflix", "Netflix", "Entertainment"},
	{"spotify", "Spotify", "Entertainment"},
	{"youtube", "YouTube Premium", "Entertainment"},
	{"hotstar", "Disney+ Hotstar", "Entertainment"},
	{"prime video", "Prime Video", "Entertainment"},
	{"amazon prime", "Amazon Prime", "Entertainment"},
	{"icloud", "iCloud", "Storage"},
	{"apple", "Apple", "Entertainment"},
	{"google one", "Google One", "Storage"},
	{"dropbox", "Dropbox", "Storage"},
	{"backblaze", "Backblaze", "Storage"},
	{"github", "GitHub", "Development"},
	{"gitlab", "GitLab", "Development"},
	{"jetbrains", "JetBrains", "Development"},
	{"vercel", "Vercel", "Development"},
	{"netlify", "Netlify", "Development"},
	{"digitalocean", "DigitalOcean", "Development"},
	{"linode", "Linode", "Development"},
	{"cloudflare", "Cloudflare", "Development"},
	{"heroku", "Heroku", "Development"},
	{"aws", "AWS", "Development"},
	{"anthropic", "Anthropic", "AI"},
	{"claude", "Claude", "AI"},
	{"openai", "OpenAI", "AI"},
	{"chatgpt", "ChatGPT", "AI"},
	{"midjourney", "Midjourney", "AI"},
	{"perplexity", "Perplexity", "AI"},
	{"cursor", "Cursor", "AI"},
	{"notion", "Notion", "Utilities"},
	{"figma", "Figma", "Utilities"},
	{"canva", "Canva", "Utilities"},
	{"slack", "Slack", "Utilities"},
	{"zoom", "Zoom", "Utilities"},
	{"adobe", "Adobe", "Utilities"},
	{"microsoft", "Microsoft 365", "Utilities"},
	{"1password", "1Password", "Utilities"},
	{"nordvpn", "NordVPN", "Utilities"},
	{"proton", "Proton", "Utilities"},
}

// prettyName turns a merchant key into a display name and a category guess.
func prettyName(key string) (string, string) {
	for _, m := range knownMerchants {
		if strings.Contains(key, m.match) {
			return m.name, m.category
		}
	}
	words := strings.Fields(key)
	for i, w := range words {
		words[i] = strings.ToUpper(w[:1]) + w[1:]
	}
	return strings.Join(words, " "), ""
}

func medianOf(vals []float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	s := append([]float64(nil), vals...)
	sort.Float64s(s)
	mid := len(s) / 2
	if len(s)%2 == 1 {
		return s[mid]
	}
	return (s[mid-1] + s[mid]) / 2
}

func round2(v float64) float64 { return math.Round(v*100) / 100 }
