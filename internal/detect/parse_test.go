package detect

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseCSVDebitOnlyAmountColumn(t *testing.T) {
	// Every value positive: the export lists debits only, so all of them count.
	csv := "Date,Description,Amount\n" +
		"2026-07-12,UPI/NETFLIX COM/1111,649.00\n" +
		"2026-07-14,POS SPOTIFY,119.00\n"
	txns, err := ParseCSV([]byte(csv), "INR")
	require.NoError(t, err)
	require.Len(t, txns, 2)
	assert.Equal(t, day(2026, 7, 12), txns[0].Date)
	assert.InDelta(t, 649.0, txns[0].Amount, 1e-9)
	assert.Equal(t, "INR", txns[0].Currency)
}

func TestParseCSVSignedAmountColumn(t *testing.T) {
	// Mixed signs: the card export writes purchases negative, credits positive.
	csv := "Date,Description,Amount\n" +
		"2026-07-12,UPI/NETFLIX COM/1111,-649.00\n" +
		"2026-07-13,SALARY CREDIT,50000.00\n"
	txns, err := ParseCSV([]byte(csv), "INR")
	require.NoError(t, err)
	require.Len(t, txns, 1, "the positive line is income under this convention")
	assert.InDelta(t, 649.0, txns[0].Amount, 1e-9)
	assert.Equal(t, "UPI/NETFLIX COM/1111", txns[0].Description)
}

func TestParseCSVDebitCreditColumnsAndPreamble(t *testing.T) {
	// A typical Indian bank export: metadata rows, then the real header, then
	// separate withdrawal/deposit columns with thousands separators.
	csv := "Statement of Account\n" +
		"Account Number,XXXXXX1234\n" +
		"\n" +
		"Txn Date,Narration,Withdrawal Amt,Deposit Amt,Closing Balance\n" +
		"12/07/2026,UPI/NETFLIX COM/1111,\"1,649.00\",,\"25,000.00\"\n" +
		"13/07/2026,SALARY JULY,,\"50,000.00\",\"75,000.00\"\n" +
		"14/07/2026,POS 4321 SPOTIFY,119.00,,\"74,881.00\"\n"
	txns, err := ParseCSV([]byte(csv), "INR")
	require.NoError(t, err)
	require.Len(t, txns, 2, "credits must be dropped")
	assert.InDelta(t, 1649.0, txns[0].Amount, 1e-9)
	assert.Equal(t, day(2026, 7, 12), txns[0].Date, "DD/MM should win for an Indian export")
	assert.InDelta(t, 119.0, txns[1].Amount, 1e-9)
}

func TestParseCSVMonthFirstDates(t *testing.T) {
	// A US-style export: 07/28 is unambiguous only because 28 > 12.
	csv := "Transaction Date,Merchant,Amount,Currency\n" +
		"07/28/2026,NETFLIX.COM,15.49,USD\n" +
		"06/28/2026,NETFLIX.COM,15.49,USD\n"
	txns, err := ParseCSV([]byte(csv), "INR")
	require.NoError(t, err)
	require.Len(t, txns, 2)
	assert.Equal(t, day(2026, 7, 28), txns[0].Date)
	assert.Equal(t, "USD", txns[0].Currency, "an explicit currency column wins over the default")
}

func TestParseCSVHandlesSymbolsParensAndDRCR(t *testing.T) {
	csv := "Date,Details,Amount\n" +
		"01-Jul-2026,GITHUB.COM,₹400.00 DR\n" +
		"02-Jul-2026,REFUND ADOBE,(1675.00)\n" +
		"03-Jul-2026,INTEREST,25.00 CR\n"
	txns, err := ParseCSV([]byte(csv), "INR")
	require.NoError(t, err)
	require.Len(t, txns, 2, "the CR line is income")
	assert.InDelta(t, 400.0, txns[0].Amount, 1e-9)
	assert.InDelta(t, 1675.0, txns[1].Amount, 1e-9, "parenthesised amounts are negative → spend")
}

func TestParseCSVSkipsFooterAndBlankRows(t *testing.T) {
	csv := "Date,Description,Amount\n" +
		"2026-07-12,NETFLIX,649.00\n" +
		",,\n" +
		"Total,,649.00\n"
	txns, err := ParseCSV([]byte(csv), "INR")
	require.NoError(t, err)
	assert.Len(t, txns, 1)
}

func TestParseCSVErrors(t *testing.T) {
	_, err := ParseCSV([]byte("foo,bar\n1,2\n"), "INR")
	assert.ErrorContains(t, err, "could not find a header row")

	_, err = ParseCSV([]byte("Date,Description,Credit\n2026-07-12,SALARY,5000\n"), "INR")
	assert.Error(t, err, "a statement with no debits has nothing to detect")
}

func TestParseCSVSemicolonEuropeanFormat(t *testing.T) {
	// German-style export: semicolon separator, comma as the decimal point,
	// dotted thousands, DD.MM.YYYY dates, and a UTF-8 BOM from Excel.
	csv := "\ufeffBuchungstag;Verwendungszweck;Betrag\n" +
		"12.07.2026;NETFLIX.COM;-1.649,00\n" +
		"13.07.2026;GEHALT JULI;2.500,00\n"
	txns, err := ParseCSV([]byte(csv), "EUR")
	require.NoError(t, err)
	require.Len(t, txns, 1)
	assert.InDelta(t, 1649.0, txns[0].Amount, 1e-9)
	assert.Equal(t, day(2026, 7, 12), txns[0].Date)
	assert.Equal(t, "EUR", txns[0].Currency)
}

func TestParseAmountSeparatorConventions(t *testing.T) {
	cases := map[string]float64{
		"1,234.56":     1234.56,
		"1.234,56":     1234.56,
		"1234,56":      1234.56,
		"1,234":        1234,
		"1234.56":      1234.56,
		"₹1,649.00 DR": 1649.00,
	}
	for in, want := range cases {
		got, ok := parseAmount(in)
		require.True(t, ok, in)
		assert.InDelta(t, want, got, 1e-9, in)
	}
}
