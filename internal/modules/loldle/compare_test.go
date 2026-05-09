package loldle

import (
	"testing"
)

// Fixtures lifted from tests/modules/loldle/compare.test.js so any future
// drift between Go and JS scoring shows up as a test failure here.

var aatrox = &Champion{
	ChampionName: "Aatrox",
	Gender:       "Male",
	Species:      []string{"Darkin"},
	RangeType:    []string{"Melee"},
	Resource:     "Manaless",
	Regions:      []string{"Runeterra", "Shurima"},
	Positions:    []string{"Top"},
	ReleaseDate:  "2013-06-13",
}

var ahri = &Champion{
	ChampionName: "Ahri",
	Gender:       "Female",
	Species:      []string{"Vastayan"},
	RangeType:    []string{"Ranged"},
	Resource:     "Mana",
	Regions:      []string{"Ionia"},
	Positions:    []string{"Middle"},
	ReleaseDate:  "2011-12-14",
}

var akali = &Champion{
	ChampionName: "Akali",
	Gender:       "Female",
	Species:      []string{"Human"},
	RangeType:    []string{"Melee"},
	Resource:     "Energy",
	Regions:      []string{"Ionia"},
	Positions:    []string{"Middle", "Top"},
	ReleaseDate:  "2010-05-11",
}

func byKey(rows []AttributeRow, key string) AttributeRow {
	for _, r := range rows {
		if r.Key == key {
			return r
		}
	}
	return AttributeRow{}
}

func TestCompareChampions_AllCorrectWhenIdentical(t *testing.T) {
	for _, r := range CompareChampions(aatrox, aatrox) {
		if r.Result != ResultCorrect {
			t.Errorf("%s: %s, want correct", r.Key, r.Result)
		}
	}
}

func TestCompareChampions_ExactMismatchIsWrong(t *testing.T) {
	r := CompareChampions(aatrox, ahri)
	if got := byKey(r, "gender").Result; got != ResultWrong {
		t.Errorf("gender = %s, want wrong", got)
	}
	if got := byKey(r, "resource").Result; got != ResultWrong {
		t.Errorf("resource = %s, want wrong", got)
	}
}

func TestCompareChampions_MultiPartialOverlap(t *testing.T) {
	// guess akali (Middle, Top) vs target with positions=[Middle] → partial.
	target := *ahri
	target.Positions = []string{"Middle"}
	r := CompareChampions(akali, &target)
	if got := byKey(r, "positions").Result; got != ResultPartial {
		t.Errorf("positions = %s, want partial", got)
	}

	target2 := *ahri
	target2.Regions = []string{"Runeterra", "Ionia"}
	r2 := CompareChampions(aatrox, &target2)
	if got := byKey(r2, "regions").Result; got != ResultPartial {
		t.Errorf("regions = %s, want partial", got)
	}
}

func TestCompareChampions_MultiIdenticalSetsCaseInsensitive(t *testing.T) {
	guess := *akali
	guess.Species = []string{"Human", "Ninja"}
	target := *akali
	target.Species = []string{"ninja", "HUMAN"}
	r := CompareChampions(&guess, &target)
	if got := byKey(r, "species").Result; got != ResultCorrect {
		t.Errorf("species = %s, want correct", got)
	}
}

func TestCompareChampions_YearDirectionUp(t *testing.T) {
	// akali 2010 vs aatrox 2013 → wrong + up
	r := CompareChampions(akali, aatrox)
	y := byKey(r, "release_date")
	if y.Result != ResultWrong {
		t.Errorf("year result = %s, want wrong", y.Result)
	}
	if y.Direction != "up" {
		t.Errorf("year direction = %s, want up", y.Direction)
	}
}

func TestCompareChampions_YearDirectionDown(t *testing.T) {
	// aatrox 2013 vs akali 2010 → wrong + down
	r := CompareChampions(aatrox, akali)
	y := byKey(r, "release_date")
	if y.Result != ResultWrong {
		t.Errorf("year result = %s, want wrong", y.Result)
	}
	if y.Direction != "down" {
		t.Errorf("year direction = %s, want down", y.Direction)
	}
}

func TestCompareChampions_AttributeOrderIsStable(t *testing.T) {
	r := CompareChampions(aatrox, ahri)
	want := []string{"gender", "species", "range_type", "resource", "regions", "positions", "release_date"}
	if len(r) != len(want) {
		t.Fatalf("len = %d, want %d", len(r), len(want))
	}
	for i, w := range want {
		if r[i].Key != w {
			t.Errorf("row[%d].Key = %s, want %s", i, r[i].Key, w)
		}
	}
}

func TestCompareYear_ZeroOnEither(t *testing.T) {
	res, dir := compareYear(0, 2013)
	if res != ResultWrong || dir != "" {
		t.Errorf("0 vs 2013: %s %s", res, dir)
	}
	res, dir = compareYear(2013, 0)
	if res != ResultWrong || dir != "" {
		t.Errorf("2013 vs 0: %s %s", res, dir)
	}
}

func TestParseYear_InvalidInputs(t *testing.T) {
	cases := map[string]int{
		"":           0,
		"abc":        0,
		"19":         0,
		"2013":       2013,
		"2013-06-13": 2013,
		"abcd-06-13": 0,
	}
	for in, want := range cases {
		if got := parseYear(in); got != want {
			t.Errorf("parseYear(%q) = %d, want %d", in, got, want)
		}
	}
}
