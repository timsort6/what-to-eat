package agent

import (
	"context"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/stretchr/testify/require"
)

type cancellationStagesModel struct {
	*stagesModel
	stage   string
	reached chan struct{}
	cleaned chan struct{}
	abort   chan struct{}
	enter   sync.Once
}

func (m *cancellationStagesModel) WithTools(infos []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	bound, err := m.stagesModel.WithTools(infos)
	if err != nil {
		return nil, err
	}
	stages, ok := bound.(*stagesModel)
	if !ok {
		return nil, ErrInvalidRecommendation
	}
	return &cancellationBoundModel{ToolCallingChatModel: stages, owner: m, infos: append([]*schema.ToolInfo(nil), infos...)}, nil
}

// Stream 覆盖未绑定实例工具的直接调用（如 FINAL 阶段），工具从调用选项读取；委托给 *stagesModel 不会递归。
func (m *cancellationStagesModel) Stream(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	infos := model.GetCommonOptions(nil, opts...).Tools
	return (&cancellationBoundModel{ToolCallingChatModel: m.stagesModel, owner: m, infos: infos}).Stream(ctx, input, opts...)
}

type cancellationBoundModel struct {
	model.ToolCallingChatModel
	owner *cancellationStagesModel
	infos []*schema.ToolInfo
}

func (m *cancellationBoundModel) Stream(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	stage := "SEARCH"
	choice := model.GetCommonOptions(nil, opts...).ToolChoice
	if choice != nil && *choice == schema.ToolChoiceForbidden {
		stage = "FINAL"
	} else if len(m.infos) == 1 && m.infos[0].Name == finalizeName {
		stage = "DECISION"
	}
	if stage != m.owner.stage {
		return m.ToolCallingChatModel.Stream(ctx, input, opts...)
	}
	m.owner.calls.Add(1)
	reader, writer := schema.Pipe[*schema.Message](0)
	go func() {
		defer close(m.owner.cleaned)
		defer writer.Close()
		content := ""
		if stage == "FINAL" {
			content = "early"
		}
		if writer.Send(schema.AssistantMessage(content, nil), nil) {
			return
		}
		<-ctx.Done()
		for !writer.Send(schema.AssistantMessage("", nil), nil) {
			select {
			case <-m.owner.abort:
				return
			default:
			}
		}
	}()
	return schema.StreamReaderWithConvert(reader, func(message *schema.Message) (*schema.Message, error) {
		m.owner.enter.Do(func() { close(m.owner.reached) })
		return message, nil
	}), nil
}

func waitCancellationSignal(t *testing.T, signal <-chan struct{}) bool {
	t.Helper()
	select {
	case <-signal:
		return true
	case <-time.After(time.Second):
		t.Error("未到达目标阶段或 reader 未清理")
		return false
	}
}

func TestRunner_StageCancellation_whenCallerCancelsOrStreamCloses(t *testing.T) {
	for _, stage := range []string{"SEARCH", "DECISION", "FINAL"} {
		for _, action := range []string{"caller", "close", "timeout"} {
			t.Run(stage+"_"+action, func(t *testing.T) {
				ctx, cancel := context.WithCancel(stagesContext(t.Context()))
				if action == "timeout" {
					ctx, cancel = context.WithTimeout(stagesContext(t.Context()), 200*time.Millisecond)
				}
				defer cancel()
				chat := &cancellationStagesModel{stagesModel: &stagesModel{calls: &atomic.Int32{}, searchRounds: 1, decision: stagesDecision(t)}, stage: stage, reached: make(chan struct{}), cleaned: make(chan struct{}), abort: make(chan struct{})}
				cfg := orchestrationConfig()
				cfg.MaxModelCalls = 3
				cfg.MaxSteps = 6
				runner, search := orchestrationRunner(t, chat, cfg)
				returned := make(chan *RunStream, 1)
				result := make(chan error, 1)
				go func() {
					stream, err := runner.Stream(ctx, nil)
					returned <- stream
					if err == nil {
						for err == nil {
							_, err = stream.Recv()
						}
					}
					result <- err
				}()
				require.True(t, waitCancellationSignal(t, chat.reached))
				var stream *RunStream
				if action == "close" {
					select {
					case stream = <-returned:
					case <-time.After(300 * time.Millisecond):
						cancel()
						close(chat.abort)
						<-chat.cleaned
						t.Fatal("目标阶段已到达但公共 Stream 尚未返回，无法 Close")
					}
					closed := make(chan struct{})
					go func() { stream.Close(); close(closed) }()
					if !waitCancellationSignal(t, closed) {
						close(chat.abort)
						<-chat.cleaned
						return
					}
				} else if action == "caller" {
					cancel()
				}
				select {
				case err := <-result:
					if action == "timeout" {
						require.ErrorIs(t, err, context.DeadlineExceeded)
					} else {
						require.ErrorIs(t, err, context.Canceled)
					}
					require.NotErrorIs(t, err, io.EOF)
				case <-time.After(time.Second):
					close(chat.abort)
					<-chat.cleaned
					t.Fatal("取消没有解除阶段读取")
				}
				require.True(t, waitCancellationSignal(t, chat.cleaned))
				wantCalls, wantSearch := int32(1), int32(0)
				switch stage {
				case "SEARCH":
				case "DECISION":
					wantCalls, wantSearch = 2, 3
				case "FINAL":
					wantCalls, wantSearch = 3, 3
				}
				require.Equal(t, wantCalls, chat.calls.Load())
				require.Equal(t, wantSearch, search.count.Load())
			})
		}
	}
}

func TestRunner_StageCancellation_whenFinalConsumerAppliesBackpressure(t *testing.T) {
	chat := &cancellationStagesModel{stagesModel: &stagesModel{calls: &atomic.Int32{}, searchRounds: 1, decision: stagesDecision(t)}, stage: "FINAL", reached: make(chan struct{}), cleaned: make(chan struct{}), abort: make(chan struct{})}
	cfg := orchestrationConfig()
	cfg.MaxModelCalls = 3
	cfg.MaxSteps = 6
	runner, search := orchestrationRunner(t, chat, cfg)
	stream, err := runner.Stream(stagesContext(t.Context()), nil)
	require.NoError(t, err)
	require.True(t, waitCancellationSignal(t, chat.reached))
	closed := make(chan struct{})
	go func() { stream.Close(); close(closed) }()
	require.True(t, waitCancellationSignal(t, closed))
	require.True(t, waitCancellationSignal(t, chat.cleaned))
	require.EqualValues(t, 3, chat.calls.Load())
	require.EqualValues(t, 3, search.count.Load())
	_, err = stream.Recv()
	require.ErrorIs(t, err, context.Canceled)
}
