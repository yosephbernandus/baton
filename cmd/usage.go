package cmd

import (
	"github.com/yosephbernandus/baton/internal/cost"
	"github.com/yosephbernandus/baton/internal/proto"
)

// costUsage converts a transport's token report into a cost entry's shape.
//
// A transport that reports nothing yields nil, which is what tells the tracker
// to fall back to elapsed time rather than record the turn as free.
func costUsage(u *proto.Usage) *cost.Usage {
	if u == nil {
		return nil
	}
	return &cost.Usage{
		InputTokens:      u.InputTokens,
		OutputTokens:     u.OutputTokens,
		CachedReadTokens: u.CachedReadTokens,
		TotalTokens:      u.TotalTokens,
	}
}
