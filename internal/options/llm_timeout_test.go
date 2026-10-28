package options

import "testing"

// N5：未单独配置 llm.timeout_seconds 时单次 HTTP 超时与 agent 超时一致（90s）；显式配置取两者较大值，不改原配置对象。
func TestEffectiveLLM_TimeoutFollowsAgent(t *testing.T) {
	options := NewServerRunOptions()
	if got := options.EffectiveLLM().TimeoutSeconds; got != 90 {
		t.Fatalf("default llm timeout = %d, want 90", got)
	}
	for _, fixture := range []struct{ llm, agent, want int }{{30, 90, 90}, {120, 90, 120}, {60, 150, 150}} {
		options.LLM.TimeoutSeconds, options.Agent.TimeoutSeconds = fixture.llm, fixture.agent
		if got := options.EffectiveLLM().TimeoutSeconds; got != fixture.want {
			t.Fatalf("llm=%d agent=%d -> %d, want %d", fixture.llm, fixture.agent, got, fixture.want)
		}
		if options.LLM.TimeoutSeconds != fixture.llm {
			t.Fatal("EffectiveLLM must not mutate options.LLM")
		}
	}
}
