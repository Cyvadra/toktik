package optutil

import (
	"github.com/Cyvadra/toktik/internal/backtest"
)

// GroupMixin provides position-group lifecycle helpers. Embed it alongside
// PricingMixin in your strategy struct.
type GroupMixin struct {
	PositionGroupID int
}

// OpenGroup creates a new position group and stores its ID.
func (g *GroupMixin) OpenGroup(ctx *backtest.BarContext, tag string, amount, decay float64) int {
	if ctx.SpreadGroups() == nil {
		return 0
	}
	id := ctx.SpreadGroups().Open(tag, amount, decay, ctx.Time())
	g.PositionGroupID = id
	return id
}

// CloseGroup closes the specified position group and clears the stored ID
// if it matches.
func (g *GroupMixin) CloseGroup(ctx *backtest.BarContext, groupID int) {
	if groupID <= 0 {
		return
	}
	if ctx.SpreadGroups() != nil {
		ctx.SpreadGroups().Close(groupID)
	}
	if g.PositionGroupID == groupID {
		g.PositionGroupID = 0
	}
}

// OpenSpreadInGroup opens a spread inside a position group. Falls back to a
// plain OpenSpread when groupID is zero.
func (g *GroupMixin) OpenSpreadInGroup(ctx *backtest.BarContext, legs []backtest.SpreadLeg, tag string, groupID int) int {
	if groupID > 0 && ctx.SpreadGroups() != nil {
		return ctx.OpenSpreadInGroup(legs, tag, groupID)
	}
	return ctx.OpenSpread(legs, tag)
}
