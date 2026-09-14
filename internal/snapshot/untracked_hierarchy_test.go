package snapshot

import (
	"fmt"
	"strings"
	"testing"
)

func TestUntrackedCodecDeepHierarchyBoundaries(test *testing.T) {
	entries := untrackedCodecHierarchyEntries(4096, 2043)
	encoded, err := EncodeUntracked(entries, 64<<20)
	if err != nil {
		test.Fatalf("encode maximum-count deep hierarchy: %v", err)
	}
	decoded, err := DecodeUntracked(encoded, 64<<20)
	if err != nil || len(decoded) != len(entries) {
		test.Fatalf("decode maximum-count deep hierarchy: %d entries, %v", len(decoded), err)
	}
	prefix := strings.Repeat("a/", 2043)
	for index, entry := range decoded {
		if entry.Path != fmt.Sprintf("%sfile-%04d", prefix, index) || entry.Kind != "file" || entry.Mode != 0o600 || len(entry.Data) != 0 || entry.LinkTarget != "" {
			test.Fatalf("deep hierarchy entry %d changed", index)
		}
	}
}

func BenchmarkUntrackedCodecHierarchyValidation(benchmark *testing.B) {
	for _, count := range []int{64, 4096} {
		for _, depth := range []int{256, 2043} {
			benchmark.Run(fmt.Sprintf("entries-%d/depth-%d", count, depth), func(benchmark *testing.B) {
				entries := untrackedCodecHierarchyEntries(count, depth)
				benchmark.ReportAllocs()
				for benchmark.Loop() {
					if err := validateUntrackedEntries(entries, 64<<20); err != nil {
						benchmark.Fatal(err)
					}
				}
			})
		}
	}
}

func untrackedCodecHierarchyEntries(count, depth int) []UntrackedEntry {
	prefix := strings.Repeat("a/", depth)
	entries := make([]UntrackedEntry, count)
	for index := range entries {
		entries[index] = UntrackedEntry{
			Path: fmt.Sprintf("%sfile-%04d", prefix, index*2053%count), Kind: "file", Mode: 0o600,
		}
	}
	return entries
}
