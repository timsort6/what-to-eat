package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"slices"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"go.uber.org/zap"
)

type RunStream struct {
	ctx         context.Context
	cancel      context.CancelFunc
	budget      *callBudget
	deltas      chan *schema.Message
	ready       chan struct{}
	done        chan struct{}
	err         error
	once        sync.Once
	mu          sync.RWMutex
	places      []Place
	decision    PublicDecision
	preferences Preferences
}

func (s *RunStream) execute(runner *Runner, input []*schema.Message) {
	err := s.runStages(runner, input)
	if contextErr := s.ctx.Err(); contextErr != nil {
		err = contextErr
	}
	if err != nil {
		s.err = fmt.Errorf("agent run: %w", err)
	}
	if s.budget != nil && s.budget.trace != nil {
		s.budget.trace.finish(err)
	}
	close(s.done)
	if err != nil {
		s.cancel()
	}
}

func (s *RunStream) runStages(runner *Runner, input []*schema.Message) error {
	rounds := searchRounds(runner.budgets)
	if runner.budgets.MaxModelCalls < 2 || runner.budgets.MaxSteps < 4 {
		return ErrBudgetExceeded
	}
	preferences, ok := RecommendationPreferencesFromContext(s.ctx)
	if !ok {
		return ErrInvalidRecommendation
	}
	scope, ok := RequestScopeFromContext(s.ctx)
	if !ok {
		return ErrInvalidScope
	}
	if err := validateScope(scope); err != nil {
		return err
	}
	policy, err := NewRecommendationPolicy(scope.Location, preferences)
	if err != nil {
		return err
	}
	// 只认输入末条的用户消息；末条非用户消息时无可引用原话，不回退到更早轮次（末条空白用户消息由 agentchat 开流前以 400 拒绝）。
	if last := len(input) - 1; last >= 0 && input[last] != nil && input[last].Role == schema.User {
		policy.latestUser = input[last].Content
	}
	infos, err := s.toolInfos(runner)
	if err != nil {
		return err
	}
	searchModel, err := runner.model.WithTools(slices.Clone(infos))
	if err != nil {
		return fmt.Errorf("bind search tools: %w", err)
	}
	searchInput := copyMessages(input)
	toolByName, configuredName, err := s.toolMap(runner)
	if err != nil {
		return err
	}
	close(s.ready)
	for range rounds {
		number, err := s.budget.takeSearchModel(s.ctx)
		if err != nil {
			return err
		}
		reader, err := searchModel.Stream(s.ctx, searchInput)
		message, err := collectModelMessage(s.ctx, reader, err)
		s.budget.complete("model", number, err)
		if err != nil {
			return err
		}
		if len(message.ToolCalls) == 0 {
			break
		}
		if err := validateSearchBatch(message.ToolCalls, toolByName); err != nil {
			return err
		}
		if err := s.budget.takeStep(s.ctx); err != nil {
			return err
		}
		searchInput = append(searchInput, message)
		if limit := s.budget.maxTools; len(message.ToolCalls) > limit {
			// 超量批次不执行任何工具；每个 tool_call_id 都回一条错误结果（否则 OpenAI 兼容接口 400），让模型在下一轮合并后重试。
			names := make([]string, 0, len(message.ToolCalls))
			feedback := fmt.Sprintf("本轮请求了%d个工具调用，超过每轮上限%d个，未执行。请合并关键词，最多%d个后重试。", len(message.ToolCalls), limit, limit)
			for _, call := range message.ToolCalls {
				names = append(names, call.Function.Name)
				searchInput = append(searchInput, schema.ToolMessage(feedback, call.ID))
			}
			zap.L().Info("agent.tool_batch_rejected", zap.Int("round", number), zap.Int("requested", len(names)), zap.Int("limit", limit), zap.Strings("tools", names))
			continue
		}
		for _, call := range message.ToolCalls {
			number, err := s.budget.takeTool(configuredName[call.Function.Name], s.ctx)
			if err != nil {
				return err
			}
			output, err := toolByName[call.Function.Name].InvokableRun(s.ctx, call.Function.Arguments)
			s.budget.complete("tool", number, err)
			if err != nil {
				return err
			}
			var result SearchResult
			if err := json.Unmarshal([]byte(output), &result); err != nil {
				return fmt.Errorf("decode search result: %w", err)
			}
			if err := policy.AddCandidates(result.Places); err != nil {
				return err
			}
			searchInput = append(searchInput, schema.ToolMessage(output, call.ID))
		}
	}
	decision, selected, err := s.finalize(runner, policy)
	if err != nil {
		return err
	}
	effective, err := policy.EffectivePreferences(decision)
	if err != nil {
		return err
	}
	public, err := publicDecision(policy, decision, selected)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.decision = public
	s.places = selected
	s.preferences = effective
	s.mu.Unlock()
	return s.streamFinal(runner, public, selected, effective)
}

func (s *RunStream) toolInfos(runner *Runner) ([]*schema.ToolInfo, error) {
	infos := make([]*schema.ToolInfo, 0, len(runner.tools))
	for _, candidate := range runner.tools {
		info, err := candidate.Info(s.ctx)
		if err != nil {
			return nil, err
		}
		if info == nil {
			return nil, errors.New("food tool returned nil info")
		}
		infos = append(infos, info)
	}
	return infos, nil
}

// toolMap 按模型侧工具名（Info.Name，如 amap_poi2_around）索引工具，并给出对应的配置名（如 amap.poi2_around）供诊断记录。
func (s *RunStream) toolMap(runner *Runner) (map[string]tool.InvokableTool, map[string]string, error) {
	tools := make(map[string]tool.InvokableTool, len(runner.tools))
	configured := make(map[string]string, len(runner.tools))
	for index, candidate := range runner.tools {
		info, err := candidate.Info(s.ctx)
		if err != nil {
			return nil, nil, err
		}
		if info == nil {
			return nil, nil, errors.New("food tool returned nil info")
		}
		tools[info.Name] = candidate
		configured[info.Name] = runner.toolNames[index]
	}
	return tools, configured, nil
}

// validateSearchBatch 只校验工具名与调用 ID；超出每轮上限由调用方反馈给模型重试，不在此拒绝。
func validateSearchBatch(calls []schema.ToolCall, tools map[string]tool.InvokableTool) error {
	if len(calls) == 0 {
		return ErrBudgetExceeded
	}
	seen := make(map[string]struct{}, len(calls))
	for _, call := range calls {
		if _, ok := tools[call.Function.Name]; !ok || call.ID == "" {
			return ErrInvalidToolInput
		}
		if _, ok := seen[call.ID]; ok {
			return ErrInvalidToolInput
		}
		seen[call.ID] = struct{}{}
	}
	return nil
}

func collectModelMessage(ctx context.Context, reader *schema.StreamReader[*schema.Message], err error) (*schema.Message, error) {
	if reader != nil {
		defer closeModelReader(ctx, reader)()
	}
	if err != nil {
		return nil, err
	}
	if reader == nil {
		return nil, errors.New("model returned nil stream")
	}
	var messages []*schema.Message
	for {
		message, recvErr := reader.Recv()
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if errors.Is(recvErr, io.EOF) {
			break
		}
		if recvErr != nil {
			return nil, recvErr
		}
		if message != nil {
			messages = append(messages, message)
		}
	}
	return schema.ConcatMessages(messages)
}

func closeModelReader(ctx context.Context, reader *schema.StreamReader[*schema.Message]) func() {
	closeOnce := sync.OnceFunc(reader.Close)
	closed := make(chan struct{})
	stop := context.AfterFunc(ctx, func() { defer close(closed); closeOnce() })
	return func() {
		if !stop() {
			<-closed
		}
		closeOnce()
	}
}

// 决策输入只附最新一条用户原话（供品类理解/冲突引用），不含助手历史。
func whitelistedDecisionInput(policy *RecommendationPolicy) string {
	b, _ := json.Marshal(struct {
		Preferences       Preferences            `json:"preferences"`
		LatestUserMessage string                 `json:"latest_user_message"`
		Candidates        []Place                `json:"candidates"`
		Decision          RecommendationDecision `json:"decision"`
	}{policy.preferences, policy.latestUser, policy.places, policy.Evaluate()})
	return string(b)
}

// streamFinal 的模型输入只有配置提示词与已接受事实：ready 为选中集，needs_choice 为方案及其引用候选（与发给客户端的决策同一投影）。
func (s *RunStream) streamFinal(runner *Runner, decision PublicDecision, selected []Place, preferences Preferences) error {
	input, err := json.Marshal(struct {
		Preferences Preferences    `json:"preferences"`
		Decision    PublicDecision `json:"decision"`
		Places      []Place        `json:"places"`
	}{preferences, decision, selected})
	if err != nil {
		return err
	}
	messages := []*schema.Message{schema.UserMessage(string(input))}
	if runner.prompt != "" {
		messages = append([]*schema.Message{schema.SystemMessage(runner.prompt)}, messages...)
	}
	number, err := s.budget.takeModel(s.ctx)
	if err != nil {
		return err
	}
	reader, err := runner.model.Stream(s.ctx, messages, model.WithTools(nil), model.WithToolChoice(schema.ToolChoiceForbidden))
	if reader != nil {
		defer closeModelReader(s.ctx, reader)()
	}
	if err != nil {
		s.budget.complete("model", number, err)
		return err
	}
	if reader == nil {
		err := errors.New("model returned nil stream")
		s.budget.complete("model", number, err)
		return err
	}
	var total int
	hasAnswer := false
	for {
		message, recvErr := reader.Recv()
		if err := s.ctx.Err(); err != nil {
			s.budget.complete("model", number, err)
			return err
		}
		if errors.Is(recvErr, io.EOF) {
			break
		}
		if recvErr != nil {
			s.budget.complete("model", number, recvErr)
			return recvErr
		}
		if message == nil {
			continue
		}
		if len(message.ToolCalls) > 0 {
			s.budget.complete("model", number, ErrInvalidRecommendation)
			return ErrInvalidRecommendation
		}
		if message.Content == "" {
			continue
		}
		hasAnswer = hasAnswer || strings.TrimSpace(message.Content) != ""
		total += utf8.RuneCountInString(message.Content)
		if total > 64000 {
			s.budget.complete("model", number, ErrBudgetExceeded)
			return ErrBudgetExceeded
		}
		select {
		case s.deltas <- schema.AssistantMessage(message.Content, nil):
		case <-s.ctx.Done():
			s.budget.complete("model", number, s.ctx.Err())
			return s.ctx.Err()
		}
	}
	if !hasAnswer {
		s.budget.complete("model", number, errors.New("final model returned no answer"))
		return errors.New("final model returned no answer")
	}
	s.budget.complete("model", number, nil)
	return nil
}

func (s *RunStream) Recv() (*schema.Message, error) {
	if err := s.ctx.Err(); err != nil {
		s.Close()
		if s.err != nil {
			return nil, s.err
		}
		return nil, err
	}
	select {
	case message := <-s.deltas:
		return message, nil
	case <-s.done:
		s.Close()
		if s.err != nil {
			return nil, s.err
		}
		return nil, io.EOF
	case <-s.ctx.Done():
		<-s.done
		s.Close()
		if s.err != nil {
			return nil, s.err
		}
		return nil, s.ctx.Err()
	}
}

func (s *RunStream) Places() []Place {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]Place(nil), s.places...)
}

// Decision 返回已通过服务端验证的决策快照；成功流的首个正文 delta 之前必然可用。
func (s *RunStream) Decision() (PublicDecision, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.decision, s.decision.Status != ""
}

// EffectivePreferences 返回该决策实际采用的条件（可能含从原话采纳的品类）；决策可用前为 false。
func (s *RunStream) EffectivePreferences() (Preferences, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.preferences, s.decision.Status != ""
}

// ObservedCandidate 是协商方案引用的真实已观察候选的白名单展示投影；三态评估在同一决策的 assessments 中按 candidate_id 关联。
type ObservedCandidate struct {
	CandidateID    string `json:"candidate_id"`
	Name           string `json:"name"`
	Address        string `json:"address"`
	DistanceMeters *int   `json:"distance_meters"`
}

// PublicDecision 是发往客户端的决策快照：已验证决策加方案引用候选的展示字段，不回流给模型。
type PublicDecision struct {
	RecommendationDecision
	Candidates []ObservedCandidate `json:"candidates"`
}

// publicDecision 把模型可能使用的 POI ID 统一为候选键，使 selected_ids 与卡片 candidate_key 对齐；
// ready 只保留选中项评估，needs_choice 只保留方案引用候选的评估与展示字段；所有切片非 nil 以便前端严格解析。
// 投影超过客户端 64KiB 上限时失败关闭，不发出半截决策。
func publicDecision(policy *RecommendationPolicy, decision RecommendationDecision, selected []Place) (PublicDecision, error) {
	key := func(id string) string { place, _ := policy.find(id); return place.CandidateKey }
	public := PublicDecision{RecommendationDecision: RecommendationDecision{Status: decision.Status, SelectedIDs: make([]string, 0, len(selected)),
		Assessments:        make([]CandidateAssessment, 0, len(decision.Assessments)),
		ReasonCodes:        append(make([]ReasonCode, 0, len(decision.ReasonCodes)), decision.ReasonCodes...),
		Options:            make([]RecommendationOption, 0, len(decision.Options)),
		FoodInterpretation: decision.FoodInterpretation, Conflict: decision.Conflict}, Candidates: []ObservedCandidate{}}
	kept := make(map[string]bool, len(selected))
	for _, place := range selected {
		public.SelectedIDs = append(public.SelectedIDs, place.CandidateKey)
		kept[place.CandidateKey] = true
	}
	for _, option := range decision.Options {
		observed := make([]string, 0, len(option.ObservedIDs))
		for _, id := range option.ObservedIDs {
			place, _ := policy.find(id)
			observed = append(observed, place.CandidateKey)
			if kept[place.CandidateKey] {
				continue
			}
			kept[place.CandidateKey] = true
			candidate := ObservedCandidate{CandidateID: place.CandidateKey, Name: place.Name, Address: place.Address}
			if distance, ok := candidateDistance(policy.origin, place.Location); ok {
				meters := int(math.Round(distance))
				candidate.DistanceMeters = &meters
			}
			public.Candidates = append(public.Candidates, candidate)
		}
		option.ObservedIDs = observed
		public.Options = append(public.Options, option)
	}
	for _, assessment := range decision.Assessments {
		assessment.CandidateID = key(assessment.CandidateID)
		if kept[assessment.CandidateID] {
			public.Assessments = append(public.Assessments, assessment)
		}
	}
	// 与 SSE 编码一致使用 json.Marshal（含 HTML 转义）计量。
	encoded, err := json.Marshal(public)
	if err != nil {
		return PublicDecision{}, err
	}
	if len(encoded) > MaxDecisionBytes {
		return PublicDecision{}, fmt.Errorf("公开决策超过 %d 字节: %w", MaxDecisionBytes, ErrInvalidRecommendation)
	}
	return public, nil
}

func (s *RunStream) Close() {
	s.once.Do(s.cancel)
	<-s.done
}
