package protocol

import "testing"

func TestBuildResponsesPreservesLiteralThinkAndAlternatingSegments(t *testing.T) {
	segments := []OutputSegment{
		{Mode: "answer", Text: "show <think>literal</think> markup"},
		{Mode: "thinking", Text: "alpha", ReasoningStreamMode: "summary"},
		{Mode: "answer", Text: "beta"},
		{Mode: "thinking", Text: "gamma", ReasoningStreamMode: "reasoning"},
		{Mode: "answer", Text: "delta"},
	}
	body := BuildResponseForMeta(ConversationMeta{Protocol: ProtocolResponses, Model: "test"}, TurnResult{
		ResponseID: "resp_test", OutputText: "ignored legacy fallback", OutputSegments: segments,
	})
	if body["output_text"] != "show <think>literal</think> markupbetadelta" {
		t.Fatalf("literal tag or answer text changed: %#v", body["output_text"])
	}
	output := body["output"].([]map[string]any)
	if len(output) != 5 {
		t.Fatalf("unexpected output item count: %#v", output)
	}
	wantTypes := []string{"message", "reasoning", "message", "reasoning", "message"}
	for index, want := range wantTypes {
		if output[index]["type"] != want {
			t.Fatalf("output order at %d: got=%v want=%v", index, output[index]["type"], want)
		}
	}
	firstReasoning := output[1]
	if len(firstReasoning["summary"].([]map[string]any)) != 1 || len(firstReasoning["content"].([]map[string]any)) != 0 {
		t.Fatalf("summary reasoning used both paths: %#v", firstReasoning)
	}
	secondReasoning := output[3]
	if len(secondReasoning["summary"].([]map[string]any)) != 0 || len(secondReasoning["content"].([]map[string]any)) != 1 {
		t.Fatalf("reasoning mode used both paths: %#v", secondReasoning)
	}
}

func TestBuildChatCompletionAggregatesOnlyStructuredThinking(t *testing.T) {
	body := BuildResponseForMeta(ConversationMeta{Protocol: ProtocolChatCompletions, Model: "test"}, TurnResult{
		OutputText: "show <think>literal</think> markup",
		OutputSegments: []OutputSegment{
			{Mode: "thinking", Text: "alpha"},
			{Mode: "answer", Text: "show <think>literal</think> markup"},
			{Mode: "thinking", Text: "gamma"},
			{Mode: "answer", Text: " answer"},
		},
	})
	message := body["choices"].([]map[string]any)[0]["message"].(map[string]any)
	if message["content"] != "show <think>literal</think> markup answer" {
		t.Fatalf("chat answer was modified: %#v", message)
	}
	if message["reasoning_content"] != "alphagamma" {
		t.Fatalf("chat reasoning aggregation changed: %#v", message)
	}
}

func TestBuildAnthropicDowngradesHumanThinkingToTextWithoutSignature(t *testing.T) {
	body := BuildResponseForMeta(ConversationMeta{Protocol: ProtocolAnthropicMessages, Model: "claude"}, TurnResult{
		OutputSegments: []OutputSegment{
			{Mode: "thinking", Text: "alpha"},
			{Mode: "answer", Text: "beta"},
			{Mode: "thinking", Text: "gamma"},
		},
	})
	content := body["content"].([]map[string]any)
	if len(content) != 3 || content[0]["type"] != "text" || content[1]["type"] != "text" || content[2]["type"] != "text" {
		t.Fatalf("thinking was not predictably downgraded: %#v", content)
	}
	for _, block := range content {
		if _, ok := block["signature"]; ok {
			t.Fatalf("unsigned signature was fabricated: %#v", block)
		}
		if block["type"] == "thinking" {
			t.Fatalf("human thinking became official block: %#v", block)
		}
	}
	if content[0]["text"] != "alpha" || content[1]["text"] != "beta" || content[2]["text"] != "gamma" {
		t.Fatalf("Anthropic text order changed: %#v", content)
	}
}

func TestBuildResponsesLegacyTextIsOpaque(t *testing.T) {
	literal := "show <think>literal</think> markup"
	body := BuildResponseForMeta(ConversationMeta{Protocol: ProtocolResponses}, TurnResult{OutputText: literal})
	if body["output_text"] != literal {
		t.Fatalf("legacy text was parsed as a thinking tag: %#v", body)
	}
}

func TestBuildToolCallStillUsesToolProtocolShape(t *testing.T) {
	body := BuildResponseForMeta(ConversationMeta{Protocol: ProtocolChatCompletions}, TurnResult{
		Mode: "tool_call", ToolName: "lookup", ToolCallID: "call_1", OutputText: `{"city":"Hangzhou"}`,
	})
	message := body["choices"].([]map[string]any)[0]["message"].(map[string]any)
	calls := message["tool_calls"].([]map[string]any)
	if len(calls) != 1 || calls[0]["type"] != "function" {
		t.Fatalf("tool call shape regressed: %#v", message)
	}
}

func TestBuildResponsesOutputIsSharedHelper(t *testing.T) {
	result := TurnResult{
		OutputSegments: []OutputSegment{
			{Mode: "thinking", Text: "alpha", ReasoningStreamMode: "summary"},
			{Mode: "answer", Text: "beta"},
			{Mode: "thinking", Text: "gamma", ReasoningStreamMode: "reasoning"},
			{Mode: "answer", Text: "delta"},
		},
	}
	viaHelper := BuildResponsesOutput(result)
	viaMeta := BuildResponseForMeta(ConversationMeta{Protocol: ProtocolResponses, Model: "test"}, result)["output"].([]map[string]any)
	if len(viaHelper) != len(viaMeta) {
		t.Fatalf("shared builder diverged in length helper=%d meta=%d", len(viaHelper), len(viaMeta))
	}
	for index := range viaHelper {
		if viaHelper[index]["type"] != viaMeta[index]["type"] {
			t.Fatalf("shared builder type mismatch at %d: %#v vs %#v", index, viaHelper[index], viaMeta[index])
		}
		if viaHelper[index]["type"] == "reasoning" {
			helperSummary := len(viaHelper[index]["summary"].([]map[string]any))
			helperContent := len(viaHelper[index]["content"].([]map[string]any))
			metaSummary := len(viaMeta[index]["summary"].([]map[string]any))
			metaContent := len(viaMeta[index]["content"].([]map[string]any))
			if helperSummary != metaSummary || helperContent != metaContent {
				t.Fatalf("reasoning field drift at %d helper=%#v meta=%#v", index, viaHelper[index], viaMeta[index])
			}
		}
	}
}
