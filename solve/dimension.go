package solve

import (
	"regexp"
	"strings"
)

// Dimensions checks a physics solution for the one mistake a prose judge is
// worst at catching.
//
// This is deliberately one narrow rule rather than a units algebra. A real
// dimensional analysis would need to parse the LaTeX, and the interesting cases
// in Квант are geometrical arguments and limiting cases where a symbolic
// checker would produce more noise than signal. What is checked here is the
// thing that is cheap and certain: a solution that worked in metres and seconds
// throughout and then states its result as a bare number has dropped the units
// between the last line of algebra and the answer, and that is a real defect in
// a physics answer however sound the derivation above it.
//
// A solution that never mentions a unit is left alone. Plenty of the physics in
// this magazine asks for a ratio, an angle or a proof, and those answers are
// dimensionless and correct. Flagging them would send the correction loop after
// solutions that have nothing wrong with them, which costs tokens and, worse,
// invites a model to bolt units onto a dimensionless answer to satisfy the
// checker.
func Dimensions(solution string) []string {
	if !usesUnits(solution) {
		return nil
	}
	line, ok := answerLine(solution)
	if !ok {
		return []string{"the solution works in units but never states its answer on a line of its own"}
	}
	if usesUnits(line) || dimensionless(line) {
		return nil
	}
	if !hasNumber(line) {
		return nil
	}
	return []string{"the answer is given as a bare number and the working carries units, " +
		"so the units were dropped somewhere between the last line of algebra and the answer: " +
		strings.TrimSpace(line)}
}

// units are the ones the magazine actually prints, in Russian and in the Latin
// forms the reading lane sometimes emits for the same symbols.
const units = `м|см|мм|км|дм|мкм|нм|с|мс|мкс|мин|ч|сут|кг|г|мг|т|Н|кН|Дж|кДж|МДж|Вт|кВт|МВт|` +
	`Па|кПа|МПа|атм|В|кВ|мВ|А|мА|Ом|кОм|Кл|Ф|мкФ|Тл|мТл|Вб|Гн|Гц|кГц|МГц|К|моль|эВ|МэВ|л|мл|` +
	`рад|град|об/мин|m|cm|mm|km|s|ms|kg|g|N|J|W|Pa|V|A|Hz|K|mol`

// unitAfterNumber matches a number with a unit on it.
//
// The unit has to follow a number, because in Russian several of these are
// ordinary words. «с» is the preposition with, «г» abbreviates год, «т» opens
// то and «А» is a letter used to label a point in a diagram. Requiring a digit
// in front is what separates 5 с from a sentence about something happening с
// постоянной скоростью.
//
// What sits between the number and the unit is nearly always LaTeX rather than
// a space, because the magazine sets the quantity as $s = 100$ м and the digit
// and the unit end up on opposite sides of a closing delimiter. A pattern that
// allows only whitespace there matches almost nothing in this corpus while
// looking correct in a unit test written in plain text.
//
// The boundaries are written as \p{L} classes rather than \b because \b in this
// regexp engine is ASCII only, and every letter here is Cyrillic: between м and
// е in метр both sides are non-ASCII, so \b finds no boundary and the rule
// matches метров as though it were metres.
var unitAfterNumber = regexp.MustCompile(
	`(?:^|[^\p{L}\d])\d+(?:[.,·]\d+)?[\s$~}\\,;]*(?:` + units + `)(?:[^\p{L}]|$)`)

func usesUnits(text string) bool { return unitAfterNumber.MatchString(text) }

var number = regexp.MustCompile(`\d`)

func hasNumber(text string) bool { return number.MatchString(text) }

// dimensionless is what an answer with no units on it legitimately looks like.
// A percentage, a ratio, an angle in degrees, or an answer that is still an
// expression in the problem's own symbols rather than a computed number.
var dimensionless = regexp.MustCompile(`%|°|\\%|\\circ|раз|отношени|во сколько|коэффициент|КПД`).MatchString

// answerHeading is how a solution announces its result. The magazine writes
// Ответ, and a model asked for the value on its own line usually copies that.
var answerHeading = regexp.MustCompile(`(?i)^\s*\**\s*(ответ|answer)`)

// answerLine finds the line the answer is stated on.
//
// A line beginning Ответ wins wherever it is, because that is the solution
// saying so itself. Failing that the last line with a digit in it is taken,
// which is what a solution that ends on its result looks like.
func answerLine(solution string) (string, bool) {
	lines := strings.Split(solution, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if answerHeading.MatchString(lines[i]) {
			return lines[i], true
		}
	}
	for i := len(lines) - 1; i >= 0; i-- {
		if hasNumber(lines[i]) {
			return lines[i], true
		}
	}
	return "", false
}
