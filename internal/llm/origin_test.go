package llm

import "testing"

// 用例与上游 modelmgr/compatibility_test.go 对齐（去掉上游独有的 BaseURL 维度：
// fork 的 Origin 只有协议与厂商，同厂商不同 endpoint 不构成不兼容）。
func TestCanSwitchMaterialCompatibility(t *testing.T) {
	for _, tc := range []struct {
		name           string
		source, target Origin
		allowed        bool
	}{
		{"new session", Origin{}, Origin{Protocol: ProtocolResponses, Provider: "a"}, true},
		{"chat vendors", Origin{Protocol: ProtocolChatCompletions, Provider: "a"}, Origin{Protocol: ProtocolChatCompletions, Provider: "b"}, true},
		{"migrated chat", Origin{Protocol: ProtocolChatCompletions}, Origin{Protocol: ProtocolChatCompletions, Provider: "b"}, true},
		{"same vendor", Origin{Protocol: ProtocolResponses, Provider: "a"}, Origin{Protocol: ProtocolResponses, Provider: "a"}, true},
		{"response vendors", Origin{Protocol: ProtocolResponses, Provider: "a"}, Origin{Protocol: ProtocolResponses, Provider: "b"}, false},
		{"chat to response", Origin{Protocol: ProtocolChatCompletions, Provider: "a"}, Origin{Protocol: ProtocolResponses, Provider: "a"}, false},
		{"response to chat", Origin{Protocol: ProtocolResponses, Provider: "a"}, Origin{Protocol: ProtocolChatCompletions, Provider: "a"}, false},
		{"missing response vendor", Origin{Protocol: ProtocolResponses}, Origin{Protocol: ProtocolResponses, Provider: "a"}, false},
		{"unknown target", Origin{}, Origin{}, false},
		{"target without provider", Origin{}, Origin{Protocol: ProtocolChatCompletions}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := CanSwitch(tc.source, tc.target); (err == nil) != tc.allowed {
				t.Fatalf("allowed=%v, error=%v", tc.allowed, err)
			}
		})
	}
}
