package common

import (
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// SetResponsesToolCallIdentity writes a resolved Responses tool name and namespace.
func SetResponsesToolCallIdentity(item []byte, name, namespace, itemPath string) []byte {
	namePath := "name"
	namespacePath := "namespace"
	if itemPath != "" {
		namePath = itemPath + ".name"
		namespacePath = itemPath + ".namespace"
	}
	item, _ = sjson.SetBytes(item, namePath, name)
	if namespace != "" {
		item, _ = sjson.SetBytes(item, namespacePath, namespace)
	} else {
		item, _ = sjson.DeleteBytes(item, namespacePath)
	}
	return item
}

// ExtractResponsesCallID extracts the tool call ID from a Responses API item,
// prioritizing dedicated tool call references over generic item IDs:
// call_id -> tool_call_id -> callId -> id (excluding fco_ output item IDs)
func ExtractResponsesCallID(node gjson.Result) string {
	if callID := strings.TrimSpace(node.Get("call_id").String()); callID != "" {
		return callID
	}
	if toolCallID := strings.TrimSpace(node.Get("tool_call_id").String()); toolCallID != "" {
		return toolCallID
	}
	if callId := strings.TrimSpace(node.Get("callId").String()); callId != "" {
		return callId
	}
	id := strings.TrimSpace(node.Get("id").String())
	if strings.HasPrefix(id, "fco_") {
		return ""
	}
	return id
}

// NormalizeResponsesToolCallOutputs scans a slice of Responses input items,
// pairs function_call_output / custom_tool_call_output with preceding pending tool calls,
// and assigns missing call_ids to tool outputs using a multi-pass matching strategy:
// 1. Exact explicit call ID match (reserving calls for outputs that explicitly reference them).
// 2. Function name match for outputs that omit a call ID (skipping pending calls reserved by a later explicit output of their own).
// 3. FIFO queue fallback for remaining outputs that omit a call ID (skipping pending calls reserved by a later explicit output of their own).
// Outputs that already carry an explicit call ID that does not match any pending call
// are never rewritten or stolen.
func NormalizeResponsesToolCallOutputs(items []gjson.Result) []gjson.Result {
	if len(items) == 0 {
		return items
	}

	normalized := make([]gjson.Result, len(items))
	copy(normalized, items)

	// A call reserves the nearest explicit output that follows it and carries its ID, so an
	// ID-less output is never assigned to a call that already has one waiting. Pairing
	// right-to-left keeps the reservation scoped to the owning call group: when a later turn
	// reuses a call_id, that later call claims the explicit output, leaving an earlier call with
	// the same ID free to take an ID-less output in its own group.
	reservedExplicitOutput := make(map[int]bool)
	unclaimedExplicitOutputs := make(map[string]int)
	for idx := len(items) - 1; idx >= 0; idx-- {
		id := ExtractResponsesCallID(items[idx])
		if id == "" {
			continue
		}
		switch items[idx].Get("type").String() {
		case "function_call_output", "custom_tool_call_output":
			unclaimedExplicitOutputs[id]++
		case "function_call", "custom_tool_call":
			if unclaimedExplicitOutputs[id] > 0 {
				unclaimedExplicitOutputs[id]--
				reservedExplicitOutput[idx] = true
			}
		}
	}

	var pendingCallIDs []string
	var pendingReserved []bool
	pendingCallNames := make(map[string]string)

	i := 0
	for i < len(normalized) {
		item := normalized[i]
		itemType := item.Get("type").String()

		switch itemType {
		case "function_call", "custom_tool_call":
			callID := ExtractResponsesCallID(item)
			if callID != "" {
				pendingCallIDs = append(pendingCallIDs, callID)
				pendingReserved = append(pendingReserved, reservedExplicitOutput[i])
				name := item.Get("name").String()
				pendingCallNames[callID] = name
			}
			i++

		case "function_call_output", "custom_tool_call_output":
			start := i
			for i < len(normalized) && (normalized[i].Get("type").String() == "function_call_output" || normalized[i].Get("type").String() == "custom_tool_call_output") {
				i++
			}
			outputs := normalized[start:i]

			if len(pendingCallIDs) > 0 {
				used := make([]bool, len(outputs))
				matchedForPending := make([]int, len(pendingCallIDs))
				for idx := range matchedForPending {
					matchedForPending[idx] = -1
				}

				// Pass 1: exact explicit call ID match
				for pendingIdx, pendingID := range pendingCallIDs {
					for outIdx, out := range outputs {
						if !used[outIdx] && ExtractResponsesCallID(out) == pendingID {
							used[outIdx] = true
							matchedForPending[pendingIdx] = outIdx
							break
						}
					}
				}

				// Pass 2: match by function name for outputs with no explicit call ID (skipping pending IDs reserved by future explicit outputs)
				for pendingIdx, pendingID := range pendingCallIDs {
					if matchedForPending[pendingIdx] >= 0 || pendingReserved[pendingIdx] {
						continue
					}
					expectedName := pendingCallNames[pendingID]
					if expectedName != "" {
						for outIdx, out := range outputs {
							if !used[outIdx] && ExtractResponsesCallID(out) == "" {
								outName := strings.TrimSpace(out.Get("name").String())
								if outName != "" && outName == expectedName {
									used[outIdx] = true
									matchedForPending[pendingIdx] = outIdx
									break
								}
							}
						}
					}
				}

				// Pass 3: FIFO fallback for outputs with no explicit call ID (skipping pending IDs reserved by future explicit outputs)
				for pendingIdx := range pendingCallIDs {
					pendingID := pendingCallIDs[pendingIdx]
					if matchedForPending[pendingIdx] >= 0 || pendingReserved[pendingIdx] {
						continue
					}
					for outIdx, out := range outputs {
						if !used[outIdx] && ExtractResponsesCallID(out) == "" {
							outName := strings.TrimSpace(out.Get("name").String())
							expectedName := pendingCallNames[pendingID]
							if outName == "" || expectedName == "" || outName == expectedName {
								used[outIdx] = true
								matchedForPending[pendingIdx] = outIdx
								break
							}
						}
					}
				}

				// Apply matched call_ids to outputs
				var remainingPending []string
				var remainingReserved []bool
				for pendingIdx, pendingID := range pendingCallIDs {
					outIdx := matchedForPending[pendingIdx]
					if outIdx < 0 {
						remainingPending = append(remainingPending, pendingID)
						remainingReserved = append(remainingReserved, pendingReserved[pendingIdx])
						continue
					}
					matchedOut := outputs[outIdx]
					if matchedOut.Get("call_id").String() != pendingID {
						raw := []byte(matchedOut.Raw)
						raw, _ = sjson.SetBytes(raw, "call_id", pendingID)
						normalized[start+outIdx] = gjson.ParseBytes(raw)
					}
				}
				pendingCallIDs = remainingPending
				pendingReserved = remainingReserved
			}

		default:
			i++
		}
	}

	return normalized
}
