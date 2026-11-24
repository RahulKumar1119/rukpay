package risk

// This file holds the sentences that the Risk_Engine returns as the reason of a
// Contribution (Requirement 3.7). It declares string constants and nothing
// else: no variable, no function, no import. Signal ids live in explain.go.
//
// Each sentence is chosen by Signal_Id and tier alone, so the same Signal_Id
// and tier always give the same bytes. Each is one calm sentence of 1 to 20
// words that starts with a capital letter and ends with one full stop. None
// holds a digit, an amount, an @ sign or an Avoid_Word. A test parses this file
// and checks every constant, so a sentence added here is checked with no change
// to the test.
const (
	// sentAmountMid is the amount sentence for 10 points.
	sentAmountMid = "This payment is bigger than a small everyday purchase."
	// sentAmountHigh is the amount sentence for 20 points.
	sentAmountHigh = "This is a large payment."
	// sentAmountTop is the amount sentence for 35 points.
	sentAmountTop = "This is a very large payment."

	// sentAgeNew is the beneficiary age sentence for 25 points.
	sentAgeNew = "The recipient was added less than a day ago."
	// sentAgeRecent is the beneficiary age sentence for 10 points.
	sentAgeRecent = "The recipient was added in the last few days."

	// sentLateNight is the late night sentence.
	sentLateNight = "The payment is being made late at night."
	// sentUnknownCall is the unknown call sentence.
	sentUnknownCall = "A call from someone you do not know is going on."

	// sentRetriesMid is the quick retries sentence for 5 points.
	sentRetriesMid = "The payment has been tried a couple of times in a row."
	// sentRetriesHigh is the quick retries sentence for 10 points.
	sentRetriesHigh = "The payment has been tried several times in a row."

	// sentBehaviourCap is the sentence of the behaviour cap line, whatever its
	// points.
	sentBehaviourCap = "The extra points from recent activity have an upper limit."
	// sentScoreCap is the sentence of the score cap line, whatever its points.
	sentScoreCap = "The total cannot go above its upper limit."
)
