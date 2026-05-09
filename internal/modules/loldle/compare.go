package loldle

import (
	"strings"
)

// AttrType classifies how a single attribute is compared. Matches the JS
// source's "exact" | "multi" | "year" discriminator.
type AttrType string

const (
	attrExact AttrType = "exact"
	attrMulti AttrType = "multi"
	attrYear  AttrType = "year"
)

// Result categories. Match JS strings byte-for-byte (handlers/render rely on
// these literals via the marker maps).
const (
	ResultCorrect = "correct"
	ResultPartial = "partial"
	ResultWrong   = "wrong"
)

// AttributeRow describes one attribute's comparison output. JS shape:
// {key, label, type, guessValue, targetValue, result, direction?}.
type AttributeRow struct {
	Key         string
	Label       string
	Type        AttrType
	GuessValue  string
	TargetValue string
	Result      string // correct | partial | wrong
	Direction   string // "up" | "down" — only set for year-type wrong rows
}

// classicAttributes is the ordered comparison spec. Order matters for both
// render output and the JS-port test that asserts `compareChampions(...)`
// returns rows in this exact sequence.
var classicAttributes = []AttributeRow{
	{Key: "gender", Label: "Gender", Type: attrExact},
	{Key: "species", Label: "Species", Type: attrMulti},
	{Key: "range_type", Label: "Range type", Type: attrMulti},
	{Key: "resource", Label: "Resource", Type: attrExact},
	{Key: "regions", Label: "Region(s)", Type: attrMulti},
	{Key: "positions", Label: "Position(s)", Type: attrMulti},
	{Key: "release_date", Label: "Release year", Type: attrYear},
}

// CompareChampions returns one row per classic attribute, in declared order.
// Values are compared per attr.Type:
//   - exact: case-insensitive string equality.
//   - multi: set comparison; full match → correct, partial overlap → partial,
//     no overlap → wrong. Two empty sets are correct (matches JS).
//   - year:  parses leading 4 digits; equal → correct, else wrong + a
//     direction hint ("up" if guess<target, "down" if guess>target).
func CompareChampions(guess, target *Champion) []AttributeRow {
	out := make([]AttributeRow, len(classicAttributes))
	for i, attr := range classicAttributes {
		row := attr // copy, then fill
		gVal := attrValue(guess, attr.Key)
		tVal := attrValue(target, attr.Key)

		switch attr.Type {
		case attrYear:
			gy := parseYear(asString(gVal))
			ty := parseYear(asString(tVal))
			row.GuessValue = yearOrPlaceholder(gy)
			row.TargetValue = yearOrPlaceholder(ty)
			row.Result, row.Direction = compareYear(gy, ty)
		case attrExact:
			row.GuessValue = formatValue(gVal)
			row.TargetValue = formatValue(tVal)
			if strings.EqualFold(asString(gVal), asString(tVal)) {
				row.Result = ResultCorrect
			} else {
				row.Result = ResultWrong
			}
		case attrMulti:
			row.GuessValue = formatValue(gVal)
			row.TargetValue = formatValue(tVal)
			row.Result = compareMultiValue(asStringSlice(gVal), asStringSlice(tVal))
		}
		out[i] = row
	}
	return out
}

// attrValue returns the named field on a Champion as `any` (string or []string).
// Centralised so adding a new attribute touches one switch.
func attrValue(c *Champion, key string) any {
	if c == nil {
		return nil
	}
	switch key {
	case "gender":
		return c.Gender
	case "species":
		return c.Species
	case "range_type":
		return c.RangeType
	case "resource":
		return c.Resource
	case "regions":
		return c.Regions
	case "positions":
		return c.Positions
	case "release_date":
		return c.ReleaseDate
	}
	return nil
}

func asString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case []string:
		return strings.Join(x, ",")
	}
	return ""
}

func asStringSlice(v any) []string {
	switch x := v.(type) {
	case []string:
		return x
	case string:
		// JS toSet falls back to splitting on "," when the value isn't an array.
		// Champions.json never produces this branch but parity matters.
		if x == "" {
			return nil
		}
		return strings.Split(x, ",")
	}
	return nil
}

// compareMultiValue: full match (case-insensitive, order-independent) →
// correct; any intersection → partial; otherwise wrong. Two empty sets
// are correct (matches JS, e.g. for hypothetical "no positions" champs).
func compareMultiValue(guess, target []string) string {
	g := toLowerSet(guess)
	t := toLowerSet(target)
	if len(g) == 0 && len(t) == 0 {
		return ResultCorrect
	}
	if len(g) == 0 || len(t) == 0 {
		return ResultWrong
	}
	if setsEqual(g, t) {
		return ResultCorrect
	}
	for v := range g {
		if _, ok := t[v]; ok {
			return ResultPartial
		}
	}
	return ResultWrong
}

func toLowerSet(xs []string) map[string]struct{} {
	out := make(map[string]struct{}, len(xs))
	for _, s := range xs {
		t := strings.TrimSpace(strings.ToLower(s))
		if t != "" {
			out[t] = struct{}{}
		}
	}
	return out
}

func setsEqual(a, b map[string]struct{}) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if _, ok := b[k]; !ok {
			return false
		}
	}
	return true
}

// parseYear extracts the first 4 digits of s. Returns 0 if absent/non-numeric
// — JS regex parity (`^(\d{4})`).
func parseYear(s string) int {
	if len(s) < 4 {
		return 0
	}
	for i := 0; i < 4; i++ {
		if s[i] < '0' || s[i] > '9' {
			return 0
		}
	}
	y := 0
	for i := 0; i < 4; i++ {
		y = y*10 + int(s[i]-'0')
	}
	return y
}

func yearOrPlaceholder(y int) string {
	if y == 0 {
		return "?"
	}
	// strconv would pull in another import; for a 4-digit positive int the
	// Sprintf path is fine and zero-allocs after warmup.
	return fmtYear(y)
}

func fmtYear(y int) string {
	// Manual base-10 to avoid strconv import; year is always 4 digits here.
	return string([]byte{
		byte('0' + (y/1000)%10),
		byte('0' + (y/100)%10),
		byte('0' + (y/10)%10),
		byte('0' + y%10),
	})
}

func compareYear(g, t int) (result, direction string) {
	if g == 0 || t == 0 {
		return ResultWrong, ""
	}
	if g == t {
		return ResultCorrect, ""
	}
	if g < t {
		return ResultWrong, "up"
	}
	return ResultWrong, "down"
}

// formatValue mirrors JS's `formatValue`: empty → "—", array → comma-joined,
// otherwise toString.
func formatValue(v any) string {
	switch x := v.(type) {
	case nil:
		return "—"
	case string:
		if x == "" {
			return "—"
		}
		return x
	case []string:
		if len(x) == 0 {
			return "—"
		}
		return strings.Join(x, ", ")
	}
	return "—"
}
