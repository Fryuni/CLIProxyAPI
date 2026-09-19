package common

import (
	"sort"

	"github.com/tidwall/gjson"
)

// AlignOpenAIToolCallMessages reorders tool result messages to immediately follow
// the assistant message that issued their matching tool_calls by tool_call_id.
// Ambiguous IDs are scoped by assistant message index so call IDs may be safely
// reused in later turns. It preserves original message order, content parts,
// reasoning fields, and numeric precision, while leaving ambiguous, orphan, and
// incomplete histories untouched.
func AlignOpenAIToolCallMessages(messages [][]byte, ambiguousByAssistant map[int]map[string]bool) [][]byte {
	if len(messages) <= 1 {
		return messages
	}

	type assistantRecord struct {
		msgIndex            int
		callIDs             []string
		hasInvalidOrEmptyID bool
		ambiguousCallIDs    map[string]bool
	}

	assistants := make([]assistantRecord, 0)

	for i, raw := range messages {
		if gjson.GetBytes(raw, "role").String() != "assistant" {
			continue
		}
		toolCalls := gjson.GetBytes(raw, "tool_calls")
		if !toolCalls.Exists() || !toolCalls.IsArray() {
			continue
		}
		rawCalls := toolCalls.Array()
		if len(rawCalls) == 0 {
			continue
		}

		callIDs := make([]string, 0, len(rawCalls))
		hasEmptyCallID := false
		seenCallIDs := make(map[string]bool)
		ambiguousCallIDs := make(map[string]bool)
		for _, tc := range rawCalls {
			callID := tc.Get("id").String()
			if callID == "" {
				hasEmptyCallID = true
				continue
			}
			if seenCallIDs[callID] {
				ambiguousCallIDs[callID] = true
			}
			seenCallIDs[callID] = true
			callIDs = append(callIDs, callID)
		}
		assistants = append(assistants, assistantRecord{
			msgIndex:            i,
			callIDs:             callIDs,
			hasInvalidOrEmptyID: hasEmptyCallID,
			ambiguousCallIDs:    ambiguousCallIDs,
		})
	}

	if len(assistants) == 0 {
		return messages
	}

	type reorderGroup struct {
		assistantIndex int
		toolIndices    []int
	}

	groups := make([]reorderGroup, 0)
	needsReorder := false

	for assistantIdx, ast := range assistants {
		if ast.hasInvalidOrEmptyID {
			continue
		}
		groupEnd := len(messages)
		if assistantIdx+1 < len(assistants) {
			groupEnd = assistants[assistantIdx+1].msgIndex
		}
		toolMsgIndicesByCallID := make(map[string][]int)
		for msgIdx := ast.msgIndex + 1; msgIdx < groupEnd; msgIdx++ {
			raw := messages[msgIdx]
			if gjson.GetBytes(raw, "role").String() != "tool" {
				continue
			}
			callID := gjson.GetBytes(raw, "tool_call_id").String()
			if callID != "" {
				toolMsgIndicesByCallID[callID] = append(toolMsgIndicesByCallID[callID], msgIdx)
			}
		}

		// Verify completeness and ambiguity within this assistant group.
		isEligible := true
		matchedToolIndices := make([]int, 0, len(ast.callIDs))

		for _, callID := range ast.callIDs {
			if ambiguousByAssistant[ast.msgIndex][callID] || ast.ambiguousCallIDs[callID] {
				isEligible = false
				break
			}
			indices := toolMsgIndicesByCallID[callID]
			if len(indices) != 1 {
				// Incomplete or ambiguous: exactly one tool message must match.
				isEligible = false
				break
			}
			matchedToolIndices = append(matchedToolIndices, indices[0])
		}

		if !isEligible {
			continue
		}

		// Sort tool message indices to preserve their relative order.
		sort.Ints(matchedToolIndices)

		// Check if tool messages already immediately follow this assistant.
		alreadyAdjacent := true
		for offset, toolIdx := range matchedToolIndices {
			if toolIdx != ast.msgIndex+offset+1 {
				alreadyAdjacent = false
				break
			}
		}

		if !alreadyAdjacent {
			needsReorder = true
			groups = append(groups, reorderGroup{
				assistantIndex: ast.msgIndex,
				toolIndices:    matchedToolIndices,
			})
		}
	}

	if !needsReorder {
		return messages
	}

	movedToolIndices := make(map[int]bool)
	toolsToInsert := make(map[int][][]byte)

	for _, g := range groups {
		toolList := make([][]byte, 0, len(g.toolIndices))
		for _, idx := range g.toolIndices {
			movedToolIndices[idx] = true
			toolList = append(toolList, messages[idx])
		}
		toolsToInsert[g.assistantIndex] = toolList
	}

	reordered := make([][]byte, 0, len(messages))
	for i := 0; i < len(messages); i++ {
		if movedToolIndices[i] {
			continue
		}
		reordered = append(reordered, messages[i])
		if tools, ok := toolsToInsert[i]; ok {
			reordered = append(reordered, tools...)
		}
	}

	return reordered
}
