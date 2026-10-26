package dict

import (
	"reflect"
	"testing"

	"worddata/internal/model"
)

func TestExtractPOSMeanings_SplitsOnChineseSemicolon(t *testing.T) {
	explain := "n. 虚荣（心），自负；（相对于非常严肃的事而言）无意义，微不足道；梳妆台，盥洗柜；自负的行为，虚荣..."
	got := extractPOSMeanings(explain, "noun")
	want := []string{
		"虚荣（心），自负",
		"（相对于非常严肃的事而言）无意义，微不足道",
		"梳妆台，盥洗柜",
		"自负的行为，虚荣...",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("extractPOSMeanings() = %#v, want %#v", got, want)
	}
}

func TestExtractPOSMeanings_FallsBackToAsciiSemicolon(t *testing.T) {
	explain := "n. foo;bar"
	got := extractPOSMeanings(explain, "noun")
	want := []string{"foo", "bar"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("extractPOSMeanings() = %#v, want %#v", got, want)
	}
}

func TestExtractPOSMeanings_NoMatchForPOS(t *testing.T) {
	// extractPOSMeaning falls back to the whole blob when the requested POS
	// has no matching marker in explain; extractPOSMeanings should still
	// split that fallback the same way.
	explain := "n. 虚荣"
	got := extractPOSMeanings(explain, "verb")
	want := []string{"n. 虚荣"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("extractPOSMeanings() = %#v, want %#v", got, want)
	}
}

func TestAssignPOSMeanings_DistributesDistinctGlossesPerSense(t *testing.T) {
	explain := "n. 虚荣（心），自负；无意义，微不足道；梳妆台，盥洗柜；自负的行为，虚荣..."
	senses := []model.Sense{
		{POS: "noun", MeaningEN: "a"},
		{POS: "noun", MeaningEN: "b"},
		{POS: "noun", MeaningEN: "c"},
		{POS: "noun", MeaningEN: "d"},
	}
	assignPOSMeanings(senses, explain)

	want := []string{
		"虚荣（心），自负",
		"无意义，微不足道",
		"梳妆台，盥洗柜",
		"自负的行为，虚荣...",
	}
	for i, w := range want {
		if senses[i].MeaningCN != w {
			t.Errorf("senses[%d].MeaningCN = %q, want %q", i, senses[i].MeaningCN, w)
		}
	}

	// Each sense must get its own gloss, not a shared duplicate.
	seen := map[string]bool{}
	for _, s := range senses {
		if seen[s.MeaningCN] {
			t.Fatalf("duplicate MeaningCN %q assigned to multiple senses: %#v", s.MeaningCN, senses)
		}
		seen[s.MeaningCN] = true
	}
}

func TestAssignPOSMeanings_LeavesExtraSensesBlankWhenGlossesRunOut(t *testing.T) {
	explain := "n. 虚荣；自负"
	senses := []model.Sense{
		{POS: "noun", MeaningEN: "a"},
		{POS: "noun", MeaningEN: "b"},
		{POS: "noun", MeaningEN: "c"},
	}
	assignPOSMeanings(senses, explain)

	if senses[0].MeaningCN != "虚荣" || senses[1].MeaningCN != "自负" {
		t.Fatalf("unexpected assignment: %#v", senses)
	}
	if senses[2].MeaningCN != "" {
		t.Fatalf("senses[2].MeaningCN = %q, want empty (no gloss left)", senses[2].MeaningCN)
	}
}

func TestAssignPOSMeanings_KeepsPOSGroupsIndependent(t *testing.T) {
	explain := "n. 名词甲；名词乙 v. 动词甲；动词乙"
	senses := []model.Sense{
		{POS: "noun", MeaningEN: "n1"},
		{POS: "verb", MeaningEN: "v1"},
		{POS: "noun", MeaningEN: "n2"},
		{POS: "verb", MeaningEN: "v2"},
	}
	assignPOSMeanings(senses, explain)

	if senses[0].MeaningCN != "名词甲" {
		t.Errorf("senses[0].MeaningCN = %q, want 名词甲", senses[0].MeaningCN)
	}
	if senses[2].MeaningCN != "名词乙" {
		t.Errorf("senses[2].MeaningCN = %q, want 名词乙", senses[2].MeaningCN)
	}
	if senses[1].MeaningCN != "动词甲" {
		t.Errorf("senses[1].MeaningCN = %q, want 动词甲", senses[1].MeaningCN)
	}
	if senses[3].MeaningCN != "动词乙" {
		t.Errorf("senses[3].MeaningCN = %q, want 动词乙", senses[3].MeaningCN)
	}
}
