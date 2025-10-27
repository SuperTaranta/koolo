package character

import (
	"fmt"
	"log/slog"
	"math"
	"sort"
	"time"

	"github.com/hectorgimenez/d2go/pkg/data"
	"github.com/hectorgimenez/d2go/pkg/data/npc"
	"github.com/hectorgimenez/d2go/pkg/data/skill"
	"github.com/hectorgimenez/d2go/pkg/data/stat"
	"github.com/hectorgimenez/koolo/internal/action/step"
	"github.com/hectorgimenez/koolo/internal/context"
	"github.com/hectorgimenez/koolo/internal/game"
	"github.com/hectorgimenez/koolo/internal/health"
	"github.com/hectorgimenez/koolo/internal/pather"
)

const (
	LightningMinDistance          = 10
	LightningMaxDistance          = 20
	LightningStaticMinDistance    = 1
	LightningStaticMaxDistance    = 3
	LightningMaxAttacksLoop       = 40
	LightningStaticFieldThreshold = 67 // Cast Static Field if monster HP is above this percentage
)

type LightningSorceress struct {
	BaseCharacter
}

func (s LightningSorceress) isPlayerDead2() bool {
	return s.Data.PlayerUnit.HPPercent() <= 0
}

func (s LightningSorceress) ShouldIgnoreMonster(m data.Monster) bool {
	return false
}

func (s LightningSorceress) CheckKeyBindings() []skill.ID {
	requiredKeybindings := []skill.ID{skill.ChainLightning, skill.Teleport, skill.TomeOfTownPortal, skill.StaticField}
	missingKeybindings := []skill.ID{}

	for _, cskill := range requiredKeybindings {
		if _, found := s.Data.KeyBindings.KeyBindingForSkill(cskill); !found {
			missingKeybindings = append(missingKeybindings, cskill)
		}
	}

	// Check for one of the armor skills
	armorSkills := []skill.ID{skill.FrozenArmor, skill.ShiverArmor, skill.ChillingArmor}
	hasArmor := false
	for _, armor := range armorSkills {
		if _, found := s.Data.KeyBindings.KeyBindingForSkill(armor); found {
			hasArmor = true
			break
		}
	}
	if !hasArmor {
		missingKeybindings = append(missingKeybindings, skill.FrozenArmor)
	}

	if len(missingKeybindings) > 0 {
		s.Logger.Debug("There are missing required key bindings.", slog.Any("Bindings", missingKeybindings))
	}

	return missingKeybindings
}

func (s LightningSorceress) KillMonsterSequence(
	monsterSelector func(d game.Data) (data.UnitID, bool),
	skipOnImmunities []stat.Resist,
) error {
	ctx := context.Get()
	completedAttackLoops := 0
	lastReposition := time.Now()
	staticFieldCast := false
	ldOpts := step.Distance(LightningMinDistance, LightningMaxDistance)
	lightningOpts := []step.AttackOption{
		step.RangedDistance(LightningMinDistance, LightningMaxDistance),
	}

	for {
		ctx.PauseIfNotPriority()

		if s.isPlayerDead2() { // Or directly: if s.Data.PlayerUnit.HPPercent() <= 0 {
			s.Logger.Info("Player detected as dead during KillMonsterSequence, stopping actions.")
			time.Sleep(500 * time.Millisecond)
			return health.ErrDied // Or return an error that indicates death if desired by higher-level logic
		}

		// First check if we need to reposition due to nearby monsters
		needsRepos, dangerousMonster := s.needsRepositioning()
		if needsRepos && time.Since(lastReposition) > time.Second*1 {
			lastReposition = time.Now()

			// Get the target monster ID
			targetID, found := monsterSelector(*s.Data)
			if !found {
				return nil
			}

			// Find the monster
			targetMonster, found := s.Data.Monsters.FindByID(targetID)
			if !found {
				s.Logger.Info("Target monster not found for repositioning")
				return nil
			}

			s.Logger.Info(fmt.Sprintf("Dangerous monster detected at distance %d, repositioning...",
				pather.DistanceFromPoint(s.Data.PlayerUnit.Position, dangerousMonster.Position)))

			// Find a safe position
			safePos, found := s.findSafePosition(targetMonster)
			if found {
				step.MoveTo(safePos)
			} else {
				s.Logger.Info("Could not find safe position for repositioning")
			}
		}
		id, found := monsterSelector(*s.Data)
		if !found {
			return nil
		}

		if !s.preBattleChecks(id, skipOnImmunities) {
			return nil
		}

		monster, found := s.Data.Monsters.FindByID(id)
		if !found || monster.Stats[stat.Life] <= 0 {
			return nil
		}

		// Cast Static Field first if needed
		if !staticFieldCast && s.shouldCastStaticField(monster) {
			staticOpts := []step.AttackOption{
				step.RangedDistance(LightningStaticMinDistance, LightningStaticMaxDistance),
			}

			if err := step.SecondaryAttack(skill.StaticField, monster.UnitID, 1, staticOpts...); err == nil {
				staticFieldCast = true
				continue
			}
		}

		if monster.Name == npc.Andariel ||
			monster.Name == npc.Duriel ||
			monster.Name == npc.Mephisto ||
			monster.Name == npc.Diablo ||
			monster.Name == npc.BaalCrab ||
			monster.Name == npc.Izual {
			if err := step.PrimaryAttack(monster.UnitID, 1, true, ldOpts); err == nil {
				completedAttackLoops++
			}
		} else {
			if err := step.SecondaryAttack(skill.ChainLightning, monster.UnitID, 1, lightningOpts...); err == nil {
				completedAttackLoops++
			}
		}

		if completedAttackLoops >= LightningMaxAttacksLoop {
			completedAttackLoops = 0
			staticFieldCast = false
		}
	}
}

func (s LightningSorceress) shouldCastStaticField(monster data.Monster) bool {
	// Only cast Static Field if monster HP is above threshold
	maxLife := float64(monster.Stats[stat.MaxLife])
	if maxLife == 0 {
		return false
	}

	hpPercentage := (float64(monster.Stats[stat.Life]) / maxLife) * 100
	return hpPercentage > LightningStaticFieldThreshold
}

func (s LightningSorceress) killBossWithStatic(bossID npc.ID, monsterType data.MonsterType) error {
	ctx := context.Get()

	for {
		ctx.PauseIfNotPriority()

		boss, found := s.Data.Monsters.FindOne(bossID, monsterType)
		if !found || boss.Stats[stat.Life] <= 0 {
			return nil
		}

		bossHPPercent := (float64(boss.Stats[stat.Life]) / float64(boss.Stats[stat.MaxLife])) * 100
		thresholdFloat := float64(ctx.CharacterCfg.Character.NovaSorceress.BossStaticThreshold)

		// Cast Static Field until boss HP is below threshold
		if bossHPPercent > thresholdFloat {
			staticOpts := []step.AttackOption{
				step.Distance(LightningStaticMinDistance, LightningStaticMaxDistance),
			}
			err := step.SecondaryAttack(skill.StaticField, boss.UnitID, 1, staticOpts...)
			if err != nil {
				s.Logger.Warn("Failed to cast Static Field", slog.String("error", err.Error()))
			}
			continue
		}

		// Switch to Lightning once boss HP is low enough
		return s.KillMonsterSequence(func(d game.Data) (data.UnitID, bool) {
			return boss.UnitID, true
		}, nil)
	}
}

func (s LightningSorceress) killMonsterByName(id npc.ID, monsterType data.MonsterType, skipOnImmunities []stat.Resist) error {
	return s.KillMonsterSequence(func(d game.Data) (data.UnitID, bool) {
		if m, found := d.Monsters.FindOne(id, monsterType); found {
			return m.UnitID, true
		}

		return 0, false
	}, skipOnImmunities)
}

func (s LightningSorceress) BuffSkills() []skill.ID {
	skillsList := make([]skill.ID, 0)
	if _, found := s.Data.KeyBindings.KeyBindingForSkill(skill.EnergyShield); found {
		skillsList = append(skillsList, skill.EnergyShield)
	}
	if _, found := s.Data.KeyBindings.KeyBindingForSkill(skill.ThunderStorm); found {
		skillsList = append(skillsList, skill.ThunderStorm)
	}

	// Add one of the armor skills
	for _, armor := range []skill.ID{skill.ChillingArmor, skill.ShiverArmor, skill.FrozenArmor} {
		if _, found := s.Data.KeyBindings.KeyBindingForSkill(armor); found {
			skillsList = append(skillsList, armor)
			break
		}
	}

	return skillsList
}

func (s LightningSorceress) PreCTABuffSkills() []skill.ID {
	return []skill.ID{}
}

func (s LightningSorceress) KillAndariel() error {
	return s.killBossWithStatic(npc.Andariel, data.MonsterTypeUnique)
}

func (s LightningSorceress) KillDuriel() error {
	return s.killBossWithStatic(npc.Duriel, data.MonsterTypeUnique)
}

func (s LightningSorceress) KillMephisto() error {
	return s.killBossWithStatic(npc.Mephisto, data.MonsterTypeUnique)
}

func (s LightningSorceress) KillDiablo() error {
	timeout := time.Second * 20
	startTime := time.Now()
	diabloFound := false

	for {
		if time.Since(startTime) > timeout && !diabloFound {
			s.Logger.Error("Diablo was not found, timeout reached")
			return nil
		}

		diablo, found := s.Data.Monsters.FindOne(npc.Diablo, data.MonsterTypeUnique)
		if !found || diablo.Stats[stat.Life] <= 0 {
			if diabloFound {
				return nil
			}
			time.Sleep(200 * time.Millisecond)
			continue
		}

		diabloFound = true
		s.Logger.Info("Diablo detected, attacking")

		return s.killBossWithStatic(npc.Diablo, data.MonsterTypeUnique)
	}
}

func (s LightningSorceress) KillBaal() error {
	return s.killBossWithStatic(npc.BaalCrab, data.MonsterTypeUnique)
}

func (s LightningSorceress) KillCountess() error {
	return s.killMonsterByName(npc.DarkStalker, data.MonsterTypeSuperUnique, nil)
}

func (s LightningSorceress) KillSummoner() error {
	return s.killMonsterByName(npc.Summoner, data.MonsterTypeUnique, nil)
}

func (s LightningSorceress) KillIzual() error {
	return s.killBossWithStatic(npc.Izual, data.MonsterTypeUnique)
}

func (s LightningSorceress) KillCouncil() error {
	return s.KillMonsterSequence(func(d game.Data) (data.UnitID, bool) {
		for _, m := range d.Monsters.Enemies() {
			if m.Name == npc.CouncilMember || m.Name == npc.CouncilMember2 || m.Name == npc.CouncilMember3 {
				return m.UnitID, true
			}
		}
		return 0, false
	}, nil)
}

func (s LightningSorceress) KillPindle() error {
	return s.killMonsterByName(npc.DefiledWarrior, data.MonsterTypeSuperUnique, s.CharacterCfg.Game.Pindleskin.SkipOnImmunities)
}

func (s LightningSorceress) KillNihlathak() error {
	return s.killMonsterByName(npc.Nihlathak, data.MonsterTypeSuperUnique, nil)
}

func (s LightningSorceress) needsRepositioning() (bool, data.Monster) {
	for _, monster := range s.Data.Monsters.Enemies() {
		if monster.Stats[stat.Life] <= 0 {
			continue
		}

		distance := pather.DistanceFromPoint(s.Data.PlayerUnit.Position, monster.Position)
		if distance < dangerDistance {
			return true, monster
		}
	}

	return false, data.Monster{}
}

func (s LightningSorceress) findSafePosition(targetMonster data.Monster) (data.Position, bool) {
	ctx := context.Get()
	playerPos := s.Data.PlayerUnit.Position

	// Define a stricter minimum safe distance from monsters
	const minSafeMonsterDistance = 2

	// Generate candidate positions in a circle around the player
	candidatePositions := []data.Position{}

	// First try positions in the opposite direction from the dangerous monster
	vectorX := playerPos.X - targetMonster.Position.X
	vectorY := playerPos.Y - targetMonster.Position.Y

	// Normalize the vector
	length := math.Sqrt(float64(vectorX*vectorX + vectorY*vectorY))
	if length > 0 {
		normalizedX := int(float64(vectorX) / length * float64(safeDistance))
		normalizedY := int(float64(vectorY) / length * float64(safeDistance))

		// Add positions in the opposite direction with some variation
		for offsetX := -3; offsetX <= 3; offsetX++ {
			for offsetY := -3; offsetY <= 3; offsetY++ {
				candidatePos := data.Position{
					X: playerPos.X + normalizedX + offsetX,
					Y: playerPos.Y + normalizedY + offsetY,
				}

				if s.Data.AreaData.IsWalkable(candidatePos) {
					candidatePositions = append(candidatePositions, candidatePos)
				}
			}
		}
	}

	// Generate positions in a circle with smaller angle increments for more candidates
	// Try positions in different directions around the player
	for angle := 0; angle < 360; angle += 5 {
		radians := float64(angle) * math.Pi / 180

		// Try multiple distances from the player
		for distance := minSafeMonsterDistance; distance <= safeDistance+5; distance += 2 {
			dx := int(math.Cos(radians) * float64(distance))
			dy := int(math.Sin(radians) * float64(distance))

			basePos := data.Position{
				X: playerPos.X + dx,
				Y: playerPos.Y + dy,
			}

			// Check a small area around the base position
			for offsetX := -1; offsetX <= 1; offsetX++ {
				for offsetY := -1; offsetY <= 1; offsetY++ {
					candidatePos := data.Position{
						X: basePos.X + offsetX,
						Y: basePos.Y + offsetY,
					}

					if s.Data.AreaData.IsWalkable(candidatePos) {
						candidatePositions = append(candidatePositions, candidatePos)
					}
				}
			}
		}
	}

	// No walkable positions found
	if len(candidatePositions) == 0 {
		return data.Position{}, false
	}

	// Evaluate all candidate positions
	type scoredPosition struct {
		pos   data.Position
		score float64
	}

	scoredPositions := []scoredPosition{}

	for _, pos := range candidatePositions {
		// Check if this position has line of sight to target
		if !ctx.PathFinder.LineOfSight(pos, targetMonster.Position) {
			continue
		}

		// Calculate minimum distance to any monster
		minMonsterDistance := math.MaxFloat64
		for _, monster := range s.Data.Monsters.Enemies() {
			if monster.Stats[stat.Life] <= 0 {
				continue
			}

			monsterDistance := pather.DistanceFromPoint(pos, monster.Position)
			if float64(monsterDistance) < minMonsterDistance {
				minMonsterDistance = float64(monsterDistance)
			}
		}

		// Strictly skip positions that are too close to monsters
		if minMonsterDistance < minSafeMonsterDistance {
			continue
		}

		// Calculate distance to target monster
		targetDistance := pather.DistanceFromPoint(pos, targetMonster.Position)

		// Score the position based on multiple factors:
		// 1. Distance from monsters (higher is better, with a strong preference for safety)
		// 2. Distance to target (should be in attack range)
		// 3. Distance from current position (closer is better for quick repositioning)
		distanceFromPlayer := pather.DistanceFromPoint(pos, playerPos)

		// Calculate attack range score (highest when in optimal attack range)
		attackRangeScore := 0.0
		if targetDistance >= minBlizzSorceressAttackDistance && targetDistance <= maxBlizzSorceressAttackDistance {
			attackRangeScore = 10.0
		} else {
			// Penalize positions outside attack range
			attackRangeScore = -math.Abs(float64(targetDistance) - float64(minBlizzSorceressAttackDistance+maxBlizzSorceressAttackDistance)/2.0)
		}

		// Final score calculation - heavily weight monster distance for safety
		score := minMonsterDistance*3.0 + attackRangeScore*2.0 - float64(distanceFromPlayer)*0.5

		// Extra bonus for positions that are very safe (far from monsters)
		if minMonsterDistance > float64(dangerDistance) {
			score += 5.0
		}

		scoredPositions = append(scoredPositions, scoredPosition{
			pos:   pos,
			score: score,
		})
	}

	// Sort positions by score (highest first)
	sort.Slice(scoredPositions, func(i, j int) bool {
		return scoredPositions[i].score > scoredPositions[j].score
	})

	// Return the best position if we found any
	if len(scoredPositions) > 0 {
		s.Logger.Info(fmt.Sprintf("Found safe position with score %.2f at distance %.2f from nearest monster",
			scoredPositions[0].score, minMonsterDistance(scoredPositions[0].pos, s.Data.Monsters)))
		return scoredPositions[0].pos, true
	}

	return data.Position{}, false
}
