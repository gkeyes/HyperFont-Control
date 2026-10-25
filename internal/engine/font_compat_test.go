package engine

import "testing"

func TestCompatOnlyForCustomXiaomiDefaultRoles(t *testing.T) {
	p := DefaultProfile()
	g := Generation{Profile: p, Targets: []Target{{Path: "/product/etc/misans_variable_fonts.xml"}}}
	if needsFontCompat(g) {
		t.Fatal("stock profile enables adapter")
	}
	p.Slots["serif"] = Slot{Mode: "variable"}
	if needsFontCompat(g) {
		t.Fatal("serif-only profile changes default text")
	}
	p.Slots["chinese"] = Slot{Mode: "static"}
	if !needsFontCompat(g) {
		t.Fatal("static Chinese missing adapter")
	}
	g.Targets = nil
	if needsFontCompat(g) {
		t.Fatal("non-Xiaomi ROM enables adapter")
	}
}

func TestSavedWeightsPersistWithoutChangingContinuousOutput(t *testing.T) {
	s, a, _ := fixtureStore(t)
	p := variableProfile(a)
	v := p.Slots["chinese"]
	v.SavedWeightMap = v.WeightMap
	v.WeightMap = nil
	v.FamilyMode = "continuous"
	p.Slots["chinese"] = v
	g, err := s.Stage(p)
	if err != nil {
		t.Fatal(err)
	}
	pending, err := s.generation("pending")
	if err != nil || pending.Profile.Slots["chinese"].SavedWeightMap["400"] != 450 {
		t.Fatal("saved mapping lost in serialization", err)
	}
	v.SavedWeightMap = nil
	p.Slots["chinese"] = v
	plain, err := s.Stage(p)
	if err != nil {
		t.Fatal(err)
	}
	for i, target := range g.Targets {
		if target.SHA != plain.Targets[i].SHA {
			t.Fatal("inactive mapping changed output")
		}
	}
	v.SavedWeightMap = map[string]float64{"400": 450}
	p.Slots["chinese"] = v
	if _, err = s.Validate(p); err == nil {
		t.Fatal("partial remembered map accepted")
	}
}
