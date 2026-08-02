package service

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestContentModerationChunks_EmptyAndShortInput(t *testing.T) {
	require.Empty(t, splitContentModerationChunks(""))
	require.Empty(t, splitContentModerationChunks("   "))

	chunks := splitContentModerationChunks("你好，世界")
	require.Len(t, chunks, 1)
	require.Equal(t, "你好，世界", chunks[0].Text)
	require.Len(t, chunks[0].Hash, 64)
}

func TestContentModerationChunks_UseRuneBoundariesAndExactOverlap(t *testing.T) {
	runes := make([]rune, contentModerationChunkSizeRunes+37)
	for index := range runes {
		runes[index] = rune('一' + index%2000)
	}
	input := string(runes)

	chunks := splitContentModerationChunks(input)
	require.Len(t, chunks, 2)

	first := []rune(chunks[0].Text)
	second := []rune(chunks[1].Text)
	require.Len(t, first, contentModerationChunkSizeRunes)
	require.Equal(
		t,
		first[len(first)-contentModerationChunkOverlapRunes:],
		second[:contentModerationChunkOverlapRunes],
	)
	require.Equal(t, runes, append(first, second[contentModerationChunkOverlapRunes:]...))
}

func TestContentModerationChunks_ExactBoundaryDoesNotCreateEmptyTail(t *testing.T) {
	input := strings.Repeat("界", contentModerationChunkSizeRunes)

	chunks := splitContentModerationChunks(input)

	require.Len(t, chunks, 1)
	require.Equal(t, input, chunks[0].Text)
}

func TestContentModerationChunks_AppendingKeepsCompletedPrefixStable(t *testing.T) {
	base := strings.Repeat("a", contentModerationChunkSizeRunes*2+777)
	extended := base + strings.Repeat("b", contentModerationChunkSizeRunes)

	before := splitContentModerationChunks(base)
	after := splitContentModerationChunks(extended)

	require.Greater(t, len(before), 1)
	require.Greater(t, len(after), len(before))
	for index := 0; index < len(before)-1; index++ {
		require.Equal(t, before[index].Hash, after[index].Hash)
		require.Equal(t, before[index].Text, after[index].Text)
	}
	require.NotEqual(t, before[len(before)-1].Hash, after[len(before)-1].Hash)
}

func TestContentModerationChunkPolicyNamespace_IsCanonicalAndVersioned(t *testing.T) {
	first := defaultContentModerationConfig()
	first.BaseURL = "http://adapter.internal/"
	first.Model = "omni-moderation-latest"
	first.Thresholds = map[string]float64{
		"sexual":  0.65,
		"illicit": 0.95,
	}
	second := cloneContentModerationConfig(first)
	second.Thresholds = map[string]float64{
		"illicit": 0.95,
		"sexual":  0.65,
	}

	namespace := contentModerationChunkPolicyNamespace(first)
	require.Len(t, namespace, 64)
	require.Equal(t, namespace, contentModerationChunkPolicyNamespace(second))

	second.Model = "other-model"
	require.NotEqual(t, namespace, contentModerationChunkPolicyNamespace(second))

	second = cloneContentModerationConfig(first)
	second.BaseURL = "http://other-adapter.internal"
	require.NotEqual(t, namespace, contentModerationChunkPolicyNamespace(second))

	second = cloneContentModerationConfig(first)
	second.Thresholds["illicit"] = 0.5
	require.NotEqual(t, namespace, contentModerationChunkPolicyNamespace(second))
}
