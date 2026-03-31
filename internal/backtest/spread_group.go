package backtest

import "time"

// SpreadGroup tracks a logical group of related spread positions that may be
// rolled (closed and reopened with new contracts). Rolling typically involves
// investment amount decay controlled by DecayFactor.
type SpreadGroup struct {
	ID          int
	Tag         string
	SpreadIDs   []int     // chronological: original, roll1, roll2, ...
	InitAmount  float64   // initial position size / investment amount
	DecayFactor float64   // multiplier per roll (e.g., 0.8 means 20% reduction)
	RollCount   int       // number of rolls performed
	OpenTime    time.Time // when the group was first opened
	Closed      bool
}

// ActiveSpreadID returns the ID of the most recent (active) spread in the chain.
func (sg *SpreadGroup) ActiveSpreadID() int {
	if len(sg.SpreadIDs) == 0 {
		return 0
	}
	return sg.SpreadIDs[len(sg.SpreadIDs)-1]
}

// CurrentAmount returns the investment amount after applying decay for all
// completed rolls.
func (sg *SpreadGroup) CurrentAmount() float64 {
	amount := sg.InitAmount
	for i := 0; i < sg.RollCount; i++ {
		amount *= sg.DecayFactor
	}
	return amount
}

// NextRollAmount returns the investment amount that would be used for the
// next roll.
func (sg *SpreadGroup) NextRollAmount() float64 {
	return sg.CurrentAmount() * sg.DecayFactor
}

// SpreadGroupTracker manages spread groups.
type SpreadGroupTracker struct {
	groups   []*SpreadGroup
	groupMap map[int]*SpreadGroup
	nextID   int
}

// NewSpreadGroupTracker creates an empty group tracker.
func NewSpreadGroupTracker() *SpreadGroupTracker {
	return &SpreadGroupTracker{
		nextID:   1,
		groupMap: make(map[int]*SpreadGroup),
	}
}

// Open creates a new spread group and returns its ID.
func (t *SpreadGroupTracker) Open(tag string, initAmount, decayFactor float64, openTime time.Time) int {
	id := t.nextID
	t.nextID++
	g := &SpreadGroup{
		ID:          id,
		Tag:         tag,
		InitAmount:  initAmount,
		DecayFactor: decayFactor,
		OpenTime:    openTime,
	}
	t.groups = append(t.groups, g)
	t.groupMap[id] = g
	return id
}

// AddSpread attaches a spread ID to a group.
func (t *SpreadGroupTracker) AddSpread(groupID, spreadID int) {
	if g, ok := t.groupMap[groupID]; ok {
		g.SpreadIDs = append(g.SpreadIDs, spreadID)
	}
}

// IncrementRoll advances the roll counter for a group.
func (t *SpreadGroupTracker) IncrementRoll(groupID int) {
	if g, ok := t.groupMap[groupID]; ok {
		g.RollCount++
	}
}

// Close marks a group as closed.
func (t *SpreadGroupTracker) Close(groupID int) {
	if g, ok := t.groupMap[groupID]; ok {
		g.Closed = true
	}
}

// Get returns a group by ID, or nil.
func (t *SpreadGroupTracker) Get(id int) *SpreadGroup {
	return t.groupMap[id]
}

// All returns all groups.
func (t *SpreadGroupTracker) All() []*SpreadGroup {
	return t.groups
}

// OpenGroups returns groups that are not yet closed.
func (t *SpreadGroupTracker) OpenGroups() []*SpreadGroup {
	var out []*SpreadGroup
	for _, g := range t.groups {
		if !g.Closed {
			out = append(out, g)
		}
	}
	return out
}
