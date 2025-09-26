package run

import (
	"fmt"
	"math/rand"
	"time"

	"github.com/hectorgimenez/d2go/pkg/data"
	"github.com/hectorgimenez/d2go/pkg/data/area"
	"github.com/hectorgimenez/d2go/pkg/data/difficulty"
	"github.com/hectorgimenez/d2go/pkg/data/item"
	"github.com/hectorgimenez/d2go/pkg/data/npc"
	"github.com/hectorgimenez/d2go/pkg/data/object"
	"github.com/hectorgimenez/koolo/internal/action"
	"github.com/hectorgimenez/koolo/internal/action/step"
	"github.com/hectorgimenez/koolo/internal/config"
	"github.com/hectorgimenez/koolo/internal/context"
	"github.com/hectorgimenez/koolo/internal/game"
	"github.com/hectorgimenez/koolo/internal/town"
	"github.com/hectorgimenez/koolo/internal/ui"
	"github.com/lxn/win"
)

var anyaLocation = data.Position{
	X: 5107,
	Y: 5119,
}

const (
	//Other Possible Prefixes
	//1182 - Archer's +3 To Bow And Crossbow Skills
	//1185 - Athlete's +3 Passive And Magic Skills
	//1260 - Kenshi's +3 To Martial Arts
	magicPrefixID = 1188 //Lancer's +3 Javelin Skills
	magicSuffixID = 170
)

type GloveRun struct {
	ctx *context.Status
}

func NewGloveRun() *GloveRun {
	return &GloveRun{
		ctx: context.Get(),
	}
}

func (g GloveRun) Name() string {
	return string(config.GloveRun)
}

func (g GloveRun) Run() error {
	// 0. Check if Player has Money Need to add this Check

	// 1. Check if in Nightmare difficulty
	if g.ctx.Data.CharacterCfg.Game.Difficulty != difficulty.Nightmare {
		g.ctx.CharacterCfg.Game.Difficulty = difficulty.Nightmare
		g.ctx.Logger.Info("Fixing difficulty")
		g.ctx.Manager.ExitGame()
		return nil
	}

	// 2. Check if in Act 5, else go to Harrogath
	err := action.WayPoint(area.Harrogath)
	if err != nil {
		return err
	}

	// 3. Go to Anya and open trade
	vendorNPC := town.GetTownByArea(g.ctx.Data.PlayerUnit.Area).GamblingNPC()

	if vendorNPC == npc.Drehya {
		_ = action.MoveToCoords(anyaLocation)
	}

	// Loop until gloves are found and bought
	for {
		found := false
		action.InteractNPC(vendorNPC)
		if vendorNPC == npc.Drehya {
			g.ctx.HID.KeySequence(win.VK_HOME, win.VK_DOWN, win.VK_RETURN)
		}
		if g.ctx.Data.OpenMenus.NPCShop {
			for _, itm := range g.ctx.Data.Inventory.ByLocation(item.LocationVendor) {
				if itm.HasPrefix(magicPrefixID) && itm.HasSuffix(magicSuffixID) {
					g.ctx.Logger.Info("Found gloves to buy", "item", itm.Name)
					BuyItem(itm, 1)
					found = true
					break
				}
				g.ctx.Logger.Info("Found wrong item to buy", "item", itm.Name)
			}
		}
		if found {
			break
		}
		SleepTime()
		refreshingShopMenu()
	}

	return nil
}

func refreshingShopMenu() {
	ctx := context.Get()
	step.CloseAllMenus()

	redPortal, found := ctx.Data.Objects.FindOne(object.PermanentTownPortal)
	if !found {
		ctx.Logger.Info("red portal not found")
		return
	}

	SleepTime()
	err := action.InteractObject(redPortal, func() bool {
		return ctx.Data.AreaData.Area == area.NihlathaksTemple && ctx.Data.AreaData.IsInside(ctx.Data.PlayerUnit.Position)
	})
	if err != nil {
		return
	}

	// Find the red portal again in Nihlathak's Temple
	redPortal, found = ctx.Data.Objects.FindOne(object.PermanentTownPortal)
	if !found {
		ctx.Logger.Info("red portal not found in Nihlathak's Temple")
		return
	}

	SleepTime()
	// Interact with the portal to return to Harrogath
	err = action.InteractObject(redPortal, func() bool {
		return ctx.Data.AreaData.Area == area.Harrogath && ctx.Data.AreaData.IsInside(ctx.Data.PlayerUnit.Position)
	})
	if err != nil {
		return
	}
	SleepTime()
}

func BuyItem(i data.Item, quantity int) {
	ctx := context.Get()
	screenPos := ui.GetScreenCoordsForItem(i)

	time.Sleep(250 * time.Millisecond)
	for k := 0; k < quantity; k++ {
		ctx.HID.Click(game.RightButton, screenPos.X, screenPos.Y)
		SleepTime()
		ctx.Logger.Debug(fmt.Sprintf("Purchased %s [X:%d Y:%d]", i.Desc().Name, i.Position.X, i.Position.Y))
	}
}

func SleepTime() {
	time.Sleep(time.Duration(rand.Intn(1100-500)+500) * time.Millisecond)
}
