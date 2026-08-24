// Package detect turns a bank or card statement into subscription candidates.
// Hand-entering every subscription is the main reason tracking dies, so Leak
// reads the statement the user already has and proposes what looks recurring —
// the user still reviews and approves before anything is written.
package detect

import (
	"encoding/csv"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"
)

// Txn is one statement line. Amount is positive for money leaving the account;
// credits and refunds are dropped during parsing.
type Txn struct {
	Date        time.Time
	Description string
	Amount      float64
	Currency    string
}

// Column-name synonyms across the banks and card issuers we have seen. Matching
// is substring-based and case-insensitive, so "Transaction Date" hits "date".
// Non-English headers are included because bank exports are rarely localized to
// the reader — adding a synonym here is the cheapest possible contribution.
var (
	dateCols = []string{
		"transaction date", "txn date", "value date", "posted date", "post date",
		"booking date", "date",
		"buchungstag", "datum", "fecha", "data", // de, de/nl, es, it/pt
	}
	descCols = []string{
		"transaction details", "transaction description", "narration", "particulars",
		"description", "details", "merchant", "payee", "memo", "name", "remarks", "reference",
		"verwendungszweck", "beschreibung", "concepto", "libelle", "descrizione",
	}
	amountCols = []string{
		"amount", "value", "transaction amount", "amt",
		"betrag", "umsatz", "importe", "montant", "importo",
	}
	debitCols = []string{
		"debit", "withdrawal", "withdrawal amt", "money out", "paid out", "dr",
		"soll", "belastung",
	}
	creditCols = []string{
		"credit", "deposit", "deposit amt", "money in", "paid in", "cr",
		"haben", "gutschrift",
	}
	currencyCols = []string{"currency", "curr", "ccy", "waehrung", "währung", "divisa"}
)

// dateLayouts are tried in order. Day-first forms come before month-first ones
// because the ambiguity is resolved separately by scanning the whole file.
var dateLayouts = []string{
	"2006-01-02", "2006/01/02", "02-01-2006", "02/01/2006", "02.01.2006",
	"02-Jan-2006", "02 Jan 2006", "Jan 02, 2006", "Jan 2, 2006", "2 January 2006",
	"01/02/2006", "2006-01-02 15:04:05", "02/01/2006 15:04", "2006-01-02T15:04:05Z07:00",
}

// ParseCSV reads a statement export. It finds the header row (banks often pad
// the top of the file with account metadata), maps the columns by synonym, and
// returns debits only.
func ParseCSV(b []byte, defaultCurrency string) ([]Txn, error) {
	text := strings.TrimPrefix(string(b), "\ufeff") // Excel writes a UTF-8 BOM
	r := csv.NewReader(strings.NewReader(text))
	r.Comma = delimiter(text)
	r.FieldsPerRecord = -1 // statements have ragged trailing columns
	r.LazyQuotes = true
	rows, err := r.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("reading statement: %w", err)
	}
	headerAt, cols := findHeader(rows)
	if headerAt < 0 {
		return nil, fmt.Errorf("could not find a header row with a date and an amount column — " +
			"expected something like `Date,Description,Amount`")
	}

	body := rows[headerAt+1:]
	monthFirst := looksMonthFirst(body, cols.date)
	negativeIsSpend := cols.debit < 0 && cols.amount >= 0 && spendSign(body, cols.amount)

	var out []Txn
	for _, row := range body {
		raw := strings.TrimSpace(cell(row, cols.date))
		if raw == "" {
			continue
		}
		when, ok := parseDate(raw, monthFirst)
		if !ok {
			continue // a footer/summary line, not a transaction
		}
		amount, ok := rowAmount(row, cols, negativeIsSpend)
		if !ok || amount <= 0 {
			continue // credit, refund, or unparseable
		}
		desc := strings.TrimSpace(cell(row, cols.desc))
		if desc == "" {
			continue
		}
		cur := strings.ToUpper(strings.TrimSpace(cell(row, cols.currency)))
		if cur == "" {
			cur = strings.ToUpper(defaultCurrency)
		}
		out = append(out, Txn{Date: when, Description: desc, Amount: amount, Currency: cur})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no debit transactions found in the statement")
	}
	return out, nil
}

// delimiter guesses the field separator. European exports (and anywhere the
// comma is a decimal separator) ship semicolon- or tab-separated files.
func delimiter(text string) rune {
	head := text
	if i := nthLineEnd(text, 20); i > 0 {
		head = text[:i]
	}
	best, bestCount := ',', strings.Count(head, ",")
	for _, c := range []rune{';', '\t', '|'} {
		if n := strings.Count(head, string(c)); n > bestCount {
			best, bestCount = c, n
		}
	}
	return best
}

// nthLineEnd returns the offset just past the nth newline, or -1.
func nthLineEnd(s string, n int) int {
	off := 0
	for i := 0; i < n; i++ {
		j := strings.IndexByte(s[off:], '\n')
		if j < 0 {
			return -1
		}
		off += j + 1
	}
	return off
}

// columns holds resolved column indexes; -1 means absent.
type columns struct{ date, desc, amount, debit, credit, currency int }

// findHeader scans the first rows for the header line. Statements frequently
// start with account-summary junk, so this cannot assume row 0.
func findHeader(rows [][]string) (int, columns) {
	limit := min(len(rows), 30)
	for i := 0; i < limit; i++ {
		c := columns{
			date:     matchCol(rows[i], dateCols),
			desc:     matchCol(rows[i], descCols),
			amount:   matchCol(rows[i], amountCols),
			debit:    matchCol(rows[i], debitCols),
			credit:   matchCol(rows[i], creditCols),
			currency: matchCol(rows[i], currencyCols),
		}
		if c.date >= 0 && c.desc >= 0 && (c.amount >= 0 || c.debit >= 0) {
			return i, c
		}
	}
	return -1, columns{}
}

// matchCol returns the index of the column whose name best matches one of the
// candidate synonyms, preferring the longest (most specific) match. Synonyms
// shorter than three characters ("dr", "cr") must match the whole header cell —
// otherwise "Description" would register as a credit column.
func matchCol(header []string, candidates []string) int {
	best, bestLen := -1, 0
	for i, h := range header {
		name := strings.ToLower(strings.TrimSpace(h))
		if name == "" {
			continue
		}
		for _, c := range candidates {
			hit := name == c || (len(c) >= 3 && strings.Contains(name, c))
			if hit && len(c) > bestLen {
				best, bestLen = i, len(c)
			}
		}
	}
	return best
}

func cell(row []string, i int) string {
	if i < 0 || i >= len(row) {
		return ""
	}
	return row[i]
}

// rowAmount resolves the debit amount for one row. negativeIsSpend describes
// the file's sign convention for a single amount column (see spendSign).
func rowAmount(row []string, c columns, negativeIsSpend bool) (float64, bool) {
	if c.debit >= 0 {
		v, ok := parseAmount(cell(row, c.debit))
		if !ok || v == 0 {
			return 0, false // empty debit cell → this row is a credit
		}
		return abs(v), true
	}
	if c.amount >= 0 {
		v, marked, ok := parseAmountMarked(cell(row, c.amount))
		if !ok || v == 0 {
			return 0, false
		}
		if marked {
			return abs(v), true // an explicit DR marker outranks any sign convention
		}
		if negativeIsSpend {
			if v > 0 {
				return 0, false // a positive value is income under this convention
			}
			return -v, true
		}
		if v < 0 {
			return 0, false
		}
		return v, true
	}
	return 0, false
}

// spendSign decides how to read a single amount column. Card and bank exports
// disagree: some write purchases as negatives with credits positive, others
// list debits as plain positives. If the column carries both signs, negatives
// are the spend; if every value is positive, they all are. Rows carrying an
// explicit DR/CR marker are ignored here — they need no convention.
func spendSign(rows [][]string, amountCol int) (negativeIsSpend bool) {
	for _, row := range rows {
		if v, marked, ok := parseAmountMarked(cell(row, amountCol)); ok && !marked && v < 0 {
			return true
		}
	}
	return false
}

var (
	// amountNoise is what surrounds a number: currency symbols and spacing.
	amountNoise = regexp.MustCompile(`[\p{Sc}\s']`)
	numberRE    = regexp.MustCompile(`^[-+]?\d+(\.\d+)?$`)
)

// normalizeSeparators resolves the two thousands/decimal conventions: whichever
// of `.` or `,` appears last is the decimal point ("1,234.56" → 1234.56,
// "1.234,56" → 1234.56). A lone separator followed by exactly two digits is a
// decimal point; otherwise it groups thousands.
func normalizeSeparators(s string) string {
	lastComma, lastDot := strings.LastIndex(s, ","), strings.LastIndex(s, ".")
	switch {
	case lastComma >= 0 && lastDot >= 0:
		if lastComma > lastDot { // European: 1.234,56
			return strings.ReplaceAll(strings.ReplaceAll(s, ".", ""), ",", ".")
		}
		return strings.ReplaceAll(s, ",", "") // Anglo: 1,234.56
	case lastComma >= 0:
		if len(s)-lastComma == 3 { // 1234,56 → decimal comma
			return strings.Replace(s, ",", ".", 1)
		}
		return strings.ReplaceAll(s, ",", "")
	default:
		return s
	}
}

// parseAmount reports the numeric value of a statement cell.
func parseAmount(s string) (float64, bool) {
	v, _, ok := parseAmountMarked(s)
	return v, ok
}

// parseAmountMarked handles thousands separators, currency symbols and codes,
// parenthesised negatives, and DR/CR markers. Anything else — a description, a
// reference number glued to text — is rejected rather than coerced. The second
// return reports an explicit DR (debit) marker; an explicit CR yields ok=false
// because a credit is never a subscription charge.
func parseAmountMarked(s string) (value float64, debitMarked bool, ok bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false, false
	}
	neg := false
	if strings.HasPrefix(s, "(") && strings.HasSuffix(s, ")") {
		neg, s = true, strings.Trim(s, "()")
	}
	var kept []string
	for _, tok := range strings.Fields(s) {
		switch up := strings.ToUpper(tok); {
		case up == "CR":
			return 0, false, false
		case up == "DR":
			debitMarked = true
		case isAlpha(tok) && len(tok) <= 3: // currency codes: INR, USD, Rs
		default:
			kept = append(kept, tok)
		}
	}
	s = amountNoise.ReplaceAllString(strings.Join(kept, ""), "")
	if up := strings.ToUpper(s); strings.HasSuffix(up, "CR") {
		return 0, false, false
	} else if strings.HasSuffix(up, "DR") {
		s, debitMarked = s[:len(s)-2], true
	}
	s = normalizeSeparators(s)
	if !numberRE.MatchString(s) {
		return 0, false, false
	}
	var v float64
	if _, err := fmt.Sscanf(s, "%f", &v); err != nil {
		return 0, false, false
	}
	if neg {
		v = -v
	}
	return v, debitMarked, true
}

func isAlpha(s string) bool {
	for _, r := range s {
		if !unicode.IsLetter(r) {
			return false
		}
	}
	return s != ""
}

// looksMonthFirst decides between DD/MM and MM/DD by looking for a first field
// above 12 anywhere in the file — the only reliable signal a statement gives.
func looksMonthFirst(rows [][]string, dateCol int) bool {
	for _, row := range rows {
		f := strings.FieldsFunc(strings.TrimSpace(cell(row, dateCol)), func(r rune) bool {
			return r == '/' || r == '-' || r == '.'
		})
		if len(f) < 2 || len(f[0]) > 2 {
			continue
		}
		var a, b int
		if _, err := fmt.Sscanf(f[0], "%d", &a); err != nil {
			continue
		}
		if _, err := fmt.Sscanf(f[1], "%d", &b); err != nil {
			continue
		}
		if a > 12 {
			return false // first field must be a day
		}
		if b > 12 {
			return true // second field must be a day, so the first is a month
		}
	}
	return false // ambiguous: day-first is the more common export format
}

func parseDate(s string, monthFirst bool) (time.Time, bool) {
	layouts := dateLayouts
	if monthFirst {
		layouts = append([]string{"01/02/2006", "01-02-2006"}, dateLayouts...)
	}
	for _, l := range layouts {
		if t, err := time.Parse(l, s); err == nil {
			return t.UTC().Truncate(24 * time.Hour), true
		}
	}
	return time.Time{}, false
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}
