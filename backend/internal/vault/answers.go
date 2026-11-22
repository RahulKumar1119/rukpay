package vault

import "errors"

// Answer is a Yes or No answer to one reality-check question.
type Answer string

// The only two accepted answer values. Any other value is rejected.
const (
	AnswerYes Answer = "YES"
	AnswerNo  Answer = "NO"
)

// Answers holds the replies to the three reality-check questions
// (Requirement 3.1). Only these three values are kept. No free text is stored
// (Requirement 3.9).
type Answers struct {
	// Coercion: are you on a call with someone claiming to be Police, CBI,
	// Customs, RBI or your bank? YES is a sign of a scam.
	Coercion Answer `json:"coercion"`
	// Secrecy: did the caller ask you not to tell your family or bank staff?
	// YES is a sign of a scam.
	Secrecy Answer `json:"secrecy"`
	// Verification: have you checked this person or bill without using a phone
	// number they just sent you? NO is a sign of a scam.
	Verification Answer `json:"verification"`
}

// ErrInvalidAnswers is returned when an answer is missing or is not YES or NO
// (Requirement 3.7).
var ErrInvalidAnswers = errors.New("invalid answers")

// CheckAnswers reports whether the answers lock the payment: true when
// Coercion is YES, Secrecy is YES or Verification is NO (Requirements 3.2 to
// 3.4). It returns ErrInvalidAnswers when any answer is not YES or NO.
func CheckAnswers(a Answers) (lock bool, err error) {
	for _, answer := range []Answer{a.Coercion, a.Secrecy, a.Verification} {
		if answer != AnswerYes && answer != AnswerNo {
			return false, ErrInvalidAnswers
		}
	}
	lock = a.Coercion == AnswerYes || a.Secrecy == AnswerYes || a.Verification == AnswerNo
	return lock, nil
}
