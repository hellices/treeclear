package snapshot

import (
	"context"
	"fmt"
	"slices"
	"strings"
)

type untrackedReadNode struct {
	path     string
	name     string
	children []*untrackedReadNode
	byName   map[string]*untrackedReadNode
}

func selectUntrackedReadPaths(ctx context.Context, paths []string) (*untrackedReadNode, error) {
	if len(paths) > maximumUntrackedEntries {
		return nil, fmt.Errorf("%w: requested entry count", ErrUntrackedLimit)
	}
	requested := make([]UntrackedEntry, len(paths))
	for index, entryPath := range paths {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		requested[index] = UntrackedEntry{Path: entryPath, Kind: "file"}
	}
	if err := validateUntrackedEntries(requested, 0); err != nil {
		return nil, err
	}
	root := &untrackedReadNode{}
	nodes := []*untrackedReadNode{root}
	reserved := 0
	for _, entry := range requested {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		parent := root
		for component := range strings.SplitSeq(entry.Path, "/") {
			child, exists := parent.byName[component]
			if !exists {
				if reserved == maximumUntrackedEntries {
					return nil, fmt.Errorf("%w: selected leaves and parent directories", ErrUntrackedLimit)
				}
				reserved++
				entryPath := component
				if parent.path != "" {
					entryPath = parent.path + "/" + component
				}
				child = &untrackedReadNode{path: entryPath, name: component}
				if parent.byName == nil {
					parent.byName = make(map[string]*untrackedReadNode)
				}
				parent.byName[component] = child
				parent.children = append(parent.children, child)
				nodes = append(nodes, child)
			}
			parent = child
		}
	}
	for _, node := range nodes {
		node.byName = nil
		slices.SortFunc(node.children, func(left, right *untrackedReadNode) int { return strings.Compare(left.name, right.name) })
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return root, nil
}
