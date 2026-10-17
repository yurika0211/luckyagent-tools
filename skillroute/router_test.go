package skillroute

import "testing"

func TestMatchingAndAmbiguity(t *testing.T) {
	research := Descriptor{Name: "deep-research", Aliases: []string{"investigate"}, Description: "Research workflow", Summary: "Use when collecting sources, evidence, and reports."}
	cases := []struct {
		name, input string
		skills      []Descriptor
		want        string
		explicit    bool
	}{
		{"explicit", "use deep-research", []Descriptor{research}, "deep-research", true},
		{"alias", "investigate this", []Descriptor{research}, "deep-research", true},
		{"cross-language", "请帮我做一个有来源支撑的深度调研报告", []Descriptor{research}, "deep-research", false},
		{"weak", "你好", []Descriptor{research}, "", false},
		{"empty-descriptor", "use deep-research", []Descriptor{{}, research}, "deep-research", true},
		{"ambiguous", "collect evidence and sources", []Descriptor{{Name: "alpha", Description: "evidence sources"}, {Name: "beta", Description: "evidence sources"}}, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Match(tc.input, tc.skills)
			if tc.want == "" {
				if got != nil {
					t.Fatalf("unexpected match: %+v", got)
				}
				return
			}
			if got == nil || got.Skill.Name != tc.want || got.Explicit != tc.explicit {
				t.Fatalf("match: %+v", got)
			}
			if tc.skills[got.Index].Name != tc.want {
				t.Fatal("original descriptor index was lost")
			}
		})
	}
}
