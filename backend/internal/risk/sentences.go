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

	// sentVelocityLow is the quick payments sentence for 5 points.
	sentVelocityLow = "Several payments were made in the last few minutes."
	// sentVelocityMid is the quick payments sentence for 10 points.
	sentVelocityMid = "Many payments were made in the last few minutes."
	// sentVelocityHigh is the quick payments sentence for 15 points.
	sentVelocityHigh = "A lot of payments were made in the last few minutes."

	// sentDailyLow is the payments in a day sentence for 5 points.
	sentDailyLow = "There have been quite a few payments in the last day."
	// sentDailyHigh is the payments in a day sentence for 10 points.
	sentDailyHigh = "There have been a lot of payments in the last day."

	// sentRepeatLow is the same recipient sentence for 5 points.
	sentRepeatLow = "A few payments went to this same recipient in the last hour."
	// sentRepeatHigh is the same recipient sentence for 10 points.
	sentRepeatHigh = "Many payments went to this same recipient in the last hour."

	// sentDeviationMid is the sentence for a payment at least 3 times the
	// largest recent payment, which earns 10 points.
	sentDeviationMid = "This payment is much larger than your recent ones."
	// sentDeviationHigh is the sentence for a payment at least 10 times the
	// largest recent payment, which earns 15 points.
	sentDeviationHigh = "This payment is far larger than your recent ones."

	// sentBehaviourCap is the sentence of the behaviour cap line, whatever its
	// points.
	sentBehaviourCap = "The extra points from recent activity have an upper limit."
	// sentScoreCap is the sentence of the score cap line, whatever its points.
	sentScoreCap = "The total cannot go above its upper limit."
)
