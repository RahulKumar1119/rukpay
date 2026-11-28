package risk

import "github.com/RahulKumar1119/rukpay/backend/internal/store"

// This file holds the Care_Points rule (Requirement 18 criteria 6 to 9). A
// payment to a saved recipient on extra-care protection adds a few points, so
// that a recipient who is new, or who has not been checked yet, gets a slower
// look. Care points only ever add: trust takes nothing away from the baseline.
// Everything here is a pure function of its arguments.

// CareCap is the most points that the care Contribution can add to a score
// (Requirement 9 criterion 2). Together with BehaviourCap it limits the extra
// points on top of the baseline to 35.
const CareCap = 10

// Care points for a recipient on extra-care protection. A recipient with no
// verified time, or with the Pressure_Flag, gets the larger number.
const (
	careNotChecked = 10
	careChecked    = 5
)

// Recipient is what scoring reads from a saved recipient. A nil *Recipient
// means the payment names no saved recipient. It holds no label, no identifier
// and no history, so the engine cannot place any of them in an Explanation.
type Recipient struct {
	// Protection is the Protection_Level of the recipient.
	Protection store.ProtectionLevel
	// Verified is true when the recipient has a verified time.
	Verified bool
	// Pressure is true when the recipient has the Pressure_Flag.
	Pressure bool
}

// careSentences maps the points of carePoints to the sentence of that tier, so
// the reason is chosen by the points alone (Requirement 3 criterion 1). Read
// only: nothing writes to it after start-up.
var careSentences = []pointsSentence{
	{points: careNotChecked, sentence: sentCareNew},
	{points: careChecked, sentence: sentCareList},
}

// carePoints returns the Care_Points of a recipient, before the CareCap. A
// payment with no recipient, and a recipient on any protection other than
// extra care, earns 0. A recipient on extra care earns 10 when it has no
// verified time or has the Pressure_Flag, and 5 otherwise. It reads r and never
// changes it.
func carePoints(r *Recipient) int {
	if r == nil || r.Protection != store.ProtectionExtraCare {
		return 0
	}
	if !r.Verified || r.Pressure {
		return careNotChecked
	}
	return careChecked
}
