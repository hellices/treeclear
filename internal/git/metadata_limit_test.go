package git

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestHashFileClassifiesInitialAndStreamingLimits(test *testing.T) {
	for _, growDuringRead := range []bool{false, true} {
		path := filepath.Join(test.TempDir(), "metadata")
		contents := []byte("xx")
		if growDuringRead {
			contents = contents[:1]
		}
		if err := os.WriteFile(path, contents, 0o600); err != nil {
			test.Fatal(err)
		}
		checks := 0
		ctx := metadataLimitContext{Context: test.Context(), beforeCheck: func() {
			checks++
			if growDuringRead && checks == 2 {
				if err := os.WriteFile(path, []byte("xx"), 0o600); err != nil {
					test.Fatal(err)
				}
			}
		}}
		digest, count, err := hashFile(ctx, path, 1)
		if !errors.Is(err, ErrReadLimit) || digest != "" || count != 0 || growDuringRead && checks != 2 {
			test.Fatalf("growth=%t: limit returned digest=%q count=%d checks=%d error=%v", growDuringRead, digest, count, checks, err)
		}
	}
}

func TestReadAdminEntriesPreservesCapacityCause(test *testing.T) {
	directory := test.TempDir()
	for _, name := range []string{"first", "second", "third"} {
		if err := os.WriteFile(filepath.Join(directory, name), nil, 0o600); err != nil {
			test.Fatal(err)
		}
	}
	entries, err := readAdminEntries(test.Context(), directory, 3)
	if err != nil || len(entries) != 3 {
		test.Fatalf("exact entry bound: count=%d error=%v", len(entries), err)
	}
	entries, err = readAdminEntries(test.Context(), directory, 2)
	if !errors.Is(err, ErrReadLimit) || entries != nil {
		test.Fatalf("entry overflow: count=%d error=%v", len(entries), err)
	}
}

type metadataLimitContext struct {
	context.Context
	beforeCheck func()
}

func (ctx metadataLimitContext) Err() error {
	ctx.beforeCheck()
	return ctx.Context.Err()
}
