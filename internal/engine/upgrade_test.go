package engine

import (
	"bytes"
	"os"
	"reflect"
	"testing"
)

func TestUpgradeRebuildPreservesCommittedChoiceAndDraft(t *testing.T) {
	for _, choice := range []string{"active", "pending", "restore"} {
		t.Run(choice, func(t *testing.T) {
			s, a, _ := fixtureStore(t)
			p := variableProfile(a)
			p.Name = "Active"
			active, err := s.Stage(p)
			if err != nil {
				t.Fatal(err)
			}
			if err = writeAtomic(s.data("active"), []byte(active.ID), 0600); err != nil {
				t.Fatal(err)
			}
			if err = os.Remove(s.data("pending")); err != nil {
				t.Fatal(err)
			}
			want := active
			if choice != "active" {
				p.Name = "Pending"
				if choice == "restore" {
					p = DefaultProfile()
				}
				want, err = s.Stage(p)
				if err != nil {
					t.Fatal(err)
				}
			}
			draft := DefaultProfile()
			draft.Name = "Unsubmitted draft"
			if err = writeJSON(s.data("draft.json"), draft); err != nil {
				t.Fatal(err)
			}
			before, _ := os.ReadFile(s.data("draft.json"))
			if _, err = s.RebuildCommitted(); err != nil {
				t.Fatal(err)
			}
			next, err := s.generation("pending")
			if err != nil || next == nil || next.ID == want.ID || !reflect.DeepEqual(next.Profile, want.Profile) {
				t.Fatalf("wrong profile rebuilt: %+v %v", next, err)
			}
			after, _ := os.ReadFile(s.data("draft.json"))
			activePointer, _ := os.ReadFile(s.data("active"))
			if !bytes.Equal(before, after) || string(activePointer) != active.ID {
				t.Fatal("upgrade changed draft or current active pointer")
			}
			if !reflect.DeepEqual(next.Fonts, want.Fonts) {
				t.Fatal("rebuild changed font bytes")
			}
		})
	}
}

func TestUpgradeRebuildDoesNotActivateUnsubmittedDraft(t *testing.T) {
	s, a, _ := fixtureStore(t)
	if _, err := s.Init(); err != nil {
		t.Fatal(err)
	}
	if err := writeJSON(s.data("draft.json"), variableProfile(a)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RebuildCommitted(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(s.data("pending")); !os.IsNotExist(err) {
		t.Fatal("upgrade activated unsubmitted draft")
	}
}

func TestUpgradeRebuildFailureRetainsPending(t *testing.T) {
	for _, failure := range []string{"corrupt-pointer", "corrupt-asset"} {
		t.Run(failure, func(t *testing.T) {
			s, a, _ := fixtureStore(t)
			g, err := s.Stage(variableProfile(a))
			if err != nil {
				t.Fatal(err)
			}
			if failure == "corrupt-pointer" {
				err = writeAtomic(s.data("pending"), []byte("invalid"), 0600)
			} else {
				err = os.WriteFile(s.data("library", a.ID+".font"), []byte("corrupt"), 0600)
			}
			if err != nil {
				t.Fatal(err)
			}
			before, _ := os.ReadFile(s.data("pending"))
			draft, _ := os.ReadFile(s.data("draft.json"))
			if _, err = s.RebuildCommitted(); err == nil {
				t.Fatal("invalid input was accepted")
			}
			after, _ := os.ReadFile(s.data("pending"))
			draftAfter, _ := os.ReadFile(s.data("draft.json"))
			if !bytes.Equal(before, after) || !bytes.Equal(draft, draftAfter) {
				t.Fatal("failed rebuild changed user state")
			}
			if _, err = os.Stat(s.data("generations", g.ID, "manifest.json")); err != nil {
				t.Fatal("failed rebuild removed previous generation")
			}
		})
	}
}
