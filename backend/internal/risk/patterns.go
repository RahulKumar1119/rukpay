package risk

// FraudPattern is one known scam sequence, held as data (Requirement 11
// criterion 3). A payment matches it when the payment has a recipient, its
// amount is at least LargeMin rupees, and the History_Snapshot holds at least
// MinSmallCount events to that same recipient, each inside the window and each
// of at most SmallMax rupees.
type FraudPattern struct {
	// ID is unique within the table, in lower-case snake case. The Signal_Id of
	// its Contribution is SignalPatternPrefix followed by ID.
	ID string
	// WindowSeconds is how far back an event can be and still count.
	WindowSeconds int64
	// MinSmallCount is the fewest small payments that match the pattern.
	MinSmallCount int
	// SmallMax is the largest amount in rupees that counts as small. An amount
	// equal to it is small.
	SmallMax int64
	// LargeMin is the smallest amount in rupees that counts as large. An amount
	// equal to it is large.
	LargeMin int64
	// Points is what the pattern adds once, however many events match.
	Points int
	// Reason is the one calm sentence of its Contribution. It is checked against
	// the wording rules of Requirement 3 together with the sentences of
	// sentences.go.
	Reason string
}

// patternTable is the Pattern_Table. It is applied in order, and one scoring
// function reads every entry, so a row added here is scored with no change to
// the scoring code. The table is read only: nothing writes to it after
// start-up. A test replaces it and puts it back.
var patternTable = []FraudPattern{
	{
		ID:            "test_then_large",
		WindowSeconds: 3_600,
		MinSmallCount: 3,
		SmallMax:      1_000,
		LargeMin:      10_000,
		Points:        15,
		Reason:        "Several small payments to this recipient were followed by a large one.",
	},
}

// Patterns returns a copy of the Pattern_Table, so that a caller cannot change
// the table the engine scores with.
func Patterns() []FraudPattern {
	out := make([]FraudPattern, len(patternTable))
	copy(out, patternTable)
	return out
}

// appendPatterns adds one Contribution for every pattern the payment matches,
// in table order, under the Signal_Id SignalPatternPrefix + ID. A pattern is
// added at most once, with its own points and not a multiple of them.
func appendPatterns(list []Contribution, amount int64, ref string, now int64, history []HistoryEvent) []Contribution {
	// With no recipient no event can be a payment to "the same recipient", so no
	// pattern matches (Requirement 11 criterion 8).
	if ref == "" {
		return list
	}
	for _, p := range patternTable {
		if matchesPattern(p, amount, ref, now, history) {
			list = append(list, Contribution{Signal: SignalPatternPrefix + p.ID, Points: p.Points, Reason: p.Reason})
		}
	}
	return list
}

// matchesPattern reports whether the payment matches p. It counts only events
// that meet all three conditions at once: the same recipient (exactly equal
// text), inside the window, and an amount of at most SmallMax. Other events
// neither count nor cancel the match.
func matchesPattern(p FraudPattern, amount int64, ref string, now int64, history []HistoryEvent) bool {
	if amount < p.LargeMin {
		return false
	}
	small := 0
	for _, e := range history {
		if e.BeneficiaryRef == ref && e.AmountINR <= p.SmallMax && within(e.At, now, p.WindowSeconds) {
			small++
		}
	}
	return small >= p.MinSmallCount
}
