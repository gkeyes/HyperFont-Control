package engine

import (
	"os"
	"strings"
	"testing"
)

// FontListParser.readNamedFamily explicitly throws for these attributes.
// A well-formed XML document is not sufficient for boot-time font initialization.
func TestGeneratedNamedFamiliesMeetAndroidParserContract(t *testing.T) {
	s, a, _ := fixtureStore(t)
	g, err := s.Stage(variableProfile(a))
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range g.Targets {
		raw, err := os.ReadFile(s.data("generations", g.ID, target.File))
		if err != nil {
			t.Fatal(err)
		}
		doc, err := parseXML(raw)
		if err != nil {
			t.Fatal(err)
		}
		var check func(*node, bool)
		check = func(n *node, insideNamedList bool) {
			if n.Name.Local == "family" && (n.attr("name") != "" || insideNamedList) {
				for _, attribute := range n.Attr {
					if attribute.Name.Local == "lang" || attribute.Name.Local == "variant" || attribute.Name.Local == "ignore" || (insideNamedList && attribute.Name.Local == "name") {
						t.Errorf("%s: Android parser rejects %s on named family %q", target.Path, attribute.Name.Local, n.attr("name"))
					}
				}
			}
			for _, child := range n.Children {
				check(child, n.Name.Local == "family-list")
			}
		}
		check(doc, false)
	}
}

func TestAndroidNamedFamilyAttributeValidation(t *testing.T) {
	for _, raw := range []string{
		`<familyset><family name="hfc" lang="zh-Hans"/></familyset>`,
		`<familyset><family name="hfc" lang=""/></familyset>`,
		`<familyset><family name="hfc" variant="compact"/></familyset>`,
		`<familyset><family name="hfc" ignore="false"/></familyset>`,
		`<familyset><family-list name="hfc"><family lang="zh-Hans"/></family-list></familyset>`,
		`<familyset><family-list name="hfc"><family name="nested"/></family-list></familyset>`,
	} {
		doc, err := parseXML([]byte(raw))
		if err != nil {
			t.Fatal(err)
		}
		if validateAndroidFontXML(doc) == nil {
			t.Fatalf("unsafe Android font XML accepted: %s", raw)
		}
	}
	for _, raw := range []string{
		`<familyset><family lang="zh-Hans" variant="compact"/></familyset>`,
		`<familyset><family name="hfc"/><family-list name="pair"><family/></family-list></familyset>`,
		`<fonts-modification><family customizationType="new-locale-family" lang="zh-Hans"/></fonts-modification>`,
	} {
		doc, err := parseXML([]byte(raw))
		if err != nil || validateAndroidFontXML(doc) != nil {
			t.Fatalf("valid font family rejected: %s", raw)
		}
	}
}

func TestBootVerificationRejects017NamedLanguageEvenWithValidHash(t *testing.T) {
	s, a, _ := fixtureStore(t)
	g, err := s.Stage(variableProfile(a))
	if err != nil {
		t.Fatal(err)
	}
	target := &g.Targets[0]
	path := s.data("generations", g.ID, target.File)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	broken := strings.Replace(string(raw), `name="00-hyperfont-chinese"`, `name="00-hyperfont-chinese" lang="zh-Hans,zh-Hant"`, 1)
	if broken == string(raw) {
		t.Fatal("missing regression entry")
	}
	if err = os.WriteFile(path, []byte(broken), 0600); err != nil {
		t.Fatal(err)
	}
	target.SHA = digest([]byte(broken))
	if err = s.verifyGeneration(g); err == nil || !strings.Contains(err.Error(), "拒绝挂载") {
		t.Fatalf("0.1.7 XML must fail before mount, got %v", err)
	}
}
