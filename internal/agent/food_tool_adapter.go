package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

type foodInvokableTool[T, D any] struct {
	info *schema.ToolInfo
	fn   func(context.Context, T) (D, error)
}

func newFoodTool[T, D any](name, description string, params map[string]*schema.ParameterInfo, fn func(context.Context, T) (D, error)) tool.InvokableTool {
	return &foodInvokableTool[T, D]{
		info: &schema.ToolInfo{
			Name:        strings.ReplaceAll(name, ".", "_"),
			Desc:        description,
			ParamsOneOf: schema.NewParamsOneOfByParams(params),
		},
		fn: fn,
	}
}

func (t *foodInvokableTool[T, D]) Info(context.Context) (*schema.ToolInfo, error) {
	return t.info, nil
}

func (t *foodInvokableTool[T, D]) InvokableRun(ctx context.Context, argumentsInJSON string, _ ...tool.Option) (string, error) {
	var input T
	if err := json.Unmarshal([]byte(argumentsInJSON), &input); err != nil {
		return "", fmt.Errorf("decode tool arguments: %w", err)
	}
	output, err := t.fn(ctx, input)
	if err != nil {
		return "", err
	}
	encoded, err := json.Marshal(output)
	if err != nil {
		return "", fmt.Errorf("encode tool output: %w", err)
	}
	return string(encoded), nil
}

func aroundToolParams() map[string]*schema.ParameterInfo {
	return map[string]*schema.ParameterInfo{
		"keywords":    {Type: schema.String, Desc: "搜索关键词"},
		"types":       {Type: schema.String, Desc: "地点类型编码"},
		"radius":      {Type: schema.Integer, Desc: "搜索半径，最多 50000 米"},
		"sortrule":    {Type: schema.String, Desc: "排序规则，distance 或 weight"},
		"region":      {Type: schema.String, Desc: "区域"},
		"city_limit":  {Type: schema.Boolean, Desc: "是否限制在指定城市"},
		"show_fields": {Type: schema.String, Desc: "扩展字段列表"},
		"page_size":   {Type: schema.Integer, Desc: "每页数量，最多 25"},
		"page_num":    {Type: schema.Integer, Desc: "页码"},
	}
}

func textToolParams() map[string]*schema.ParameterInfo {
	return map[string]*schema.ParameterInfo{
		"keywords":    {Type: schema.String, Desc: "搜索关键词"},
		"types":       {Type: schema.String, Desc: "地点类型编码"},
		"region":      {Type: schema.String, Desc: "区域"},
		"city_limit":  {Type: schema.Boolean, Desc: "是否限制在指定城市"},
		"show_fields": {Type: schema.String, Desc: "扩展字段列表"},
		"page_size":   {Type: schema.Integer, Desc: "每页数量，最多 25"},
		"page_num":    {Type: schema.Integer, Desc: "页码"},
	}
}
