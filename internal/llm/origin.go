package llm

import "fmt"

// Origin 标识"一段对话材料由哪套协议、哪个厂商产生"。它是"能不能换模型"的比较单位：
// 会话 metadata 里记录的 llm_origin、以及目标 provider/model 解析出的归属，都归约成
// 这个形状，因此切换裁决只有这一个入口，不必去各处找隐式的协议假设。
//
// Provider 为空表示来源没有记录厂商（迁移前的旧数据），按"未知"处理。
type Origin struct {
	Protocol Protocol
	Provider string
}

// CanSwitch 回答"这段已有材料能不能交给目标模型继续"。它只做比较：不解析模型、
// 不改选择、不看会话状态，也不读写任何存储。
//
// 规则与上游 Elflare/elbot 的 modelmgr.CanSwitch 一致：
//
//   - 目标缺协议或缺厂商：无法判断，直接拒绝。这是配置错误，不该静默放行；
//   - 来源没记协议：放行。旧会话没有这个事实，宁可让它试一次，也不要凭猜测拦人；
//   - 同协议且（是 chat_completions 或同厂商）：放行。chat 材料在厂商之间可移植；
//     responses 材料的续链依赖厂商侧状态（store / previous_response_id），跨厂商不可移植；
//   - 其余：拒绝。
func CanSwitch(source, target Origin) error {
	if target.Protocol == "" || target.Provider == "" {
		return fmt.Errorf("target model is missing protocol or provider")
	}
	if source.Protocol == "" {
		return nil
	}
	if source.Protocol == target.Protocol && (source.Protocol == ProtocolChatCompletions || source.Provider == target.Provider) {
		return nil
	}
	sourceProvider := source.Provider
	if sourceProvider == "" {
		sourceProvider = "unknown"
	}
	return fmt.Errorf("session context %s/%s is not compatible with target model %s/%s; start a new session or switch back to a compatible model",
		sourceProvider, source.Protocol, target.Provider, target.Protocol)
}
