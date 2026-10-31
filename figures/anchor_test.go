package figures

import "testing"

func TestAMarkAndItsCaptionAreOneAnchor(t *testing.T) {
	body := "Text before.\n\n⟦figure⟧\n\nРис. 2. Струя\n\nText after.\n\nРис. 3. Без метки\n"
	got := Anchors(body)
	if len(got) != 2 {
		t.Fatalf("got %d anchors, want 2: %+v", len(got), got)
	}
	if !got[0].Marker || got[0].Number != "2" || got[0].Key != "рис2струя" {
		t.Errorf("first anchor = %+v", got[0])
	}
	if got[1].Marker || got[1].Number != "3" || got[1].Line != 8 {
		t.Errorf("second anchor = %+v", got[1])
	}
}

func TestAMarkWithoutACaptionIsKeyedByTheTextAfterIt(t *testing.T) {
	got := Anchors("⟦figure⟧\nДалее идёт текст.\n")
	if len(got) != 1 || got[0].Number != "" || got[0].Key != "далееидёттекст" {
		t.Fatalf("got %+v", got)
	}
}
