package agent

import (
	"encoding/json"
	"strings"
	"testing"
)

func readyContract() RecommendationDecision {
	return RecommendationDecision{
		Status: DecisionReady, SelectedIDs: []string{"p1"}, ReasonCodes: []ReasonCode{}, Options: []RecommendationOption{},
		Assessments: []CandidateAssessment{{CandidateID: "p1",
			Range: MatchEvidence{State: MatchSatisfied, Fields: []EvidenceField{"location"}},
			Food:  MatchEvidence{State: MatchUnknown, Fields: []EvidenceField{}},
			Price: MatchEvidence{State: MatchUnknown, Fields: []EvidenceField{}},
		}},
	}
}

func TestDecision_Parse_whenEvidenceIsTriState(t *testing.T) {
	input, err := json.Marshal(readyContract())
	if err != nil {
		t.Fatal(err)
	}
	decision, err := ParseRecommendationDecision(input)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Assessments[0].Food.State != MatchUnknown || len(decision.SelectedIDs) != 1 {
		t.Fatalf("未知证据丢失: %+v", decision)
	}
}

func TestDecision_Parse_whenChoiceHasNoObservedAlternatives(t *testing.T) {
	radius := 4000
	decision := RecommendationDecision{Status: DecisionNeedsChoice, SelectedIDs: []string{}, Assessments: []CandidateAssessment{}, ReasonCodes: []ReasonCode{ReasonNoCandidates}, Options: []RecommendationOption{{
		OptionID: "expand", Label: "扩大到4公里后重新检索", ObservedIDs: []string{}, ReasonCodes: []ReasonCode{ReasonOutsideRadius}, Changes: PreferenceChanges{RadiusMeters: &radius},
	}}}
	input, err := json.Marshal(decision)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseRecommendationDecision(input); err != nil {
		t.Fatal(err)
	}
}

func TestDecision_Reject_whenShapeIsUnsafe(t *testing.T) {
	for name, mutate := range map[string]func(*RecommendationDecision){
		"未知状态":  func(d *RecommendationDecision) { d.Status = "accepted" },
		"未知三态":  func(d *RecommendationDecision) { d.Assessments[0].Food.State = "probably" },
		"无来源事实": func(d *RecommendationDecision) { d.Assessments[0].Food.State = MatchSatisfied },
		"越权来源":  func(d *RecommendationDecision) { d.Assessments[0].Range.Fields = []EvidenceField{"raw.secret"} },
		"重复来源": func(d *RecommendationDecision) {
			d.Assessments[0].Range.Fields = []EvidenceField{"location", "location"}
		},
		// “选中项须出现在 assessments 中”已取消（三态由服务端生成）；候选池归属由 RecommendationPolicy 校验。
		"重复选择":  func(d *RecommendationDecision) { d.SelectedIDs = []string{"p1", "p1"} },
		"重复评估":  func(d *RecommendationDecision) { d.Assessments = append(d.Assessments, d.Assessments[0]) },
		"超量选择":  func(d *RecommendationDecision) { d.SelectedIDs = make([]string, 11) },
		"超量评估":  func(d *RecommendationDecision) { d.Assessments = make([]CandidateAssessment, 226) },
		"超量选项":  func(d *RecommendationDecision) { d.Options = make([]RecommendationOption, 4) },
		"未知原因":  func(d *RecommendationDecision) { d.ReasonCodes = []ReasonCode{"invented"} },
		"重复原因":  func(d *RecommendationDecision) { d.ReasonCodes = []ReasonCode{ReasonNoCandidates, ReasonNoCandidates} },
		"待选仍推荐": func(d *RecommendationDecision) { d.Status = DecisionNeedsChoice },
		"就绪未选择": func(d *RecommendationDecision) { d.SelectedIDs = []string{} },
	} {
		t.Run(name, func(t *testing.T) {
			decision := readyContract()
			mutate(&decision)
			input, err := json.Marshal(decision)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := ParseRecommendationDecision(input); err == nil {
				t.Fatal("非法决策被接受")
			}
		})
	}
}

func TestDecision_Reject_whenJSONMalformedOrUnbounded(t *testing.T) {
	input, err := json.Marshal(readyContract())
	if err != nil {
		t.Fatal(err)
	}
	for _, malformed := range []string{
		`null`, `[]`, `{`, string(input) + `{}`, strings.Repeat(" ", MaxDecisionBytes) + string(input),
		strings.Replace(string(input), `"status":"ready"`, `"status":"ready","tool":"arbitrary"`, 1),
		strings.Replace(string(input), `"state":"unknown"`, `"state":"unknown","raw":{}`, 1),
	} {
		if _, err := ParseRecommendationDecision([]byte(malformed)); err == nil {
			t.Fatal("非法或超大JSON被接受")
		}
	}
}

func TestDecision_Reject_whenChoiceReferencesOrChangesInvalid(t *testing.T) {
	for name, mutate := range map[string]func(*RecommendationDecision){
		"空差异":    func(d *RecommendationDecision) { d.Options[0].Changes = PreferenceChanges{} },
		"重复选项":   func(d *RecommendationDecision) { d.Options = append(d.Options, d.Options[0]) },
		"重复观察":   func(d *RecommendationDecision) { d.Options[0].ObservedIDs = []string{"p1", "p1"} },
		"缺少证据数组": func(d *RecommendationDecision) { d.Assessments[0].Price.Fields = nil },
	} {
		t.Run(name, func(t *testing.T) {
			decision := readyContract()
			radius := 4000
			decision.Status, decision.SelectedIDs = DecisionNeedsChoice, []string{}
			decision.Options = []RecommendationOption{{OptionID: "expand", Label: "扩大到4公里", ObservedIDs: []string{}, ReasonCodes: []ReasonCode{ReasonOutsideRadius}, Changes: PreferenceChanges{RadiusMeters: &radius}}}
			mutate(&decision)
			input, err := json.Marshal(decision)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := ParseRecommendationDecision(input); err == nil {
				t.Fatal("非法协商被接受")
			}
		})
	}
}

func TestDecision_Accept_whenJSONExactlyAtByteLimit(t *testing.T) {
	input, err := json.Marshal(readyContract())
	if err != nil {
		t.Fatal(err)
	}
	input = append(input, []byte(strings.Repeat(" ", MaxDecisionBytes-len(input)))...)
	if _, err := ParseRecommendationDecision(input); err != nil {
		t.Fatal(err)
	}
}
