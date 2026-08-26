package domain

// Stage is the ten-step position lifecycle enforced as a strict prefix:
// 进场合格, 基面合格, 就位, 调平, 座浆, 初紧, 终紧, 养护, 复测, 解锁.
// A position may only advance one step at a time and may never skip a stage.
type Stage uint8

const (
	StageIncomingAccepted Stage = iota // 进场合格
	StageBaseAccepted                  // 基面合格
	StagePlaced                        // 就位
	StageLeveled                       // 调平
	StageGrouted                       // 座浆
	StageInitialTightened              // 初紧
	StageFinalTightened                // 终紧
	StageCured                         // 养护
	StageRemeasured                    // 复测
	StageUnlocked                      // 解锁
)

var stageNames = [...]string{
	"incoming_accepted",
	"base_accepted",
	"placed",
	"leveled",
	"grouted",
	"initial_tightened",
	"final_tightened",
	"cured",
	"remeasured",
	"unlocked",
}

// String returns the stable wire name of the stage.
func (s Stage) String() string {
	if int(s) < len(stageNames) {
		return stageNames[s]
	}
	return "unknown"
}

// Valid reports whether the stage is within the defined ten-stage lifecycle.
func (s Stage) Valid() bool {
	return s <= StageUnlocked
}

// Next returns the stage immediately after s. The zero value (incoming
// accepted) advances to base accepted.
func (s Stage) Next() (Stage, bool) {
	if s >= StageUnlocked {
		return 0, false
	}
	return s + 1, true
}

// IsPrefix reports whether s is exactly the stage that must follow prev in the
// strict prefix. A position may only advance to prev.Next().
func IsNextStage(prev, next Stage) bool {
	if !prev.Valid() || !next.Valid() {
		return false
	}
	return next == prev+1
}

// StagePrefix is an ordered view of a position's progress.
type StagePrefix []Stage

// Has reports whether the prefix contains the given stage.
func (p StagePrefix) Has(s Stage) bool {
	for _, v := range p {
		if v == s {
			return true
		}
	}
	return false
}

// Max returns the furthest stage reached (the position's current stage).
func (p StagePrefix) Max() Stage {
	if len(p) == 0 {
		return StageIncomingAccepted
	}
	var max Stage
	for _, v := range p {
		if v > max {
			max = v
		}
	}
	return max
}
