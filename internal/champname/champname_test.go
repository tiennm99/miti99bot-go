package champname

import "testing"

type fakeChamp struct {
	Name string
}

func nameOfFake(c *fakeChamp) string { return c.Name }

func TestNormalize(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"", ""},
		{"Kai'Sa", "kaisa"},
		{"KAI SA", "kaisa"},
		{"kaisa", "kaisa"},
		{"Lee Sin", "leesin"},
		{"Dr. Mundo", "drmundo"},
		{"K9", "k9"},
		{"   ", ""},
		{"!!!", ""},
		{"Aurelion Sol", "aurelionsol"},
		{"Cho'Gath", "chogath"},
	}
	for _, tt := range tests {
		got := Normalize(tt.in)
		if got != tt.want {
			t.Errorf("Normalize(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestFind(t *testing.T) {
	pool := []fakeChamp{
		{Name: "Kai'Sa"},
		{Name: "Karma"},
		{Name: "Karthus"},
		{Name: "Lee Sin"},
		{Name: "Lulu"},
		{Name: "Lux"},
	}
	tests := []struct {
		name, in string
		wantName string // "" → nil expected
	}{
		{"empty input → nil", "", ""},
		{"non-alpha input → nil", "!!!", ""},
		{"exact match", "kaisa", "Kai'Sa"},
		{"exact match with punctuation", "Kai'Sa", "Kai'Sa"},
		{"exact match with spaces", "Lee Sin", "Lee Sin"},
		{"unique prefix", "kart", "Karthus"},
		{"unique prefix with case", "KART", "Karthus"},
		{"ambiguous prefix → nil", "ka", ""},
		{"ambiguous prefix lu → nil", "lu", ""},
		{"unambiguous prefix lux", "lux", "Lux"},
		{"no match", "zilean", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Find(pool, tt.in, nameOfFake)
			if tt.wantName == "" {
				if got != nil {
					t.Errorf("Find(%q): got %v, want nil", tt.in, got)
				}
				return
			}
			if got == nil {
				t.Fatalf("Find(%q): got nil, want %q", tt.in, tt.wantName)
			}
			if got.Name != tt.wantName {
				t.Errorf("Find(%q): got %q, want %q", tt.in, got.Name, tt.wantName)
			}
		})
	}
}

func TestFindByExactName(t *testing.T) {
	pool := []fakeChamp{{Name: "Kai'Sa"}, {Name: "Karma"}}

	if got := FindByExactName(pool, "Kai'Sa", nameOfFake); got == nil || got.Name != "Kai'Sa" {
		t.Errorf("FindByExactName(Kai'Sa) = %v, want hit", got)
	}
	// Normalisation must NOT apply — exact match only.
	if got := FindByExactName(pool, "kaisa", nameOfFake); got != nil {
		t.Errorf("FindByExactName(kaisa) = %v, want nil (case-sensitive)", got)
	}
	if got := FindByExactName(pool, "Zilean", nameOfFake); got != nil {
		t.Errorf("FindByExactName(Zilean) = %v, want nil", got)
	}
}
